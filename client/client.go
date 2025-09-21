// Package client 实现了 NPC 客户端与 NPS 服务端交互的核心逻辑。
// 本文件负责：
// 1) 建立与服务端的控制信道（signal）与数据复用隧道（tunnel）；
// 2) 根据服务端指令建立新通道，转发 TCP/UDP/HTTP 流量；
// 3) 支持 UDP 打洞的 P2P 连接与 KCP 会话复用；
// 4) 维护心跳与健康检查，并在异常时进行资源清理与重连。
package client

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"ehang.io/nps-mux"

	"github.com/astaxie/beego/logs"
	"github.com/xtaci/kcp-go"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/config"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/crypt"
)

// TRPClient 表示一个与 NPS 服务端交互的客户端实例。
// 字段说明：
// - svrAddr: 服务端地址，形如 host:port；
// - bridgeConnType: 传输桥接类型（如 tcp、kcp、websocket 等），影响底层复用与编解码；
// - proxyUrl: 当需要通过 HTTP/HTTPS 代理连接服务端时的代理地址；
// - vKey: 认证所用的校验 key；
// - p2pAddr: UDP 打洞过程中，用于在短时间窗口内复用本地端口的缓存；
// - tunnel: 复用隧道（pmux/nps-mux），用于承载多路数据连接；
// - signal: 与服务端的主控制连接，用于接收控制指令（如 NEW_UDP_CONN 等）；
// - ticker: 心跳检测定时器；
// - cnf: 客户端配置，包含健康检查项等；
// - disconnectTime: 复用隧道的空闲断连时间（秒）；
// - once: 保证 Close/closing 只执行一次。
type TRPClient struct {
	svrAddr        string
	bridgeConnType string
	proxyUrl       string
	vKey           string
	p2pAddr        map[string]string
	tunnel         *nps_mux.Mux
	signal         *conn.Conn
	ticker         *time.Ticker
	cnf            *config.Config
	disconnectTime int
	once           sync.Once
}

// NewRPClient 创建并返回一个 TRPClient。
// 参数：
// - svraddr: 服务端地址（host:port）。
// - vKey: 与服务端匹配的验证 key。
// - bridgeConnType: 连接/复用类型（影响底层传输）。
// - proxyUrl: 可选，若需要走代理访问服务端。
// - cnf: 运行配置，含健康检查项等。
// - disconnectTime: 复用隧道空闲断开时间（秒）。
// 返回：
// - *TRPClient: 未连接状态的客户端实例，需调用 Start 启动。
// 注意：本函数仅做初始化，不会进行网络连接。
func NewRPClient(svraddr string, vKey string, bridgeConnType string, proxyUrl string, cnf *config.Config, disconnectTime int) *TRPClient {
	return &TRPClient{
		svrAddr:        svraddr,
		p2pAddr:        make(map[string]string, 0),
		vKey:           vKey,
		bridgeConnType: bridgeConnType,
		proxyUrl:       proxyUrl,
		cnf:            cnf,
		disconnectTime: disconnectTime,
		once:           sync.Once{},
	}
}

// NowStatus 表示当前客户端状态：
// 0 = 未就绪/断开；1 = 已连接并就绪。
var NowStatus int

// CloseClient 标志用于外部或内部控制客户端的退出。
// Start 会在每次循环前检查该值，若为 true 则停止重连与后续流程。
var CloseClient bool

