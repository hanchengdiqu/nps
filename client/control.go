// Package client
//
// control.go 负责 npc 客户端与 nps 服务端的“控制链路”建立与维护：
// - 从本地配置文件读取配置后与服务端建立连接并同步配置(StartFromFile)
// - 查询当前客户端在服务端上的任务/主机运行状态(GetTaskStatus)
// - 按需通过 TCP、KCP(UDP) 或经由 HTTP/SOCKS5 代理与服务端握手(NewConn)
// - 支持 HTTP 代理的 CONNECT 隧道建立(NewHttpProxyConn)
// - 提供 P2P UDP 打洞所需的辅助函数(handleP2PUdp 等)
//
// 注意：本次提交仅为源码增加中文注释，未对任何业务逻辑做改动，以便阅读和二次开发。
package client

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/config"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/version"
	"github.com/astaxie/beego/logs"
	"github.com/xtaci/kcp-go"
	"golang.org/x/net/proxy"
)

// GetTaskStatus 连接服务端并拉取当前配置中 Host 与 Task 的运行状态。
//
// 参数:
//   - path: 本地 npc 配置文件路径。
//
// 流程:
//  1. 读取配置并与服务端建立控制连接。
//  2. 发送 WORK_STATUS 指令，请求状态列表。
//  3. 读取本地缓存的 vkey（运行时下发的临时口令），计算 md5 后回传用于鉴权。
//  4. 服务端返回正在运行的 remark 列表，逐项比对配置，输出 ok/not running。
//
// 备注: 函数最后调用 os.Exit(0) 直接退出进程，适用于“查询一次即退出”的命令式调用。
func GetTaskStatus(path string) {
	cnf, err := config.NewConfig(path)
	if err != nil {
		log.Fatalln(err)
	}
	// 与服务端建立一次性的控制连接
	c, err := NewConn(cnf.CommonConfig.Tp, cnf.CommonConfig.VKey, cnf.CommonConfig.Server, common.WORK_CONFIG, cnf.CommonConfig.ProxyUrl)
	if err != nil {
		log.Fatalln(err)
	}
	// 请求状态
	if _, err := c.Write([]byte(common.WORK_STATUS)); err != nil {
		log.Fatalln(err)
	}
	// 读取运行时下发的 vkey（保存在临时目录），以其 md5 作为二次校验
	if f, err := common.ReadAllFromFile(filepath.Join(common.GetTmpPath(), "npc_vkey.txt")); err != nil {
		log.Fatalln(err)
	} else if _, err := c.Write([]byte(crypt.Md5(string(f)))); err != nil {
		log.Fatalln(err)
	}
	// 读取服务端返回的 isPub 标记（当前连接是否处于“公共/下发新口令”模式）
	var isPub bool
	binary.Read(c, binary.LittleEndian, &isPub)
	// 读取 remark 列表并逐项对比输出
	if l, err := c.GetLen(); err != nil {
		log.Fatalln(err)
	} else if b, err := c.GetShortContent(l); err != nil {
		log.Fatalln(err)
	} else {
		arr := strings.Split(string(b), common.CONN_DATA_SEQ)
		// 检查 Host
		for _, v := range cnf.Hosts {
			if common.InStrArr(arr, v.Remark) {
				log.Println(v.Remark, "ok")
			} else {
				log.Println(v.Remark, "not running")
			}
		}
		// 检查 Task（可能包含多端口或 secret 模式的伪端口 0）
		for _, v := range cnf.Tasks {
			ports := common.GetPorts(v.Ports)
			if v.Mode == "secret" {
				ports = append(ports, 0)
			}
			for _, vv := range ports {
				var remark string
				if len(ports) > 1 {
					remark = v.Remark + "_" + strconv.Itoa(vv)
				} else {
					remark = v.Remark
				}
				if common.InStrArr(arr, remark) {
					log.Println(remark, "ok")
				} else {
					log.Println(remark, "not running")
				}
			}
		}
	}
	os.Exit(0)
}

