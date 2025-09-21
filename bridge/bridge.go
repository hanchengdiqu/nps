/*
Package bridge 实现服务端与客户端之间的“桥接”层。

主要职责：
- 维护与每个客户端的三条连接：信令(signal)、转发隧道(tunnel)、文件隧道(file)。
- 校验客户端版本与验签，完成注册/配置下发。
- 为上层代理模块发送新连接指令，并与对应客户端建立复用连接。
- 维护客户端健康状态与自动摘除/恢复后端目标。
- 心跳与清理断开的客户端。
本文件只包含服务端侧的桥接逻辑，不涉及具体协议转发细节。
*/
package bridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ehang.io/nps-mux"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/version"
	"ehang.io/nps/server/connection"
	"ehang.io/nps/server/tool"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

// Client 表示一个已连接的客户端在服务端侧的三通道聚合对象。
// - signal: 用于控制面信令（认证、心跳、配置、健康上报等）
// - tunnel: 用于业务流量转发的复用隧道
// - file:   专用于文件传输/管理的复用隧道（与业务隧道隔离）
// - Version: 客户端上报的版本号（仅记录，服务端会在握手时校验）
// - retryTime: 心跳失败次数计数器；当连续>=3次检查失败则判定客户端离线。
type Client struct {
	// 业务转发隧道，基于 ehang.io/nps-mux 进行多路复用
	tunnel *nps_mux.Mux
	// 控制面信令连接
	signal *conn.Conn
	// 文件传输隧道，独立于业务隧道
	file *nps_mux.Mux
	// 客户端版本号（由客户端上报）
	Version string
	// 心跳重试计数
	retryTime int // it will be add 1 when ping not ok until to 3 will close the client
}

// NewClient 创建一个 Client 聚合对象。
// t: 业务隧道复用器；f: 文件隧道复用器；s: 信令连接；vs: 客户端版本。
func NewClient(t, f *nps_mux.Mux, s *conn.Conn, vs string) *Client {
	return &Client{
		signal:  s,
		tunnel:  t,
		file:    f,
		Version: vs,
	}
}

// Bridge 是服务端桥接核心对象，负责管理所有客户端的连接与任务。
// 字段说明：
// - TunnelPort: 监听的桥接端口（仅用于日志显示，实际端口取自配置）
// - Client: 当前已注册上线的客户端映射，key 为客户端 ID
// - Register: 通过 WORK_REGISTER 注册的 IP 白名单，带过期时间
// - tunnelType: 使用的底层传输类型（"tcp" 或 "kcp"），影响复用与保活
// - OpenTask/CloseTask: 通知上层开启/关闭某个转发任务的通道
// - CloseClient: 通知上层某客户端下线
// - SecretChan: 秘密隧道/内网穿透（secret/p2p）所需的密钥通道
// - ipVerify: 是否启用来源 IP 验证
// - runList: 正在运行的任务列表（通过 sync.Map 共享）
// - disconnectTime: 复用连接的闲置断开时间，传递给 nps-mux
type Bridge struct {
	TunnelPort     int // 通信隧道端口（日志显示）
	Client         sync.Map
	Register       sync.Map
	tunnelType     string // bridge type kcp or tcp
	OpenTask       chan *file.Tunnel
	CloseTask      chan *file.Tunnel
	CloseClient    chan int
	SecretChan     chan *conn.Secret
	ipVerify       bool
	runList        sync.Map // map[int]interface{}
	disconnectTime int
}

// NewTunnel 创建 Bridge，并初始化内部通道与参数。
// tunnelPort: 监听端口；tunnelType: "tcp"/"kcp"；ipVerify: 是否启用 IP 校验；
// runList: 运行中的任务共享表；disconnectTime: 复用连接闲置断开时间（秒）。
func NewTunnel(tunnelPort int, tunnelType string, ipVerify bool, runList sync.Map, disconnectTime int) *Bridge {
	return &Bridge{
		TunnelPort:     tunnelPort,
		tunnelType:     tunnelType,
		OpenTask:       make(chan *file.Tunnel),
		CloseTask:      make(chan *file.Tunnel),
		CloseClient:    make(chan int),
		SecretChan:     make(chan *conn.Secret),
		ipVerify:       ipVerify,
		runList:        runList,
		disconnectTime: disconnectTime,
	}
}

