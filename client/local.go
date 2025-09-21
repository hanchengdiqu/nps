// Package client 实现了 npc 客户端侧的本地监听、P2P 穿透、
// 与服务端会话复用等逻辑。该文件负责启动本地监听端口、
// 维护与服务端之间的控制/数据通道，并在必要时回退到通过服务端中转。
package client

import (
	"ehang.io/nps-mux"
	"errors"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/config"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server/proxy"
	"github.com/astaxie/beego/logs"
	"github.com/xtaci/kcp-go"
)

// 全局状态变量
// LocalServer: 本地 TCP 监听器集合，用于在不同模式下对外提供本地端口
// udpConn: 与服务端/对端进行 P2P 打洞后建立的基于 KCP 的 UDP 会话
// muxSession: 在 udpConn 之上建立的多路复用会话，用于复用多条逻辑连接
// fileServer: 本地文件服务的 http.Server 列表，便于统一关闭
// p2pNetBridge: 适配 proxy 层的桥接器，实现发送链路信息以建立转发
// lock: 保护 udpConn/muxSession 等共享状态的互斥锁
// udpConnStatus: UDP 通道是否处于可用状态的标记
var (
	LocalServer   []*net.TCPListener
	udpConn       net.Conn
	muxSession    *nps_mux.Mux
	fileServer    []*http.Server
	p2pNetBridge  *p2pBridge
	lock          sync.RWMutex
	udpConnStatus bool
)

// p2pBridge 实现 proxy 侧期望的 Bridge 接口能力，
// 用于通过已建立的 muxSession 新建连接并向服务端发送 Link 信息，
// 从而完成 P2P 访客与服务端/目标之间的桥接。
type p2pBridge struct {
}

// SendLinkInfo 在已建立的 muxSession 上创建一条新的逻辑连接，
// 将链路信息发送给服务端，请求为当前访客连接分配目标端。
// 返回：
// - target: 与服务端（或对端）建立的可读写连接
// - err: 发生错误时返回
func (p2pBridge *p2pBridge) SendLinkInfo(clientId int, link *conn.Link, t *file.Tunnel) (target net.Conn, err error) {
	for i := 0; muxSession == nil; i++ {
		if i >= 20 {
			err = errors.New("p2pBridge:too many times to get muxSession")
			logs.Error(err)
			return
		}
		runtime.Gosched() // waiting for another goroutine establish the mux connection
	}
	nowConn, err := muxSession.NewConn()
	if err != nil {
		udpConn = nil
		return nil, err
	}
	if _, err := conn.NewConn(nowConn).SendInfo(link, ""); err != nil {
		udpConnStatus = false
		return nil, err
	}
	return nowConn, nil
}

// CloseLocalServer 关闭当前进程内启动的所有本地监听器与文件服务器。
// 注意：该方法不会关闭与服务端已建立的会话，仅用于清理本地监听资源。
func CloseLocalServer() {
	for _, v := range LocalServer {
		v.Close()
	}
	for _, v := range fileServer {
		v.Close()
	}
}

// startLocalFileServer 在远端通过 mux 复用构建一个 HTTP 文件服务，
// 将本地目录 t.LocalPath 通过 strip prefix 的方式暴露给远端访问。
func startLocalFileServer(config *config.CommonConfig, t *file.Tunnel, vkey string) {
	remoteConn, err := NewConn(config.Tp, vkey, config.Server, common.WORK_FILE, config.ProxyUrl)
	if err != nil {
		logs.Error("Local connection server failed ", err.Error())
		return
	}
	srv := &http.Server{
		Handler: http.StripPrefix(t.StripPre, http.FileServer(http.Dir(t.LocalPath))),
	}
	logs.Info("start local file system, local path %s, strip prefix %s ,remote port %s ", t.LocalPath, t.StripPre, t.Ports)
	fileServer = append(fileServer, srv)
	listener := nps_mux.NewMux(remoteConn.Conn, common.CONN_TCP, config.DisconnectTime)
	logs.Error(srv.Serve(listener))
}

// StartLocalServer 根据配置启动本地监听：
// - p2ps: 本地 socks5 代理（通过 p2pBridge 与远端交互）
// - p2pt: 本地 tcp 隧道转发（通过 p2pBridge 与远端交互）
// - p2p/secret: 本地 tcp 监听，分别走 P2P 或密钥校验后通过服务端中转
func StartLocalServer(l *config.LocalServer, config *config.CommonConfig) error {
	if l.Type != "secret" {
		go handleUdpMonitor(config, l)
	}
	task := &file.Tunnel{
		Port:     l.Port,
		ServerIp: "0.0.0.0",
		Status:   true,
		Client: &file.Client{
			Cnf: &file.Config{
				U:        "",
				P:        "",
				Compress: config.Client.Cnf.Compress,
			},
			Status:    true,
			RateLimit: 0,
			Flow:      &file.Flow{},
		},
		Flow:   &file.Flow{},
		Target: &file.Target{},
	}
	switch l.Type {
	case "p2ps":
		logs.Info("successful start-up of local socks5 monitoring, port", l.Port)
		return proxy.NewSock5ModeServer(p2pNetBridge, task).Start()
	case "p2pt":
		logs.Info("successful start-up of local tcp trans monitoring, port", l.Port)
		return proxy.NewTunnelModeServer(proxy.HandleTrans, p2pNetBridge, task).Start()
	case "p2p", "secret":
		listener, err := net.ListenTCP("tcp", &net.TCPAddr{net.ParseIP("0.0.0.0"), l.Port, ""})
		if err != nil {
			logs.Error("local listener startup failed port %d, error %s", l.Port, err.Error())
			return err
		}
		LocalServer = append(LocalServer, listener)
		logs.Info("successful start-up of local tcp monitoring, port", l.Port)
		conn.Accept(listener, func(c net.Conn) {
			logs.Trace("new %s connection", l.Type)
			if l.Type == "secret" {
				handleSecret(c, config, l)
			} else if l.Type == "p2p" {
				handleP2PVisitor(c, config, l)
			}
		})
	}
	return nil
}