// errAdd 表示服务端拒绝添加 Host/Task 的通用错误（如端口被占用或者不允许开放）。
var errAdd = errors.New("The server returned an error, which port or host may have been occupied or not allowed to open.")

// StartFromFile 从指定配置文件启动 npc 客户端。
//
// 主要职责:
//   - 读取并验证配置
//   - 与服务端握手、必要时接收下发的临时 vkey（isPub=true 时）
//   - 将 Hosts 与 Tasks 同步到服务端，启动本地文件服务或本地监听（secret/p2p）
//   - 启动 RPC 客户端保持与服务端的长连接，并在断开后根据 AutoReconnection 自动重连
//
// 流程概述:
//  1. 循环重连(re 标签)，失败时等待 5 秒，直至配置禁止自动重连。
//  2. NewConn 建立控制连接并进行版本/校验握手。
//  3. 若 isPub=true，则先同步全局客户端配置(common.NEW_CONF)，随后接收 16 字节的临时 vkey。
//  4. 推送所有 Hosts 与 Tasks 至服务端，逐项检查添加状态；Task 为文件模式时启动本地文件服务器。
//  5. 启动本地服务(LocalServer)用于 secret 或 p2p 场景。
//  6. 关闭控制连接，提示 Web 登录信息，随后启动 RPC 客户端保持业务通道。
func StartFromFile(path string) {
	first := true
	cnf, err := config.NewConfig(path)
	if err != nil || cnf.CommonConfig == nil {
		logs.Error("Config file %s loading error %s", path, err.Error())
		os.Exit(0)
	}
	logs.Info("Loading configuration file %s successfully", path)

re:
	// 根据 AutoReconnection 决定是否继续重连；非首次进入时等待 5 秒
	if first || cnf.CommonConfig.AutoReconnection {
		if !first {
			logs.Info("Reconnecting...")
			time.Sleep(time.Second * 5)
		}
	} else {
		return
	}
	first = false

	// 建立控制连接（WORK_CONFIG）
	c, err := NewConn(cnf.CommonConfig.Tp, cnf.CommonConfig.VKey, cnf.CommonConfig.Server, common.WORK_CONFIG, cnf.CommonConfig.ProxyUrl)
	if err != nil {
		logs.Error(err)
		goto re
	}
	var isPub bool
	binary.Read(c, binary.LittleEndian, &isPub)

	// 运行期 vkey（用于 Web 登录）
	var b []byte
	vkey := cnf.CommonConfig.VKey
	if isPub {
		// 将全局配置发送到服务端，服务器可能基于此为该客户端分配/确认账户信息
		if _, err := c.SendInfo(cnf.CommonConfig.Client, common.NEW_CONF); err != nil {
			logs.Error(err)
			goto re
		}
		if !c.GetAddStatus() {
			logs.Error("the web_user may have been occupied!")
			goto re
		}

		// 读取服务端下发的 16 字节临时 vkey
		if b, err = c.GetShortContent(16); err != nil {
			logs.Error(err)
			goto re
		}
		vkey = string(b)
	}
	// 将 vkey 缓存到临时目录，供其他命令(如查询状态)复用
	ioutil.WriteFile(filepath.Join(common.GetTmpPath(), "npc_vkey.txt"), []byte(vkey), 0600)

	// 同步 Hosts 到服务端
	for _, v := range cnf.Hosts {
		if _, err := c.SendInfo(v, common.NEW_HOST); err != nil {
			logs.Error(err)
			goto re
		}
		if !c.GetAddStatus() {
			logs.Error(errAdd, v.Host)
			goto re
		}
	}

	// 同步 Tasks 到服务端；文件任务会在本地启动简单的文件服务器
	for _, v := range cnf.Tasks {
		if _, err := c.SendInfo(v, common.NEW_TASK); err != nil {
			logs.Error(err)
			goto re
		}
		if !c.GetAddStatus() {
			logs.Error(errAdd, v.Ports, v.Remark)
			goto re
		}
		if v.Mode == "file" {
			// 启动本地文件服务，供服务端通过该任务访问
			go startLocalFileServer(cnf.CommonConfig, v, vkey)
		}
	}

	// 启动本地服务（secret/p2p 等场景的本地监听）
	for _, v := range cnf.LocalServer {
		go StartLocalServer(v, cnf.CommonConfig)
	}

	// 控制通道阶段完成，关闭临时连接
	c.Close()
	// 提示 Web 登录账号
	if cnf.CommonConfig.Client.WebUserName == "" || cnf.CommonConfig.Client.WebPassword == "" {
		logs.Notice("web access login username:user password:%s", vkey)
	} else {
		logs.Notice("web access login username:%s password:%s", cnf.CommonConfig.Client.WebUserName, cnf.CommonConfig.Client.WebPassword)
	}
	// 启动 RPC 客户端保持业务链路
	NewRPClient(cnf.CommonConfig.Server, vkey, cnf.CommonConfig.Tp, cnf.CommonConfig.ProxyUrl, cnf, cnf.CommonConfig.DisconnectTime).Start()
	CloseLocalServer()
	goto re
}

