// Package proxy 实现了各种代理协议的服务器端处理逻辑
// 本文件专门处理SOCKS5代理协议的实现
package proxy

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego/logs"
)

// SOCKS5协议中的地址类型常量定义
const (
	ipV4       = 1 // IPv4地址类型
	domainName = 3 // 域名地址类型
	ipV6       = 4 // IPv6地址类型
)

// SOCKS5协议中的命令类型常量定义
const (
	connectMethod   = 1 // CONNECT命令 - 建立TCP连接
	bindMethod      = 2 // BIND命令 - 绑定端口等待连接
	associateMethod = 3 // UDP ASSOCIATE命令 - UDP关联
)

// UDP数据包的最大尺寸常量
// 基于以太网最大包大小减去IP和UDP头部计算得出
// IPv4头部20字节，UDP头部4字节，总开销24字节
// 以太网最大包大小1500字节，1500 - 24 = 1476字节
const (
	maxUDPPacketSize = 1476
)

// SOCKS5协议响应状态码常量定义
const (
	succeeded            uint8 = iota // 0 - 成功
	serverFailure                     // 1 - 服务器故障
	notAllowed                        // 2 - 不允许的连接
	networkUnreachable                // 3 - 网络不可达
	hostUnreachable                   // 4 - 主机不可达
	connectionRefused                 // 5 - 连接被拒绝
	ttlExpired                        // 6 - TTL过期
	commandNotSupported               // 7 - 命令不支持
	addrTypeNotSupported              // 8 - 地址类型不支持
)

// SOCKS5用户名密码认证相关常量定义
const (
	UserPassAuth    = uint8(2) // 用户名密码认证方法编号
	userAuthVersion = uint8(1) // 用户认证版本号
	authSuccess     = uint8(0) // 认证成功状态码
	authFailure     = uint8(1) // 认证失败状态码
)

// Sock5ModeServer SOCKS5代理服务器结构体
// 继承自BaseServer，提供SOCKS5协议的代理服务
type Sock5ModeServer struct {
	BaseServer           // 基础服务器功能
	listener   net.Listener // TCP监听器
}

// handleRequest 处理SOCKS5客户端请求
// 根据SOCKS5协议规范解析客户端请求并分发到对应的处理方法
// 参数:
//   c - 客户端连接
func (s *Sock5ModeServer) handleRequest(c net.Conn) {
	/*
		SOCKS请求格式如下:
		+----+-----+-------+------+----------+----------+
		|VER | CMD |  RSV  | ATYP | DST.ADDR | DST.PORT |
		+----+-----+-------+------+----------+----------+
		| 1  |  1  | X'00' |  1   | Variable |    2     |
		+----+-----+-------+------+----------+----------+
		VER: 版本号，SOCKS5为0x05
		CMD: 命令码，1=CONNECT，2=BIND，3=UDP ASSOCIATE
		RSV: 保留字段，必须为0x00
		ATYP: 地址类型，1=IPv4，3=域名，4=IPv6
		DST.ADDR: 目标地址
		DST.PORT: 目标端口
	*/
	header := make([]byte, 3)

	_, err := io.ReadFull(c, header)
	if err != nil {
		logs.Warn("illegal request", err)
		c.Close()
		return
	}

	// 根据命令类型分发处理
	switch header[1] {
	case connectMethod:
		s.handleConnect(c) // 处理CONNECT命令
	case bindMethod:
		s.handleBind(c) // 处理BIND命令
	case associateMethod:
		s.handleUDP(c) // 处理UDP ASSOCIATE命令
	default:
		s.sendReply(c, commandNotSupported) // 不支持的命令
		c.Close()
	}
}

// sendReply 向客户端发送SOCKS5响应
// 构造并发送符合SOCKS5协议规范的响应消息
// 参数:
//   c - 客户端连接
//   rep - 响应状态码
func (s *Sock5ModeServer) sendReply(c net.Conn, rep uint8) {
	/*
		SOCKS5响应格式:
		+----+-----+-------+------+----------+----------+
		|VER | REP |  RSV  | ATYP | BND.ADDR | BND.PORT |
		+----+-----+-------+------+----------+----------+
		| 1  |  1  | X'00' |  1   | Variable |    2     |
		+----+-----+-------+------+----------+----------+
	*/
	reply := []byte{
		5,   // VER: SOCKS版本号5
		rep, // REP: 响应状态码
		0,   // RSV: 保留字段
		1,   // ATYP: 地址类型(IPv4)
	}

	// 获取本地地址信息
	localAddr := c.LocalAddr().String()
	localHost, localPort, _ := net.SplitHostPort(localAddr)
	ipBytes := net.ParseIP(localHost).To4()
	nPort, _ := strconv.Atoi(localPort)
	
	// 添加绑定地址和端口
	reply = append(reply, ipBytes...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(nPort))
	reply = append(reply, portBytes...)

	c.Write(reply)
}

