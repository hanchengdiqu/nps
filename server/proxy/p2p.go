// Package proxy 提供 NPS（内网穿透代理）服务器的核心代理功能
// 包含基础代理服务器实现、流量统计、连接管理、认证检查等核心功能
// 支持多种代理模式：HTTP、HTTPS、TCP、UDP、SOCKS5、P2P 等
package proxy

import (
	"net"
	"strings"
	"time"

	"ehang.io/nps/lib/common"
	"github.com/astaxie/beego/logs"
)

// P2PServer P2P（点对点）服务器结构体
// 负责协助客户端之间建立直连通道，实现NAT穿透
// 当两个客户端需要建立P2P连接时，服务器作为中介协助交换地址信息
type P2PServer struct {
	BaseServer              // 继承基础服务器功能
	p2pPort  int            // P2P服务器监听端口
	p2p      map[string]*p2p // P2P连接映射表，key为密码，value为P2P连接信息
	listener *net.UDPConn   // UDP监听器，用于接收P2P握手消息
}

// p2p P2P连接信息结构体
// 存储访客端（visitor）和提供端（provider）的UDP地址
// 当两端都注册后，服务器会交换它们的地址信息
type p2p struct {
	visitorAddr  *net.UDPAddr // 访客端UDP地址
	providerAddr *net.UDPAddr // 提供端UDP地址
}

// NewP2PServer 创建新的P2P服务器实例
// p2pPort: P2P服务器监听端口
// 返回: 初始化完成的P2PServer实例
func NewP2PServer(p2pPort int) *P2PServer {
	return &P2PServer{
		p2pPort: p2pPort,
		p2p:     make(map[string]*p2p),
	}
}

// Start 启动P2P服务器
// 在指定端口上启动UDP监听，处理客户端的P2P握手请求
// 返回: 启动过程中的错误信息
func (s *P2PServer) Start() error {
	logs.Info("start p2p server port", s.p2pPort)
	var err error
	// 在0.0.0.0:指定端口上启动UDP监听
	s.listener, err = net.ListenUDP("udp", &net.UDPAddr{net.ParseIP("0.0.0.0"), s.p2pPort, ""})
	if err != nil {
		return err
	}
	// 持续监听UDP消息
	for {
		// 从缓冲区池获取字节数组，提高性能
		buf := common.BufPoolUdp.Get().([]byte)
		// 读取UDP数据包
		n, addr, err := s.listener.ReadFromUDP(buf)
		if err != nil {
			// 如果连接已关闭，退出循环
			if strings.Contains(err.Error(), "use of closed network connection") {
				break
			}
			continue
		}
		// 为每个P2P握手请求启动独立的goroutine处理
		go s.handleP2P(addr, string(buf[:n]))
	}
	return nil
}

// handleP2P 处理P2P握手消息
// 解析客户端发送的握手信息，协助两端建立直连
// addr: 发送方的UDP地址
// str: 握手消息内容，格式为"密码*#*角色"
func (s *P2PServer) handleP2P(addr *net.UDPAddr, str string) {
	var (
		v  *p2p
		ok bool
	)
	// 按分隔符拆分消息：密码*#*角色
	arr := strings.Split(str, common.CONN_DATA_SEQ)
	if len(arr) < 2 {
		return
	}
	// 根据密码获取或创建P2P连接信息
	if v, ok = s.p2p[arr[0]]; !ok {
		v = new(p2p)
		s.p2p[arr[0]] = v
	}
	logs.Trace("new p2p connection ,role %s , password %s ,local address %s", arr[1], arr[0], addr.String())
	
	// 根据角色处理不同的连接端
	if arr[1] == common.WORK_P2P_VISITOR {
		// 处理访客端连接
		v.visitorAddr = addr
		// 等待提供端连接，最多等待20秒
		for i := 20; i > 0; i-- {
			if v.providerAddr != nil {
				// 两端都已连接，交换地址信息
				// 向访客端发送提供端地址
				s.listener.WriteTo([]byte(v.providerAddr.String()), v.visitorAddr)
				// 向提供端发送访客端地址
				s.listener.WriteTo([]byte(v.visitorAddr.String()), v.providerAddr)
				break
			}
			time.Sleep(time.Second)
		}
		// 地址交换完成后，删除P2P连接信息
		delete(s.p2p, arr[0])
	} else {
		// 处理提供端连接
		v.providerAddr = addr
	}
}