// NewConn 与服务端建立一次控制连接并做版本/校验握手。
//
// 参数:
//   - tp: 传输协议，tcp 或 kcp。
//   - vkey: 验证密钥。
//   - server: 服务端地址（host:port）。
//   - connType: 本次连接用途（如 WORK_CONFIG）。
//   - proxyUrl: 代理地址，支持 socks5:// 或 http(s)://。
//
// 返回: 成功返回封装后的 conn.Conn；失败返回 error。
func NewConn(tp string, vkey string, server string, connType string, proxyUrl string) (*conn.Conn, error) {
	var err error
	var connection net.Conn
	var sess *kcp.UDPSession
	if tp == "tcp" {
		// TCP 分为直连与经由代理两种
		if proxyUrl != "" {
			u, er := url.Parse(proxyUrl)
			if er != nil {
				return nil, er
			}
			switch u.Scheme {
			case "socks5":
				// 通过 SOCKS5 代理建立到服务端的 TCP 连接
				n, er := proxy.FromURL(u, nil)
				if er != nil {
					return nil, er
				}
				connection, err = n.Dial("tcp", server)
			default:
				// 其他情况按 HTTP 代理处理（CONNECT 隧道）
				connection, err = NewHttpProxyConn(u, server)
			}
		} else {
			// 无代理，直接拨号
			connection, err = net.Dial("tcp", server)
		}
	} else {
		// KCP(UDP) 方式
		sess, err = kcp.DialWithOptions(server, nil, 10, 3)
		if err == nil {
			conn.SetUdpSession(sess)
			connection = sess
		}
	}
	if err != nil {
		return nil, err
	}
	// 为初次握手设置 10 秒超时，随后清除
	connection.SetDeadline(time.Now().Add(time.Second * 10))
	defer connection.SetDeadline(time.Time{})

	c := conn.NewConn(connection)
	// 发送“连通性测试”并协商版本
	if _, err := c.Write([]byte(common.CONN_TEST)); err != nil {
		return nil, err
	}
	if err := c.WriteLenContent([]byte(version.GetVersion())); err != nil {
		return nil, err
	}
	if err := c.WriteLenContent([]byte(version.VERSION)); err != nil {
		return nil, err
	}
	// 服务端回写 client core 的 MD5，用于版本匹配校验
	b, err := c.GetShortContent(32)
	if err != nil {
		logs.Error(err)
		return nil, err
	}
	if crypt.Md5(version.GetVersion()) != string(b) {
		logs.Error("The client does not match the server version. The current core version of the client is", version.GetVersion())
		return nil, err
	}
	// 发送鉴权值
	if _, err := c.Write([]byte(common.Getverifyval(vkey))); err != nil {
		return nil, err
	}
	// 读取鉴权结果
	if s, err := c.ReadFlag(); err != nil {
		return nil, err
	} else if s == common.VERIFY_EER {
		return nil, errors.New(fmt.Sprintf("Validation key %s incorrect", vkey))
	}
	// 发送连接用途类型
	if _, err := c.Write([]byte(connType)); err != nil {
		return nil, err
	}
	// 开启保活（按协议类型）
	c.SetAlive(tp)

	return c, nil
}