// StartTunnel 启动桥接监听并进入接入循环。
// - 若 tunnelType=="kcp"，则启动 KCP 监听；否则由 connection.GetBridgeListener 返回对应监听器。
// - 每个新连接都会交由 cliProcess 进行握手验证与类型分流。
func (s *Bridge) StartTunnel() error {
	go s.ping()
	if s.tunnelType == "kcp" {
		logs.Info("server start, the bridge type is %s, the bridge port is %d", s.tunnelType, s.TunnelPort)
		return conn.NewKcpListenerAndProcess(beego.AppConfig.String("bridge_ip")+":"+beego.AppConfig.String("bridge_port"), func(c net.Conn) {
			s.cliProcess(conn.NewConn(c))
		})
	} else {
		listener, err := connection.GetBridgeListener(s.tunnelType)
		if err != nil {
			logs.Error(err)
			os.Exit(0)
			return err
		}
		conn.Accept(listener, func(c net.Conn) {
			s.cliProcess(conn.NewConn(c))
		})
	}
	return nil
}

// get health information form client
// GetHealthFromClient 持续读取客户端的健康上报信息，并动态维护后端目标可用性。
// - 当 status=false 时，表示目标探测失败：从 TargetArr 中移除，并记录到 HealthRemoveArr。
// - 当 status=true 时，表示目标恢复可用：从 HealthRemoveArr 移除并重新加入 TargetArr。
// - 该方法在连接终止后会调用 DelClient 进行清理。
func (s *Bridge) GetHealthFromClient(id int, c *conn.Conn) {
	for {
		if info, status, err := c.GetHealthInfo(); err != nil {
			break
		} else if !status { //the status is true , return target to the targetArr
			file.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
				v := value.(*file.Tunnel)
				if v.Client.Id == id && v.Mode == "tcp" && strings.Contains(v.Target.TargetStr, info) {
					v.Lock()
					if v.Target.TargetArr == nil || (len(v.Target.TargetArr) == 0 && len(v.HealthRemoveArr) == 0) {
						v.Target.TargetArr = common.TrimArr(strings.Split(v.Target.TargetStr, "\n"))
					}
					v.Target.TargetArr = common.RemoveArrVal(v.Target.TargetArr, info)
					if v.HealthRemoveArr == nil {
						v.HealthRemoveArr = make([]string, 0)
					}
					v.HealthRemoveArr = append(v.HealthRemoveArr, info)
					v.Unlock()
				}
				return true
			})
			file.GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
				v := value.(*file.Host)
				if v.Client.Id == id && strings.Contains(v.Target.TargetStr, info) {
					v.Lock()
					if v.Target.TargetArr == nil || (len(v.Target.TargetArr) == 0 && len(v.HealthRemoveArr) == 0) {
						v.Target.TargetArr = common.TrimArr(strings.Split(v.Target.TargetStr, "\n"))
					}
					v.Target.TargetArr = common.RemoveArrVal(v.Target.TargetArr, info)
					if v.HealthRemoveArr == nil {
						v.HealthRemoveArr = make([]string, 0)
					}
					v.HealthRemoveArr = append(v.HealthRemoveArr, info)
					v.Unlock()
				}
				return true
			})
		} else { //the status is false,remove target from the targetArr
			file.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
				v := value.(*file.Tunnel)
				if v.Client.Id == id && v.Mode == "tcp" && common.IsArrContains(v.HealthRemoveArr, info) && !common.IsArrContains(v.Target.TargetArr, info) {
					v.Lock()
					v.Target.TargetArr = append(v.Target.TargetArr, info)
					v.HealthRemoveArr = common.RemoveArrVal(v.HealthRemoveArr, info)
					v.Unlock()
				}
				return true
			})

			file.GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
				v := value.(*file.Host)
				if v.Client.Id == id && common.IsArrContains(v.HealthRemoveArr, info) && !common.IsArrContains(v.Target.TargetArr, info) {
					v.Lock()
					v.Target.TargetArr = append(v.Target.TargetArr, info)
					v.HealthRemoveArr = common.RemoveArrVal(v.HealthRemoveArr, info)
					v.Unlock()
				}
				return true
			})
		}
	}
	s.DelClient(id)
}