// doConnect 执行连接操作的核心逻辑
// 解析目标地址并建立到目标服务器的连接
// 参数:
//   c - 客户端连接
//   command - 命令类型
func (s *Sock5ModeServer) doConnect(c net.Conn, command uint8) {
	// 读取地址类型
	addrType := make([]byte, 1)
	c.Read(addrType)
	
	var host string
	// 根据地址类型解析目标地址
	switch addrType[0] {
	case ipV4:
		// IPv4地址 - 4字节
		ipv4 := make(net.IP, net.IPv4len)
		c.Read(ipv4)
		host = ipv4.String()
	case ipV6:
		// IPv6地址 - 16字节
		ipv6 := make(net.IP, net.IPv6len)
		c.Read(ipv6)
		host = ipv6.String()
	case domainName:
		// 域名 - 首字节为长度，后跟域名字符串
		var domainLen uint8
		binary.Read(c, binary.BigEndian, &domainLen)
		domain := make([]byte, domainLen)
		c.Read(domain)
		host = string(domain)
	default:
		// 不支持的地址类型
		s.sendReply(c, addrTypeNotSupported)
		return
	}

	// 读取目标端口(2字节，大端序)
	var port uint16
	binary.Read(c, binary.BigEndian, &port)
	
	// 构造目标地址
	addr := net.JoinHostPort(host, strconv.Itoa(int(port)))
	
	// 确定连接类型
	var ltype string
	if command == associateMethod {
		ltype = common.CONN_UDP // UDP连接
	} else {
		ltype = common.CONN_TCP // TCP连接
	}
	
	// 处理客户端连接，建立隧道
	s.DealClient(conn.NewConn(c), s.task.Client, addr, nil, ltype, func() {
		s.sendReply(c, succeeded) // 连接成功后发送成功响应
	}, s.task.Flow, s.task.Target.LocalProxy)
	return
}

// handleConnect 处理CONNECT命令
// CONNECT是SOCKS5最常用的命令，用于建立TCP连接
// 参数:
//   c - 客户端连接
func (s *Sock5ModeServer) handleConnect(c net.Conn) {
	s.doConnect(c, connectMethod)
}

// handleBind 处理BIND命令
// BIND命令用于被动模式连接，目前为空实现
// 参数:
//   c - 客户端连接
func (s *Sock5ModeServer) handleBind(c net.Conn) {
	// TODO: 实现BIND命令处理逻辑
}

// sendUdpReply 发送UDP关联响应
// 为UDP ASSOCIATE命令发送特定的响应消息
// 参数:
//   writeConn - 用于写入响应的连接
//   c - UDP监听连接
//   rep - 响应状态码
//   serverIp - 服务器IP地址
func (s *Sock5ModeServer) sendUdpReply(writeConn net.Conn, c net.Conn, rep uint8, serverIp string) {
	reply := []byte{
		5,   // VER: SOCKS版本号5
		rep, // REP: 响应状态码
		0,   // RSV: 保留字段
		1,   // ATYP: 地址类型(IPv4)
	}
	
	// 获取本地地址信息，使用服务器IP
	localHost, localPort, _ := net.SplitHostPort(c.LocalAddr().String())
	localHost = serverIp // 使用指定的服务器IP
	ipBytes := net.ParseIP(localHost).To4()
	nPort, _ := strconv.Atoi(localPort)
	
	// 构造完整响应
	reply = append(reply, ipBytes...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(nPort))
	reply = append(reply, portBytes...)
	writeConn.Write(reply)
}