// NewHttpProxyConn 通过 HTTP 代理建立到远端的 CONNECT 隧道。
//
// 支持在 URL 中携带基本认证信息(user:pass)，自动附加 Authorization 头。
func NewHttpProxyConn(url *url.URL, remoteAddr string) (net.Conn, error) {
	// 构造 CONNECT 请求
	req, err := http.NewRequest("CONNECT", "http://"+remoteAddr, nil)
	if err != nil {
		return nil, err
	}
	// HTTP 基本认证
	password, _ := url.User.Password()
	req.Header.Set("Authorization", "Basic "+basicAuth(strings.Trim(url.User.Username(), " "), password))
	// 先与代理建立 TCP 连接
	proxyConn, err := net.Dial("tcp", url.Host)
	if err != nil {
		return nil, err
	}
	// 发送 CONNECT 请求
	if err := req.Write(proxyConn); err != nil {
		return nil, err
	}
	// 读取代理响应
	res, err := http.ReadResponse(bufio.NewReader(proxyConn), req)
	if err != nil {
		return nil, err
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errors.New("Proxy error " + res.Status)
	}
	return proxyConn, nil
}

// basicAuth 返回 HTTP Basic-Auth 的 base64 负载（不含 "Basic " 前缀）。
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

// getRemoteAddressFromServer 给“中继/信令服务器”发送打洞握手包，获取对端外网地址信息。
//
// rAddr 是服务器的 UDP 地址，add 表示在 rAddr 基础上偏移的端口（0/1/2 三个信道）。
func getRemoteAddressFromServer(rAddr string, localConn *net.UDPConn, md5Password, role string, add int) error {
	// 计算不同的服务端端口（用于不同信道）
	rAddr, err := getNextAddr(rAddr, add)
	if err != nil {
		logs.Error(err)
		return err
	}
	addr, err := net.ResolveUDPAddr("udp", rAddr)
	if err != nil {
		return err
	}
	// 数据负载包含 md5Password 与 role，服务端据此识别会话并回包
	if _, err := localConn.WriteTo(common.GetWriteStr(md5Password, role), addr); err != nil {
		return err
	}
	return nil
}