// 验证失败，返回错误验证flag，并且关闭连接
func (s *Bridge) verifyError(c *conn.Conn) {
	c.Write([]byte(common.VERIFY_EER))
}

// verifySuccess 验证通过后向客户端写入成功标识。
func (s *Bridge) verifySuccess(c *conn.Conn) {
	c.Write([]byte(common.VERIFY_SUCCESS))
}

// cliProcess 处理一条新的原始连接：
// 1) 读取探测标识与版本，校验客户端与服务端版本一致性；
// 2) 完成验签（VerifyKey），根据客户端类型旗标分流到不同处理：
//   - WORK_MAIN: 建立信令连接；
//   - WORK_CHAN: 建立业务隧道复用连接；
//   - WORK_CONFIG: 进入配置通道，接收/下发配置；
//   - WORK_REGISTER: 记录 IP 验证信息；
//   - WORK_SECRET: secret 通道；
//   - WORK_FILE: 建立文件传输复用连接；
//   - WORK_P2P: P2P 握手与信息转发。
func (s *Bridge) cliProcess(c *conn.Conn) {
	//read test flag
	if _, err := c.GetShortContent(3); err != nil {
		logs.Info("The client %s connect error", c.Conn.RemoteAddr(), err.Error())
		return
	}
	//version check
	if b, err := c.GetShortLenContent(); err != nil || string(b) != version.GetVersion() {
		logs.Info("The client %s version does not match", c.Conn.RemoteAddr())
		c.Close()
		return
	}
	//version get
	var vs []byte
	var err error
	if vs, err = c.GetShortLenContent(); err != nil {
		logs.Info("get client %s version error", err.Error())
		c.Close()
		return
	}
	//write server version to client
	c.Write([]byte(crypt.Md5(version.GetVersion())))
	c.SetReadDeadlineBySecond(5)
	var buf []byte
	//get vKey from client
	if buf, err = c.GetShortContent(32); err != nil {
		c.Close()
		return
	}
	//verify
	id, err := file.GetDb().GetIdByVerifyKey(string(buf), c.Conn.RemoteAddr().String())
	if err != nil {
		logs.Info("Current client connection validation error, close this client:", c.Conn.RemoteAddr())
		s.verifyError(c)
		return
	} else {
		s.verifySuccess(c)
	}
	if flag, err := c.ReadFlag(); err == nil {
		s.typeDeal(flag, c, id, string(vs))
	} else {
		logs.Warn(err, flag)
	}
	return
}

// DelClient 将指定客户端从连接表中移除，并关闭信令连接；
// 如果该客户端不是公用客户端，还会推送 CloseClient 事件供上层清理任务。
func (s *Bridge) DelClient(id int) {
	if v, ok := s.Client.Load(id); ok {
		if v.(*Client).signal != nil {
			v.(*Client).signal.Close()
		}
		s.Client.Delete(id)
		if file.GetDb().IsPubClient(id) {
			return
		}
		if c, err := file.GetDb().GetClient(id); err == nil {
			s.CloseClient <- c.Id
		}
	}
}