// handleUdpMonitor 以 1 秒为周期检查 UDP 通道状态，
// 若不可用则尝试重新打洞建立新的 UDP/KCP 会话。
func handleUdpMonitor(config *config.CommonConfig, l *config.LocalServer) {
	ticker := time.NewTicker(time.Second * 1)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !udpConnStatus {
				udpConn = nil
				tmpConn, err := common.GetLocalUdpAddr()
				if err != nil {
					logs.Error(err)
					return
				}
				for i := 0; i < 10; i++ {
					logs.Notice("try to connect to the server", i+1)
					newUdpConn(tmpConn.LocalAddr().String(), config, l)
					if udpConn != nil {
						udpConnStatus = true
						break
					}
				}
			}
		}
	}
}

// handleSecret 走“密钥认证+服务端中转”的方式转发当前本地连接。
// 步骤：
// 1) 与服务端建立 WORK_SECRET 类型连接
// 2) 发送密码的 MD5 校验值
// 3) 成功后将本地连接与远端连接进行数据转发
func handleSecret(localTcpConn net.Conn, config *config.CommonConfig, l *config.LocalServer) {
	remoteConn, err := NewConn(config.Tp, config.VKey, config.Server, common.WORK_SECRET, config.ProxyUrl)
	if err != nil {
		logs.Error("Local connection server failed ", err.Error())
		return
	}
	if _, err := remoteConn.Write([]byte(crypt.Md5(l.Password))); err != nil {
		logs.Error("Local connection server failed ", err.Error())
		return
	}
	conn.CopyWaitGroup(remoteConn.Conn, localTcpConn, false, false, nil, nil, false, nil)
}

// handleP2PVisitor 为本地到来的 TCP 访客尝试使用 P2P 直连；
// 若当前未建立 UDP 通道，则自动回退至 handleSecret 通过服务端中转。
func handleP2PVisitor(localTcpConn net.Conn, config *config.CommonConfig, l *config.LocalServer) {
	if udpConn == nil {
		logs.Notice("new conn, P2P can not penetrate successfully, traffic will be transferred through the server")
		handleSecret(localTcpConn, config, l)
		return
	}
	logs.Trace("start trying to connect with the server")
	//TODO just support compress now because there is not tls file in client packages
	link := conn.NewLink(common.CONN_TCP, l.Target, false, config.Client.Cnf.Compress, localTcpConn.LocalAddr().String(), false)
	if target, err := p2pNetBridge.SendLinkInfo(0, link, nil); err != nil {
		logs.Error(err)
		udpConnStatus = false
		return
	} else {
		conn.CopyWaitGroup(target, localTcpConn, false, config.Client.Cnf.Compress, nil, nil, false, nil)
	}
}

// newUdpConn 与服务端协商进行 UDP 打洞，随后在打洞成功的 PacketConn 上
// 通过 KCP 建立可靠会话，并基于此创建 mux 复用以承载后续多路连接。
func newUdpConn(localAddr string, config *config.CommonConfig, l *config.LocalServer) {
	lock.Lock()
	defer lock.Unlock()
	remoteConn, err := NewConn(config.Tp, config.VKey, config.Server, common.WORK_P2P, config.ProxyUrl)
	if err != nil {
		logs.Error("Local connection server failed ", err.Error())
		return
	}
	if _, err := remoteConn.Write([]byte(crypt.Md5(l.Password))); err != nil {
		logs.Error("Local connection server failed ", err.Error())
		return
	}
	// 从服务端读取 P2P 打洞所需的远端地址/秘钥信息
	var rAddr []byte
	//读取服务端地址、密钥 继续做处理
	if rAddr, err = remoteConn.GetShortLenContent(); err != nil {
		logs.Error(err)
		return
	}
	var localConn net.PacketConn
	var remoteAddress string
	// 与服务端协商并进行打洞，返回可用的本地 PacketConn 及对端地址
	if remoteAddress, localConn, err = handleP2PUdp(localAddr, string(rAddr), crypt.Md5(l.Password), common.WORK_P2P_VISITOR); err != nil {
		logs.Error(err)
		return
	}
	// 基于打洞得到的地址与 PacketConn 建立 KCP 会话（可靠传输）
	udpTunnel, err := kcp.NewConn(remoteAddress, nil, 150, 3, localConn)
	if err != nil || udpTunnel == nil {
		logs.Warn(err)
		return
	}
	logs.Trace("successful create a connection with server", remoteAddress)
	conn.SetUdpSession(udpTunnel)
	udpConn = udpTunnel
	// 在 KCP 会话之上创建多路复用会话，后续业务连接均复用该通道
	muxSession = nps_mux.NewMux(udpConn, "kcp", config.DisconnectTime)
	p2pNetBridge = &p2pBridge{}
}