// handleP2PUdp 完成 P2P UDP 打洞的前置步骤：
//  1. 本地监听 UDP
//  2. 向信令服务器三个端口发送握手，等待回包，得到对端的三个外网地址
//  3. 调用 sendP2PTestMsg 进行真实的打洞互探
//
// 成功后返回可用的对端地址与一个新的 PacketConn（用于后续数据传输）。
func handleP2PUdp(localAddr, rAddr, md5Password, role string) (remoteAddress string, c net.PacketConn, err error) {
	localConn, err := newUdpConnByAddr(localAddr)
	if err != nil {
		return
	}
	// 分别在 rAddr、rAddr+1、rAddr+2 三个端口进行握手
	err = getRemoteAddressFromServer(rAddr, localConn, md5Password, role, 0)
	if err != nil {
		logs.Error(err)
		return
	}
	err = getRemoteAddressFromServer(rAddr, localConn, md5Password, role, 1)
	if err != nil {
		logs.Error(err)
		return
	}
	err = getRemoteAddressFromServer(rAddr, localConn, md5Password, role, 2)
	if err != nil {
		logs.Error(err)
		return
	}
	// 等待来自上述三个端口的返回数据，拿到对端申明的三个外网地址
	var remoteAddr1, remoteAddr2, remoteAddr3 string
	for {
		buf := make([]byte, 1024)
		if n, addr, er := localConn.ReadFromUDP(buf); er != nil {
			err = er
			return
		} else {
			rAddr2, _ := getNextAddr(rAddr, 1)
			rAddr3, _ := getNextAddr(rAddr, 2)
			switch addr.String() {
			case rAddr:
				remoteAddr1 = string(buf[:n])
			case rAddr2:
				remoteAddr2 = string(buf[:n])
			case rAddr3:
				remoteAddr3 = string(buf[:n])
			}
		}
		if remoteAddr1 != "" && remoteAddr2 != "" && remoteAddr3 != "" {
			break
		}
	}
	// 执行打洞测试，获得最终可用的对端地址
	if remoteAddress, err = sendP2PTestMsg(localConn, remoteAddr1, remoteAddr2, remoteAddr3); err != nil {
		return
	}
	// 打洞成功后再新建一个 UDP 连接供数据使用（避免复用上面的信令连接）
	c, err = newUdpConnByAddr(localAddr)
	return
}

// sendP2PTestMsg 基于三元地址进行 NAT 打洞：
// - interval=0 表示对端端口固定，直接在 remoteAddr3 +/- interval 上探测
// - interval!=0 时，在一定端口区间内并发尝试发送 CONNECT 包，等待 SUCCESS/END
// 返回值为最终确认可达的对端地址。
func sendP2PTestMsg(localConn *net.UDPConn, remoteAddr1, remoteAddr2, remoteAddr3 string) (string, error) {
	logs.Trace(remoteAddr3, remoteAddr2, remoteAddr1)
	defer localConn.Close()
	isClose := false
	defer func() { isClose = true }()
	interval, err := getAddrInterval(remoteAddr1, remoteAddr2, remoteAddr3)
	if err != nil {
		return "", err
	}
	// 固定周期向预估端口发送 CONNECT 探测包，直到协商完成或超时
	go func() {
		addr, err := getNextAddr(remoteAddr3, interval)
		if err != nil {
			return
		}
		remoteUdpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return
		}
		logs.Trace("try send test packet to target %s", addr)
		ticker := time.NewTicker(time.Millisecond * 500)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if isClose {
					return
				}
				if _, err := localConn.WriteTo([]byte(common.WORK_P2P_CONNECT), remoteUdpAddr); err != nil {
					return
				}
			}
		}
	}()
	// interval 不为 0 时，额外在端口区间内并发“撒网”
	if interval != 0 {
		ip := common.GetIpByAddr(remoteAddr2)
		go func() {
			ports := getRandomPortArr(common.GetPortByAddr(remoteAddr3), common.GetPortByAddr(remoteAddr3)+interval*50)
			for i := 0; i <= 50; i++ {
				go func(port int) {
					trueAddress := ip + ":" + strconv.Itoa(port)
					logs.Trace("try send test packet to target %s", trueAddress)
					remoteUdpAddr, err := net.ResolveUDPAddr("udp", trueAddress)
					if err != nil {
						return
					}
					ticker := time.NewTicker(time.Second * 2)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							if isClose {
								return
							}
							if _, err := localConn.WriteTo([]byte(common.WORK_P2P_CONNECT), remoteUdpAddr); err != nil {
								return
							}
						}
					}
				}(ports[i])
				time.Sleep(time.Millisecond * 10) // 轻微错峰，避免瞬时过多 goroutine 同时发送
			}
		}()

	}

	// 读取对端的响应，根据不同标记完成握手
	buf := make([]byte, 10)
	for {
		localConn.SetReadDeadline(time.Now().Add(time.Second * 10))
		n, addr, err := localConn.ReadFromUDP(buf)
		localConn.SetReadDeadline(time.Time{})
		if err != nil {
			break
		}
		switch string(buf[:n]) {
		case common.WORK_P2P_SUCCESS:
			// 收到 SUCCESS 后持续回发 END，帮助对端尽快结束握手
			for i := 20; i > 0; i-- {
				if _, err = localConn.WriteTo([]byte(common.WORK_P2P_END), addr); err != nil {
					return "", err
				}
			}
			return addr.String(), nil
		case common.WORK_P2P_END:
			logs.Trace("Remotely Address %s Reply Packet Successfully Received", addr.String())
			return addr.String(), nil
		case common.WORK_P2P_CONNECT:
			// 对端在尝试连接我们，主动回 SUCCESS 协助其完成握手
			go func() {
				for i := 20; i > 0; i-- {
					logs.Trace("try send receive success packet to target %s", addr.String())
					if _, err = localConn.WriteTo([]byte(common.WORK_P2P_SUCCESS), addr); err != nil {
						return
					}
					time.Sleep(time.Second)
				}
			}()
		default:
			continue
		}
	}
	return "", errors.New("connect to the target failed, maybe the nat type is not support p2p")
}