// use different
// typeDeal 根据客户端上报的工作类型（flag）决定后续处理方式。
// 各分支说明：
// - WORK_MAIN: 建立/更新信令连接，并启动健康信息读取；
// - WORK_CHAN: 建立业务隧道复用连接；
// - WORK_CONFIG: 进入配置模式，允许新建客户端/任务/Host 等；
// - WORK_REGISTER: 记录来源 IP 的白名单有效时间；
// - WORK_SECRET: 接收 secret 密钥并转发到 SecretChan；
// - WORK_FILE: 建立文件传输复用连接；
// - WORK_P2P: 处理 P2P 握手，向对应客户端与请求方下发必要信息。
func (s *Bridge) typeDeal(typeVal string, c *conn.Conn, id int, vs string) {
	isPub := file.GetDb().IsPubClient(id)
	switch typeVal {
	case common.WORK_MAIN:
		if isPub {
			c.Close()
			return
		}
		tcpConn, ok := c.Conn.(*net.TCPConn)
		if ok {
			// add tcp keep alive option for signal connection
			_ = tcpConn.SetKeepAlive(true)
			_ = tcpConn.SetKeepAlivePeriod(5 * time.Second)
		}
		//the vKey connect by another ,close the client of before
		if v, ok := s.Client.LoadOrStore(id, NewClient(nil, nil, c, vs)); ok {
			if v.(*Client).signal != nil {
				v.(*Client).signal.WriteClose()
			}
			v.(*Client).signal = c
			v.(*Client).Version = vs
		}
		go s.GetHealthFromClient(id, c)
		logs.Info("clientId %d connection succeeded, address:%s ", id, c.Conn.RemoteAddr())
	case common.WORK_CHAN:
		muxConn := nps_mux.NewMux(c.Conn, s.tunnelType, s.disconnectTime)
		if v, ok := s.Client.LoadOrStore(id, NewClient(muxConn, nil, nil, vs)); ok {
			v.(*Client).tunnel = muxConn
		}
	case common.WORK_CONFIG:
		client, err := file.GetDb().GetClient(id)
		if err != nil || (!isPub && !client.ConfigConnAllow) {
			c.Close()
			return
		}
		binary.Write(c, binary.LittleEndian, isPub)
		go s.getConfig(c, isPub, client)
	case common.WORK_REGISTER:
		go s.register(c)
	case common.WORK_SECRET:
		if b, err := c.GetShortContent(32); err == nil {
			s.SecretChan <- conn.NewSecret(string(b), c)
		} else {
			logs.Error("secret error, failed to match the key successfully")
		}
	case common.WORK_FILE:
		muxConn := nps_mux.NewMux(c.Conn, s.tunnelType, s.disconnectTime)
		if v, ok := s.Client.LoadOrStore(id, NewClient(nil, muxConn, nil, vs)); ok {
			v.(*Client).file = muxConn
		}
	case common.WORK_P2P:
		//read md5 secret
		if b, err := c.GetShortContent(32); err != nil {
			logs.Error("p2p error,", err.Error())
		} else if t := file.GetDb().GetTaskByMd5Password(string(b)); t == nil {
			logs.Error("p2p error, failed to match the key successfully")
		} else {
			if v, ok := s.Client.Load(t.Client.Id); !ok {
				return
			} else {
				//向密钥对应的客户端发送与服务端udp建立连接信息，地址，密钥
				v.(*Client).signal.Write([]byte(common.NEW_UDP_CONN))
				svrAddr := beego.AppConfig.String("p2p_ip") + ":" + beego.AppConfig.String("p2p_port")
				if err != nil {
					logs.Warn("get local udp addr error")
					return
				}
				v.(*Client).signal.WriteLenContent([]byte(svrAddr))
				v.(*Client).signal.WriteLenContent(b)
				//向该请求者发送建立连接请求,服务器地址
				c.WriteLenContent([]byte(svrAddr))
			}
		}
	}
	c.SetAlive(s.tunnelType)
	return
}

// register ip
// register 记录客户端来源 IP 的白名单有效期（小时）。
// 客户端通过 WORK_REGISTER 发送一个 int32 的小时数，服务端据此计算过期时间。
func (s *Bridge) register(c *conn.Conn) {
	var hour int32
	if err := binary.Read(c, binary.LittleEndian, &hour); err == nil {
		s.Register.Store(common.GetIpByAddr(c.Conn.RemoteAddr().String()), time.Now().Add(time.Hour*time.Duration(hour)))
	}
}