// handleUDP 处理UDP ASSOCIATE命令
// 实现SOCKS5的UDP代理功能，建立UDP隧道
// 参数:
//   c - 客户端TCP控制连接
func (s *Sock5ModeServer) handleUDP(c net.Conn) {
	defer c.Close()
	
	// 解析客户端请求的地址信息(通常被忽略)
	addrType := make([]byte, 1)
	c.Read(addrType)
	var host string
	switch addrType[0] {
	case ipV4:
		ipv4 := make(net.IP, net.IPv4len)
		c.Read(ipv4)
		host = ipv4.String()
	case ipV6:
		ipv6 := make(net.IP, net.IPv6len)
		c.Read(ipv6)
		host = ipv6.String()
	case domainName:
		var domainLen uint8
		binary.Read(c, binary.BigEndian, &domainLen)
		domain := make([]byte, domainLen)
		c.Read(domain)
		host = string(domain)
	default:
		s.sendReply(c, addrTypeNotSupported)
		return
	}
	
	// 读取端口信息
	var port uint16
	binary.Read(c, binary.BigEndian, &port)
	logs.Warn(host, string(port))
	
	// 创建本地UDP监听地址
	replyAddr, err := net.ResolveUDPAddr("udp", s.task.ServerIp+":0")
	if err != nil {
		logs.Error("build local reply addr error", err)
		return
	}
	
	// 监听UDP端口
	reply, err := net.ListenUDP("udp", replyAddr)
	if err != nil {
		s.sendReply(c, addrTypeNotSupported)
		logs.Error("listen local reply udp port error")
		return
	}
	
	// 向客户端发送UDP关联成功响应
	s.sendUdpReply(c, reply, succeeded, common.GetServerIpByClientIp(c.RemoteAddr().(*net.TCPAddr).IP))
	defer reply.Close()
	
	// 创建到客户端的隧道连接
	link := conn.NewLink("udp5", "", s.task.Client.Cnf.Crypt, s.task.Client.Cnf.Compress, c.RemoteAddr().String(), false)
	target, err := s.bridge.SendLinkInfo(s.task.Client.Id, link, s.task)
	if err != nil {
		logs.Warn("get connection from client id %d  error %s", s.task.Client.Id, err.Error())
		return
	}

	var clientAddr net.Addr
	
	// 启动goroutine处理从本地UDP到客户端的数据转发
	go func() {
		b := common.BufPoolUdp.Get().([]byte)
		defer common.BufPoolUdp.Put(b)
		defer c.Close()

		for {
			// 从UDP监听端口读取数据
			n, laddr, err := reply.ReadFrom(b)
			if err != nil {
				logs.Error("read data from %s err %s", reply.LocalAddr().String(), err.Error())
				return
			}
			// 记录客户端地址
			if clientAddr == nil {
				clientAddr = laddr
			}
			// 转发数据到客户端
			if _, err := target.Write(b[:n]); err != nil {
				logs.Error("write data to client error", err.Error())
				return
			}
		}
	}()

	// 启动goroutine处理从客户端到本地UDP的数据转发
	go func() {
		var l int32
		b := common.BufPoolUdp.Get().([]byte)
		defer common.BufPoolUdp.Put(b)
		defer c.Close()
		
		for {
			// 读取数据长度
			if err := binary.Read(target, binary.LittleEndian, &l); err != nil || l >= common.PoolSizeUdp || l <= 0 {
				logs.Warn("read len bytes error", err.Error())
				return
			}
			// 读取实际数据
			binary.Read(target, binary.LittleEndian, b[:l])
			if err != nil {
				logs.Warn("read data form client error", err.Error())
				return
			}
			// 转发数据到客户端UDP地址
			if _, err := reply.WriteTo(b[:l], clientAddr); err != nil {
				logs.Warn("write data to user ", err.Error())
				return
			}
		}
	}()

	// 保持TCP控制连接活跃
	b := common.BufPoolUdp.Get().([]byte)
	defer common.BufPoolUdp.Put(b)
	defer target.Close()
	for {
		_, err := c.Read(b)
		if err != nil {
			c.Close()
			return
		}
	}
}