// Start 启动客户端主流程：
// 1) 通过 NewConn 建立与服务端的控制连接（WORK_MAIN）；
// 2) 并发启动 ping() 监控复用隧道状态；
// 3) 并发建立数据复用通道 newChan()（WORK_CHAN）；
// 4) 若配置中开启健康检查，则启动 heathCheck；
// 5) 进入 handleMain() 循环处理来自服务端的控制消息（如 NEW_UDP_CONN）。
// 发生错误时会等待 5 秒重试，直到 CloseClient 被置为 true。
// 注意：该方法会阻塞在 handleMain()，需要在独立 goroutine 中调用或在主线程按需处理。
// 线程安全：内部使用 once 确保 Close 仅执行一次。
// 重连策略：遇到网络错误或收到非法数据时，进入 retry 标签重新连接。
func (s *TRPClient) Start() {
	CloseClient = false
retry:
	if CloseClient {
		return
	}
	NowStatus = 0
	c, err := NewConn(s.bridgeConnType, s.vKey, s.svrAddr, common.WORK_MAIN, s.proxyUrl)
	if err != nil {
		logs.Error("The connection server failed and will be reconnected in five seconds, error", err.Error())
		time.Sleep(time.Second * 5)
		goto retry
	}
	if c == nil {
		logs.Error("Error data from server, and will be reconnected in five seconds")
		time.Sleep(time.Second * 5)
		goto retry
	}
	logs.Info("Successful connection with server %s", s.svrAddr)
	//monitor the connection
	go s.ping()
	s.signal = c
	//start a channel connection
	go s.newChan()
	//start health check if the it's open
	if s.cnf != nil && len(s.cnf.Healths) > 0 {
		go heathCheck(s.cnf.Healths, s.signal)
	}
	NowStatus = 1
	//msg connection, eg udp
	s.handleMain()
}

// handleMain 处理来自服务端控制连接（signal）的消息。
// 目前主要处理 NEW_UDP_CONN，用于触发 P2P UDP 打洞：
// - 读取服务端下发的远端 UDP 地址与口令；
// - 以时间片为单位复用本地端口（降低 NAT 映射变化带来的失败率）；
// - 异步调用 newUdpConn 发起打洞与后续的 KCP+Mux 建链。
func (s *TRPClient) handleMain() {
	for {
		flags, err := s.signal.ReadFlag()
		if err != nil {
			logs.Error("Accept server data error %s, end this service", err.Error())
			break
		}
		switch flags {
		case common.NEW_UDP_CONN:
			//read server udp addr and password
			if lAddr, err := s.signal.GetShortLenContent(); err != nil {
				logs.Warn(err)
				return
			} else if pwd, err := s.signal.GetShortLenContent(); err == nil {
				var localAddr string
				//The local port remains unchanged for a certain period of time
				if v, ok := s.p2pAddr[crypt.Md5(string(pwd)+strconv.Itoa(int(time.Now().Unix()/100)))]; !ok {
					tmpConn, err := common.GetLocalUdpAddr()
					if err != nil {
						logs.Error(err)
						return
					}
					localAddr = tmpConn.LocalAddr().String()
				} else {
					localAddr = v
				}
				go s.newUdpConn(localAddr, string(lAddr), string(pwd))
			}
		}
	}
	s.Close()
}

// newUdpConn 负责基于服务端下发的信息发起 UDP 打洞：
// 1) 通过 handleP2PUdp 完成打洞并获得可用的本地 PacketConn 与远端地址；
// 2) 使用 kcp-go 基于该 PacketConn 建立 KCP 监听（ServeConn）；
// 3) 接受来自对端的 KCP 会话，校验远端地址后，
// 4) 将 KCP 会话交给 nps-mux，作为复用隧道承载新建连接；
// 5) 由 conn.Accept 驱动，进入 handleChan 处理每条上层连接。
func (s *TRPClient) newUdpConn(localAddr, rAddr string, md5Password string) {
	var localConn net.PacketConn
	var err error
	var remoteAddress string
	if remoteAddress, localConn, err = handleP2PUdp(localAddr, rAddr, md5Password, common.WORK_P2P_PROVIDER); err != nil {
		logs.Error(err)
		return
	}
	l, err := kcp.ServeConn(nil, 150, 3, localConn)
	if err != nil {
		logs.Error(err)
		return
	}
	logs.Trace("start local p2p udp listen, local address", localConn.LocalAddr().String())
	for {
		udpTunnel, err := l.AcceptKCP()
		if err != nil {
			logs.Error(err)
			l.Close()
			return
		}
		if udpTunnel.RemoteAddr().String() == string(remoteAddress) {
			conn.SetUdpSession(udpTunnel)
			logs.Trace("successful connection with client ,address %s", udpTunnel.RemoteAddr().String())
			//read link info from remote
			conn.Accept(nps_mux.NewMux(udpTunnel, s.bridgeConnType, s.disconnectTime), func(c net.Conn) {
				go s.handleChan(c)
			})
			break
		}
	}
}