// SendLinkInfo 为上层代理模块建立到客户端侧的目标连接：
// - 若 link.LocalProxy=true，直接在本地拨号到 link.Host；
// - 否则通过对应客户端的复用隧道建立一条新连接，并发送 Link 元信息；
// - 当 t.Mode=="file" 时，禁用加密与压缩，以优化文件传输；
// - 如启用了 IP 白名单验证，将校验请求来源的 RemoteAddr 是否在有效期内。
func (s *Bridge) SendLinkInfo(clientId int, link *conn.Link, t *file.Tunnel) (target net.Conn, err error) {
	//if the proxy type is local
	if link.LocalProxy {
		target, err = net.Dial("tcp", link.Host)
		return
	}
	if v, ok := s.Client.Load(clientId); ok {
		//If ip is restricted to do ip verification
		if s.ipVerify {
			ip := common.GetIpByAddr(link.RemoteAddr)
			if v, ok := s.Register.Load(ip); !ok {
				return nil, errors.New(fmt.Sprintf("The ip %s is not in the validation list", ip))
			} else {
				if !v.(time.Time).After(time.Now()) {
					return nil, errors.New(fmt.Sprintf("The validity of the ip %s has expired", ip))
				}
			}
		}
		var tunnel *nps_mux.Mux
		if t != nil && t.Mode == "file" {
			tunnel = v.(*Client).file
		} else {
			tunnel = v.(*Client).tunnel
		}
		if tunnel == nil {
			err = errors.New("the client connect error")
			return
		}
		if target, err = tunnel.NewConn(); err != nil {
			return
		}
		if t != nil && t.Mode == "file" {
			//TODO if t.mode is file ,not use crypt or compress
			link.Crypt = false
			link.Compress = false
			return
		}
		if _, err = conn.NewConn(target).SendInfo(link, ""); err != nil {
			logs.Info("new connect error ,the target %s refuse to connect", link.Host)
			return
		}
	} else {
		err = errors.New(fmt.Sprintf("the client %d is not connect", clientId))
	}
	return
}

// ping 定时巡检所有客户端连接状态：
// - 若缺失必要通道（signal/tunnel），连续 3 次检查失败后判定离线；
// - 若发现多路复用隧道已关闭，也会触发下线；
// - 下线时将调用 DelClient 进行统一清理。
func (s *Bridge) ping() {
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			arr := make([]int, 0)
			s.Client.Range(func(key, value interface{}) bool {
				v := value.(*Client)
				if v.tunnel == nil || v.signal == nil {
					v.retryTime += 1
					if v.retryTime >= 3 {
						arr = append(arr, key.(int))
					}
					return true
				}
				if v.tunnel.IsClose {
					arr = append(arr, key.(int))
				}
				return true
			})
			for _, v := range arr {
				logs.Info("the client %d closed", v)
				s.DelClient(v)
			}
		}
	}
}