// handleConn 处理新的客户端连接
// 执行SOCKS5协议的初始握手和认证流程
// 参数:
//   c - 客户端连接
func (s *Sock5ModeServer) handleConn(c net.Conn) {
	// 读取客户端握手请求的前2字节
	buf := make([]byte, 2)
	if _, err := io.ReadFull(c, buf); err != nil {
		logs.Warn("negotiation err", err)
		c.Close()
		return
	}

	// 检查SOCKS版本号
	if version := buf[0]; version != 5 {
		logs.Warn("only support socks5, request from: ", c.RemoteAddr())
		c.Close()
		return
	}
	
	// 获取认证方法数量
	nMethods := buf[1]

	// 读取客户端支持的认证方法列表
	methods := make([]byte, nMethods)
	if len, err := c.Read(methods); len != int(nMethods) || err != nil {
		logs.Warn("wrong method")
		c.Close()
		return
	}
	
	// 检查是否需要用户名密码认证
	if (s.task.Client.Cnf.U != "" && s.task.Client.Cnf.P != "") || (s.task.MultiAccount != nil && len(s.task.MultiAccount.AccountMap) > 0) {
		// 需要认证，返回用户名密码认证方法
		buf[1] = UserPassAuth
		c.Write(buf)
		if err := s.Auth(c); err != nil {
			c.Close()
			logs.Warn("Validation failed:", err)
			return
		}
	} else {
		// 不需要认证，返回无认证方法
		buf[1] = 0
		c.Write(buf)
	}
	
	// 处理客户端请求
	s.handleRequest(c)
}

// Auth 执行SOCKS5用户名密码认证
// 验证客户端提供的用户名和密码
// 参数:
//   c - 客户端连接
// 返回:
//   error - 认证错误信息，nil表示认证成功
func (s *Sock5ModeServer) Auth(c net.Conn) error {
	// 读取认证请求头部(版本号和用户名长度)
	header := []byte{0, 0}
	if _, err := io.ReadAtLeast(c, header, 2); err != nil {
		return err
	}
	
	// 检查认证版本号
	if header[0] != userAuthVersion {
		return errors.New("验证方式不被支持")
	}
	
	// 读取用户名
	userLen := int(header[1])
	user := make([]byte, userLen)
	if _, err := io.ReadAtLeast(c, user, userLen); err != nil {
		return err
	}
	
	// 读取密码长度
	if _, err := c.Read(header[:1]); err != nil {
		return errors.New("密码长度获取错误")
	}
	
	// 读取密码
	passLen := int(header[0])
	pass := make([]byte, passLen)
	if _, err := io.ReadAtLeast(c, pass, passLen); err != nil {
		return err
	}

	var U, P string
	if s.task.MultiAccount != nil {
		// 启用多用户认证模式
		U = string(user)
		var ok bool
		P, ok = s.task.MultiAccount.AccountMap[U]
		if !ok {
			return errors.New("验证不通过")
		}
	} else {
		// 单用户认证模式
		U = s.task.Client.Cnf.U
		P = s.task.Client.Cnf.P
	}

	// 验证用户名和密码
	if string(user) == U && string(pass) == P {
		// 认证成功
		if _, err := c.Write([]byte{userAuthVersion, authSuccess}); err != nil {
			return err
		}
		return nil
	} else {
		// 认证失败
		if _, err := c.Write([]byte{userAuthVersion, authFailure}); err != nil {
			return err
		}
		return errors.New("验证不通过")
	}
}

// Start 启动SOCKS5代理服务器
// 在指定的IP和端口上监听客户端连接
// 返回:
//   error - 启动错误信息，nil表示启动成功
func (s *Sock5ModeServer) Start() error {
	return conn.NewTcpListenerAndProcess(s.task.ServerIp+":"+strconv.Itoa(s.task.Port), func(c net.Conn) {
		// 检查客户端流量和连接数限制
		if err := s.CheckFlowAndConnNum(s.task.Client); err != nil {
			logs.Warn("client id %d, task id %d, error %s, when socks5 connection", s.task.Client.Id, s.task.Id, err.Error())
			c.Close()
			return
		}
		logs.Trace("New socks5 connection,client %d,remote address %s", s.task.Client.Id, c.RemoteAddr())
		s.handleConn(c)        // 处理连接
		s.task.Client.AddConn() // 增加连接计数
	}, &s.listener)
}

// NewSock5ModeServer 创建新的SOCKS5代理服务器实例
// 参数:
//   bridge - 网络桥接器，用于与客户端通信
//   task - 隧道任务配置
// 返回:
//   *Sock5ModeServer - SOCKS5服务器实例
func NewSock5ModeServer(bridge NetBridge, task *file.Tunnel) *Sock5ModeServer {
	s := new(Sock5ModeServer)
	s.bridge = bridge
	s.task = task
	return s
}

// Close 关闭SOCKS5代理服务器
// 停止监听并释放相关资源
// 返回:
//   error - 关闭错误信息，nil表示关闭成功
func (s *Sock5ModeServer) Close() error {
	return s.listener.Close()
}
