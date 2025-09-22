package proxy

import (
	"net"
	"net/http"
	"net/url"
	"sync"

	"ehang.io/nps/lib/cache"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
	"github.com/pkg/errors"
)

// HttpsServer HTTPS代理服务器结构体
// 继承自httpServer，提供HTTPS代理功能
type HttpsServer struct {
	httpServer
	listener         net.Listener        // 网络监听器
	httpsListenerMap sync.Map            // 存储不同域名的HTTPS监听器映射表
}

// NewHttpsServer 创建新的HTTPS服务器实例
// 参数:
//   - l: 网络监听器
//   - bridge: 网络桥接接口
//   - useCache: 是否使用缓存
//   - cacheLen: 缓存长度
func NewHttpsServer(l net.Listener, bridge NetBridge, useCache bool, cacheLen int) *HttpsServer {
	https := &HttpsServer{listener: l}
	https.bridge = bridge
	https.useCache = useCache
	if useCache {
		https.cache = cache.New(cacheLen)
	}
	return https
}

// Start 启动HTTPS服务器
// 根据配置决定是纯代理模式还是SSL证书模式
func (https *HttpsServer) Start() error {
	// 检查是否配置为纯代理模式
	if b, err := beego.AppConfig.Bool("https_just_proxy"); err == nil && b {
		// 纯代理模式：直接处理HTTPS连接，不进行SSL终止
		conn.Accept(https.listener, func(c net.Conn) {
			https.handleHttps(c)
		})
	} else {
		// SSL证书模式：使用SSL证书进行HTTPS服务
		// 启动默认监听器
		certFile := beego.AppConfig.String("https_default_cert_file")
		keyFile := beego.AppConfig.String("https_default_key_file")
		if common.FileExists(certFile) && common.FileExists(keyFile) {
			l := NewHttpsListener(https.listener)
			https.NewHttps(l, certFile, keyFile)
			https.httpsListenerMap.Store("default", l)
		}
		// 接受连接并处理SNI（服务器名称指示）
		conn.Accept(https.listener, func(c net.Conn) {
			serverName, rb := GetServerNameFromClientHello(c)
			// 如果客户端Hello消息不包含SNI，使用默认SSL证书
			if serverName == "" {
				serverName = "default"
			}
			var l *HttpsListener
			if v, ok := https.httpsListenerMap.Load(serverName); ok {
				// 如果已存在该域名的监听器，直接使用
				l = v.(*HttpsListener)
			} else {
				// 创建新的HTTPS请求来查询主机信息
				r := buildHttpsRequest(serverName)
				if host, err := file.GetDb().GetInfoByHost(serverName, r); err != nil {
					c.Close()
					logs.Notice("the url %s can't be parsed!,remote addr %s", serverName, c.RemoteAddr().String())
					return
				} else {
					if !common.FileExists(host.CertFilePath) || !common.FileExists(host.KeyFilePath) {
						// 如果主机证书文件或密钥文件未设置，使用默认文件
						if v, ok := https.httpsListenerMap.Load("default"); ok {
							l = v.(*HttpsListener)
						} else {
							c.Close()
							logs.Error("the key %s cert %s file is not exist", host.KeyFilePath, host.CertFilePath)
							return
						}
					} else {
						// 创建新的HTTPS监听器并启动SSL服务
						l = NewHttpsListener(https.listener)
						https.NewHttps(l, host.CertFilePath, host.KeyFilePath)
						https.httpsListenerMap.Store(serverName, l)
					}
				}
			}
			// 将连接包装并发送到对应的监听器
			acceptConn := conn.NewConn(c)
			acceptConn.Rb = rb
			l.acceptConn <- acceptConn
		})
	}
	return nil
}

// Close 关闭HTTPS服务器
func (https *HttpsServer) Close() error {
	return https.listener.Close()
}