// get config and add task from client config
// getConfig 处理来自客户端的配置交互：
// - WORK_STATUS: 返回该客户端当前运行的 Host/Task 备注列表；
// - NEW_CONF: 新建客户端配置并返回 VerifyKey；
// - NEW_HOST: 新增或复用一个 Host；
// - NEW_TASK: 新增一组/多个转发任务，并根据模式校验端口与目标对应关系。
// 在处理过程中如遇失败，将向客户端返回失败标记并中断；必要时会触发 DelClient。
func (s *Bridge) getConfig(c *conn.Conn, isPub bool, client *file.Client) {
	var fail bool
loop:
	for {
		flag, err := c.ReadFlag()
		if err != nil {
			break
		}
		switch flag {
		case common.WORK_STATUS:
			if b, err := c.GetShortContent(32); err != nil {
				break loop
			} else {
				var str string
				id, err := file.GetDb().GetClientIdByVkey(string(b))
				if err != nil {
					break loop
				}
				file.GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
					v := value.(*file.Host)
					if v.Client.Id == id {
						str += v.Remark + common.CONN_DATA_SEQ
					}
					return true
				})
				file.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
					v := value.(*file.Tunnel)
					//if _, ok := s.runList[v.Id]; ok && v.Client.Id == id {
					if _, ok := s.runList.Load(v.Id); ok && v.Client.Id == id {
						str += v.Remark + common.CONN_DATA_SEQ
					}
					return true
				})
				binary.Write(c, binary.LittleEndian, int32(len([]byte(str))))
				binary.Write(c, binary.LittleEndian, []byte(str))
			}
		case common.NEW_CONF:
			var err error
			if client, err = c.GetConfigInfo(); err != nil {
				fail = true
				c.WriteAddFail()
				break loop
			} else {
				if err = file.GetDb().NewClient(client); err != nil {
					fail = true
					c.WriteAddFail()
					break loop
				}
				c.WriteAddOk()
				c.Write([]byte(client.VerifyKey))
				s.Client.Store(client.Id, NewClient(nil, nil, nil, ""))
			}
		case common.NEW_HOST:
			h, err := c.GetHostInfo()
			if err != nil {
				fail = true
				c.WriteAddFail()
				break loop
			}
			h.Client = client
			if h.Location == "" {
				h.Location = "/"
			}
			if !client.HasHost(h) {
				if file.GetDb().IsHostExist(h) {
					fail = true
					c.WriteAddFail()
					break loop
				} else {
					file.GetDb().NewHost(h)
					c.WriteAddOk()
				}
			} else {
				c.WriteAddOk()
			}
		case common.NEW_TASK:
			if t, err := c.GetTaskInfo(); err != nil {
				fail = true
				c.WriteAddFail()
				break loop
			} else {
				ports := common.GetPorts(t.Ports)
				targets := common.GetPorts(t.Target.TargetStr)
				if len(ports) > 1 && (t.Mode == "tcp" || t.Mode == "udp") && (len(ports) != len(targets)) {
					fail = true
					c.WriteAddFail()
					break loop
				} else if t.Mode == "secret" || t.Mode == "p2p" {
					ports = append(ports, 0)
				}
				if len(ports) == 0 {
					fail = true
					c.WriteAddFail()
					break loop
				}
				for i := 0; i < len(ports); i++ {
					tl := new(file.Tunnel)
					tl.Mode = t.Mode
					tl.Port = ports[i]
					tl.ServerIp = t.ServerIp
					if len(ports) == 1 {
						tl.Target = t.Target
						tl.Remark = t.Remark
					} else {
						tl.Remark = t.Remark + "_" + strconv.Itoa(tl.Port)
						tl.Target = new(file.Target)
						if t.TargetAddr != "" {
							tl.Target.TargetStr = t.TargetAddr + ":" + strconv.Itoa(targets[i])
						} else {
							tl.Target.TargetStr = strconv.Itoa(targets[i])
						}
					}
					tl.Id = int(file.GetDb().JsonDb.GetTaskId())
					tl.Status = true
					tl.Flow = new(file.Flow)
					tl.NoStore = true
					tl.Client = client
					tl.Password = t.Password
					tl.LocalPath = t.LocalPath
					tl.StripPre = t.StripPre
					tl.MultiAccount = t.MultiAccount
					if !client.HasTunnel(tl) {
						if err := file.GetDb().NewTask(tl); err != nil {
							logs.Notice("Add task error ", err.Error())
							fail = true
							c.WriteAddFail()
							break loop
						}
						if b := tool.TestServerPort(tl.Port, tl.Mode); !b && t.Mode != "secret" && t.Mode != "p2p" {
							fail = true
							c.WriteAddFail()
							break loop
						} else {
							s.OpenTask <- tl
						}
					}
					c.WriteAddOk()
				}
			}
		}
	}
	if fail && client != nil {
		s.DelClient(client.Id)
	}
	c.Close()
}