// newChan 建立用于数据转发的复用隧道（WORK_CHAN）。
// 步骤：
// - 通过 NewConn 连接服务端的通道端口；
// - 使用 nps-mux 对底层连接进行复用；
// - 循环 Accept 新的逻辑连接，并交由 handleChan 处理；
// - 若 Accept 出错（隧道断开），调用 Close 进行清理并退出。
// 注意：该函数在独立 goroutine 中运行。
func (s *TRPClient) newChan() {
	tunnel, err := NewConn(s.bridgeConnType, s.vKey, s.svrAddr, common.WORK_CHAN, s.proxyUrl)
	if err != nil {
		logs.Error("connect to ", s.svrAddr, "error:", err)
		return
	}
	s.tunnel = nps_mux.NewMux(tunnel.Conn, s.bridgeConnType, s.disconnectTime)
	for {
		src, err := s.tunnel.Accept()
		if err != nil {
			logs.Warn(err)
			s.Close()
			break
		}
		go s.handleChan(src)
	}
}

// handleChan 处理一条由隧道接入的新逻辑连接：
// - 首先从连接中读取 LinkInfo（目标地址、连接类型、编解码选项等）；
// - 对 HTTP 连接做特殊处理：日志记录请求行，并将请求转发至目标；
// - 对 UDP5（ Socks5-UDP 拓展协议）连接交由 handleUdp 处理；
// - 对 TCP/UDP 连接，直连目标并做双向拷贝，支持加密/压缩；
// - 发生错误或目标不可达时，及时关闭源连接并记录日志。
func (s *TRPClient) handleChan(src net.Conn) {
	lk, err := conn.NewConn(src).GetLinkInfo()
	if err != nil || lk == nil {
		src.Close()
		logs.Error("get connection info from server error ", err)
		return
	}
	//host for target processing
	lk.Host = common.FormatAddress(lk.Host)
	//if Conn type is http, read the request and log
	if lk.ConnType == "http" {
		if targetConn, err := net.DialTimeout(common.CONN_TCP, lk.Host, lk.Option.Timeout); err != nil {
			logs.Warn("connect to %s error %s", lk.Host, err.Error())
			src.Close()
		} else {
			srcConn := conn.GetConn(src, lk.Crypt, lk.Compress, nil, false)
			go func() {
				common.CopyBuffer(srcConn, targetConn)
				srcConn.Close()
				targetConn.Close()
			}()
			for {
				if r, err := http.ReadRequest(bufio.NewReader(srcConn)); err != nil {
					srcConn.Close()
					targetConn.Close()
					break
				} else {
					logs.Trace("http request, method %s, host %s, url %s, remote address %s", r.Method, r.Host, r.URL.Path, r.RemoteAddr)
					r.Write(targetConn)
				}
			}
		}
		return
	}
	if lk.ConnType == "udp5" {
		logs.Trace("new %s connection with the goal of %s, remote address:%s", lk.ConnType, lk.Host, lk.RemoteAddr)
		s.handleUdp(src)
	}
	//connect to target if conn type is tcp or udp
	if targetConn, err := net.DialTimeout(lk.ConnType, lk.Host, lk.Option.Timeout); err != nil {
		logs.Warn("connect to %s error %s", lk.Host, err.Error())
		src.Close()
	} else {
		logs.Trace("new %s connection with the goal of %s, remote address:%s", lk.ConnType, lk.Host, lk.RemoteAddr)
		conn.CopyWaitGroup(src, targetConn, lk.Crypt, lk.Compress, nil, nil, false, nil)
	}
}