// newUdpConnByAddr 在本地指定地址创建 UDP 监听。
func newUdpConnByAddr(addr string) (*net.UDPConn, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	return udpConn, nil
}

// getNextAddr 在给定地址的基础上，对端口做 +n 偏移，返回新的地址字符串。
func getNextAddr(addr string, n int) (string, error) {
	arr := strings.Split(addr, ":")
	if len(arr) != 2 {
		return "", errors.New(fmt.Sprintf("the format of %s incorrect", addr))
	}
	if p, err := strconv.Atoi(arr[1]); err != nil {
		return "", err
	} else {
		return arr[0] + ":" + strconv.Itoa(p+n), nil
	}
}

// getAddrInterval 根据三个地址计算端口的“步长”与方向。
//
// 一般情况下服务端会以相同步长为两个辅助端口(r2、r3)分配端口，
// 这里取两段差值的较小值作为 interval；若第三个端口在第一端口的“左侧”，则返回负数。
func getAddrInterval(addr1, addr2, addr3 string) (int, error) {
	arr1 := strings.Split(addr1, ":")
	if len(arr1) != 2 {
		return 0, errors.New(fmt.Sprintf("the format of %s incorrect", addr1))
	}
	arr2 := strings.Split(addr2, ":")
	if len(arr2) != 2 {
		return 0, errors.New(fmt.Sprintf("the format of %s incorrect", addr2))
	}
	arr3 := strings.Split(addr3, ":")
	if len(arr3) != 2 {
		return 0, errors.New(fmt.Sprintf("the format of %s incorrect", addr3))
	}
	p1, err := strconv.Atoi(arr1[1])
	if err != nil {
		return 0, err
	}
	p2, err := strconv.Atoi(arr2[1])
	if err != nil {
		return 0, err
	}
	p3, err := strconv.Atoi(arr3[1])
	if err != nil {
		return 0, err
	}
	interVal := int(math.Floor(math.Min(math.Abs(float64(p3-p2)), math.Abs(float64(p2-p1)))))
	if p3-p1 < 0 {
		return -interVal, nil
	}
	return interVal, nil
}

// getRandomPortArr 在 [min, max] 区间内生成随机乱序的端口数组。
func getRandomPortArr(min, max int) []int {
	if min > max {
		min, max = max, min
	}
	addrAddr := make([]int, max-min+1)
	for i := min; i <= max; i++ {
		addrAddr[max-i] = i
	}
	rand.Seed(time.Now().UnixNano())
	var r, temp int
	for i := max - min; i > 0; i-- {
		r = rand.Int() % i
		temp = addrAddr[i]
		addrAddr[i] = addrAddr[r]
		addrAddr[r] = temp
	}
	return addrAddr
}