// NewHttps 根据证书和密钥文件创建新的HTTPS服务器
// 参数:
//   - l: 网络监听器
//   - certFile: 证书文件路径
//   - keyFile: 密钥文件路径
func (https *HttpsServer) NewHttps(l net.Listener, certFile string, keyFile string) {
	go func() {
		logs.Error(https.NewServer(0, "https").ServeTLS(l, certFile, keyFile))
	}()
}

// handleHttps 处理纯代理模式的HTTPS连接
// 从客户端Hello消息中提取主机名，然后代理到目标服务器
func (https *HttpsServer) handleHttps(c net.Conn) {
	hostName, rb := GetServerNameFromClientHello(c)
	var targetAddr string
	r := buildHttpsRequest(hostName)
	var host *file.Host
	var err error
	
	// 根据主机名查询主机配置信息
	if host, err = file.GetDb().GetInfoByHost(hostName, r); err != nil {
		c.Close()
		logs.Notice("the url %s can't be parsed!", hostName)
		return
	}
	
	// 检查客户端流量和连接数限制
	if err := https.CheckFlowAndConnNum(host.Client); err != nil {
		logs.Warn("client id %d, host id %d, error %s, when https connection", host.Client.Id, host.Id, err.Error())
		c.Close()
		return
	}
	defer host.Client.AddConn()
	
	// 进行身份验证
	if err = https.auth(r, conn.NewConn(c), host.Client.Cnf.U, host.Client.Cnf.P); err != nil {
		logs.Warn("auth error", err, r.RemoteAddr)
		return
	}
	
	// 获取随机目标地址
	if targetAddr, err = host.Target.GetRandomTarget(); err != nil {
		logs.Warn(err.Error())
	}
	
	logs.Trace("new https connection,clientId %d,host %s,remote address %s", host.Client.Id, r.Host, c.RemoteAddr().String())
	// 处理客户端连接，建立代理
	https.DealClient(conn.NewConn(c), host.Client, targetAddr, rb, common.CONN_TCP, nil, host.Flow, host.Target.LocalProxy)
}

// HttpsListener HTTPS监听器结构体
// 用于管理HTTPS连接的接受和分发
type HttpsListener struct {
	acceptConn     chan *conn.Conn  // 连接接受通道
	parentListener net.Listener     // 父级监听器
}

// NewHttpsListener 创建新的HTTPS监听器
func NewHttpsListener(l net.Listener) *HttpsListener {
	return &HttpsListener{parentListener: l, acceptConn: make(chan *conn.Conn)}
}

// Accept 接受连接
// 从连接通道中获取新的连接
func (httpsListener *HttpsListener) Accept() (net.Conn, error) {
	httpsConn := <-httpsListener.acceptConn
	if httpsConn == nil {
		return nil, errors.New("get connection error")
	}
	return httpsConn, nil
}

// Close 关闭监听器
func (httpsListener *HttpsListener) Close() error {
	return nil
}

// Addr 获取监听器地址
func (httpsListener *HttpsListener) Addr() net.Addr {
	return httpsListener.parentListener.Addr()
}

// GetServerNameFromClientHello 从客户端Hello消息中获取服务器名称
// 通过读取TLS握手数据来提取SNI（服务器名称指示）
// 返回值:
//   - string: 服务器名称
//   - []byte: 原始客户端Hello数据
func GetServerNameFromClientHello(c net.Conn) (string, []byte) {
	buf := make([]byte, 4096)
	data := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		return "", nil
	}
	if n < 42 {
		return "", nil
	}
	copy(data, buf[:n])
	clientHello := new(crypt.ClientHelloMsg)
	clientHello.Unmarshal(data[5:n])
	return clientHello.GetServerName(), buf[:n]
}

// buildHttpsRequest 构建HTTPS请求
// 根据主机名创建一个基本的HTTP请求对象
func buildHttpsRequest(hostName string) *http.Request {
	r := new(http.Request)
	r.RequestURI = "/"
	r.URL = new(url.URL)
	r.URL.Scheme = "https"
	r.Host = hostName
	return r
}