// handleUdp 实现 UDP Relay：
// - 在本地绑定一个临时 UDP 端口，供上游应用发送流量；
// - 从本地 UDP 读取数据 -> 打包为自定义 UDPDatagram -> 通过 serverConn 发送至服务端；
// - 从 serverConn 读取数据 -> 解包 UDPDatagram -> 写回本地 UDP；
// - 发生任何读写错误时，及时关闭相关连接。
func (s *TRPClient) handleUdp(serverConn net.Conn) {
	// bind a local udp port
	local, err := net.ListenUDP("udp", nil)
	defer serverConn.Close()
	if err != nil {
		logs.Error("bind local udp port error ", err.Error())
		return
	}
	defer local.Close()
	go func() {
		defer serverConn.Close()
		b := common.BufPoolUdp.Get().([]byte)
		defer common.BufPoolUdp.Put(b)
		for {
			n, raddr, err := local.ReadFrom(b)
			if err != nil {
				logs.Error("read data from remote server error", err.Error())
			}
			buf := bytes.Buffer{}
			dgram := common.NewUDPDatagram(common.NewUDPHeader(0, 0, common.ToSocksAddr(raddr)), b[:n])
			dgram.Write(&buf)
			b, err := conn.GetLenBytes(buf.Bytes())
			if err != nil {
				logs.Warn("get len bytes error", err.Error())
				continue
			}
			if _, err := serverConn.Write(b); err != nil {
				logs.Error("write data to remote  error", err.Error())
				return
			}
		}
	}()
	b := common.BufPoolUdp.Get().([]byte)
	defer common.BufPoolUdp.Put(b)
	for {
		n, err := serverConn.Read(b)
		if err != nil {
			logs.Error("read udp data from server error ", err.Error())
			return
		}

		udpData, err := common.ReadUDPDatagram(bytes.NewReader(b[:n]))
		if err != nil {
			logs.Error("unpack data error", err.Error())
			return
		}
		raddr, err := net.ResolveUDPAddr("udp", udpData.Header.Addr.String())
		if err != nil {
			logs.Error("build remote addr err", err.Error())
			continue // drop silently
		}
		_, err = local.WriteTo(udpData.Data, raddr)
		if err != nil {
			logs.Error("write data to remote ", raddr.String(), "error", err.Error())
			return
		}
	}
}

// ping 定期检查复用隧道是否已关闭：
// - 每 5 秒检查一次；
// - 若发现 tunnel.IsClose 为 true，则调用 Close 触发统一清理并退出循环；
// - 该方法通常与 Start 并发运行。
// 设计目的：避免通道异常断开后，客户端仍长时间处于假在线状态。
func (s *TRPClient) ping() {
	s.ticker = time.NewTicker(time.Second * 5)
loop:
	for {
		select {
		case <-s.ticker.C:
			if s.tunnel != nil && s.tunnel.IsClose {
				s.Close()
				break loop
			}
		}
	}
}

// Close 是对外暴露的关闭入口，保证只执行一次清理逻辑。
// 内部通过 sync.Once 触发 closing()，避免重复关闭导致的崩溃或竞态。
func (s *TRPClient) Close() {
	s.once.Do(s.closing)
}

// closing 执行实际的资源释放：
// - 将 CloseClient 置为 true，阻止后续重连；
// - 重置 NowStatus；
// - 关闭数据隧道 tunnel 与控制连接 signal；
// - 停止心跳定时器 ticker。
// 该方法仅应由 Close() 通过 once 调用。
func (s *TRPClient) closing() {
	CloseClient = true
	NowStatus = 0
	if s.tunnel != nil {
		_ = s.tunnel.Close()
	}
	if s.signal != nil {
		_ = s.signal.Close()
	}
	if s.ticker != nil {
		s.ticker.Stop()
	}
}
