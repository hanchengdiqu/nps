// Package proxy 提供了NPS服务器端的代理功能实现
// 包含TCP隧道模式服务器和Web管理服务器的实现
package proxy

import (
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strconv"

	"ehang.io/nps/bridge"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server/connection"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

// TunnelModeServer 隧道模式服务器结构体
// 用于处理TCP、HTTP等隧道连接的服务器实现
type TunnelModeServer struct {
	BaseServer           // 继承基础服务器功能
	process  process     // 连接处理函数，定义如何处理每个新连接
	listener net.Listener // 网络监听器，用于接收客户端连接
}

// NewTunnelModeServer 创建新的隧道模式服务器实例
// 支持tcp、http、host等多种隧道类型
// 参数:
//   process: 连接处理函数，定义如何处理每个新连接
//   bridge: 网络桥接器，用于客户端和服务器之间的通信
//   task: 隧道任务配置，包含端口、目标地址等信息
// 返回值: 初始化完成的TunnelModeServer实例
func NewTunnelModeServer(process process, bridge NetBridge, task *file.Tunnel) *TunnelModeServer {
	s := new(TunnelModeServer)
	s.bridge = bridge  // 设置网络桥接器
	s.process = process // 设置连接处理函数
	s.task = task      // 设置隧道任务配置
	return s
}

// Start 启动隧道模式服务器
// 在指定的IP和端口上开始监听客户端连接
// 对每个新连接进行流量和连接数检查，然后交给process函数处理
// 返回值: 如果启动失败返回错误信息
func (s *TunnelModeServer) Start() error {
	// 构建监听地址：服务器IP + 端口
	listenAddr := s.task.ServerIp + ":" + strconv.Itoa(s.task.Port)
	
	// 创建TCP监听器并处理连接
	return conn.NewTcpListenerAndProcess(listenAddr, func(c net.Conn) {
		// 检查客户端的流量限制和连接数限制
		if err := s.CheckFlowAndConnNum(s.task.Client); err != nil {
			logs.Warn("client id %d, task id %d,error %s, when tcp connection", s.task.Client.Id, s.task.Id, err.Error())
			c.Close() // 超出限制，关闭连接
			return
		}
		
		// 记录新连接信息
		logs.Trace("new tcp connection,local port %d,client %d,remote address %s", s.task.Port, s.task.Client.Id, c.RemoteAddr())
		
		// 使用指定的process函数处理连接
		s.process(conn.NewConn(c), s)
		
		// 增加客户端连接计数
		s.task.Client.AddConn()
	}, &s.listener)
}

// Close 关闭隧道模式服务器
// 停止监听并关闭所有相关资源
// 返回值: 如果关闭过程中出现错误则返回错误信息
func (s *TunnelModeServer) Close() error {
	return s.listener.Close() // 关闭网络监听器
}

// WebServer Web管理服务器结构体
// 提供基于Web界面的NPS管理功能
type WebServer struct {
	BaseServer // 继承基础服务器功能
}

// Start 启动Web管理服务器
// 配置并启动基于Beego框架的Web管理界面
// 支持HTTP和HTTPS两种模式，提供NPS的Web管理功能
// 返回值: 如果启动失败返回错误信息
func (s *WebServer) Start() error {
	// 获取Web管理端口配置
	p, _ := beego.AppConfig.Int("web_port")
	if p == 0 {
		// 如果端口为0，则无限等待（阻塞服务器启动）
		stop := make(chan struct{})
		<-stop
	}
	
	// 启用Session功能
	beego.BConfig.WebConfig.Session.SessionOn = true
	
	// 设置静态文件路径
	staticPath := filepath.Join(common.GetRunPath(), "web", "static")
	beego.SetStaticPath(beego.AppConfig.String("web_base_url")+"/static", staticPath)
	
	// 设置模板文件路径
	viewsPath := filepath.Join(common.GetRunPath(), "web", "views")
	beego.SetViewsPath(viewsPath)
	
	// 初始化错误信息
	err := errors.New("Web management startup failure ")
	var l net.Listener
	
	// 获取Web管理监听器
	if l, err = connection.GetWebManagerListener(); err == nil {
		// 初始化Beego HTTP运行环境
		beego.InitBeforeHTTPRun()
		
		// 根据配置选择HTTP或HTTPS模式
		if beego.AppConfig.String("web_open_ssl") == "true" {
			// HTTPS模式：使用SSL证书
			keyPath := beego.AppConfig.String("web_key_file")   // 私钥文件路径
			certPath := beego.AppConfig.String("web_cert_file") // 证书文件路径
			err = http.ServeTLS(l, beego.BeeApp.Handlers, certPath, keyPath)
		} else {
			// HTTP模式：普通HTTP服务
			err = http.Serve(l, beego.BeeApp.Handlers)
		}
	} else {
		// 获取监听器失败，记录错误
		logs.Error(err)
	}
	return err
}

// Close 关闭Web管理服务器
// Web服务器通常由HTTP服务器自行管理生命周期，因此这里返回nil
// 返回值: 总是返回nil，表示关闭成功
func (s *WebServer) Close() error {
	return nil // Web服务器无需特殊关闭操作
}

// NewWebServer 创建新的Web管理服务器实例
// 参数:
//   bridge: 网络桥接器，用于与客户端通信
// 返回值: 初始化完成的WebServer实例
func NewWebServer(bridge *bridge.Bridge) *WebServer {
	s := new(WebServer)
	s.bridge = bridge // 设置网络桥接器
	return s
}

// process 连接处理函数类型定义
// 定义了处理客户端连接的函数签名
// 参数:
//   c: 客户端连接对象
//   s: 隧道模式服务器实例
// 返回值: 处理过程中的错误信息，成功则返回nil
type process func(c *conn.Conn, s *TunnelModeServer) error

// ProcessTunnel TCP隧道代理处理函数
// 处理TCP隧道连接，将客户端连接转发到目标服务器
// 这是一个process函数的具体实现，用于TCP代理场景
// 参数:
//   c: 客户端连接对象
//   s: 隧道模式服务器实例
// 返回值: 处理过程中的错误信息，成功则返回nil
func ProcessTunnel(c *conn.Conn, s *TunnelModeServer) error {
	// 从目标配置中随机选择一个目标地址（支持负载均衡）
	targetAddr, err := s.task.Target.GetRandomTarget()
	if err != nil {
		c.Close() // 获取目标地址失败，关闭连接
		logs.Warn("tcp port %d ,client id %d,task id %d connect error %s", s.task.Port, s.task.Client.Id, s.task.Id, err.Error())
		return err
	}
	
	// 处理客户端连接，建立到目标服务器的隧道
	// 参数说明：连接对象、客户端信息、目标地址、读取缓冲区、连接类型、额外数据、流量控制、本地代理设置
	return s.DealClient(c, s.task.Client, targetAddr, nil, common.CONN_TCP, nil, s.task.Flow, s.task.Target.LocalProxy)
}

// ProcessHttp HTTP代理处理函数
// 处理HTTP代理连接，支持HTTP和HTTPS（CONNECT方法）代理
// 这是一个process函数的具体实现，用于HTTP代理场景
// 参数:
//   c: 客户端连接对象
//   s: 隧道模式服务器实例
// 返回值: 处理过程中的错误信息，成功则返回nil
func ProcessHttp(c *conn.Conn, s *TunnelModeServer) error {
	// 解析HTTP请求，获取目标主机地址和请求信息
	_, addr, rb, err, r := c.GetHost()
	if err != nil {
		c.Close() // 解析HTTP请求失败，关闭连接
		logs.Info(err)
		return err
	}
	
	// 处理HTTPS代理（HTTP CONNECT方法）
	if r.Method == "CONNECT" {
		// 发送连接建立成功响应
		c.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		rb = nil // CONNECT方法不需要转发请求体
	}
	
	// 进行身份验证（如果配置了用户名和密码）
	if err := s.auth(r, c, s.task.Client.Cnf.U, s.task.Client.Cnf.P); err != nil {
		return err // 身份验证失败
	}
	
	// 处理客户端连接，建立到目标服务器的代理隧道
	// 参数说明：连接对象、客户端信息、目标地址、请求缓冲区、连接类型、额外数据、流量控制、本地代理设置
	return s.DealClient(c, s.task.Client, addr, rb, common.CONN_TCP, nil, s.task.Flow, s.task.Target.LocalProxy)
}
