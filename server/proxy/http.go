// Package proxy 提供 NPS（内网穿透代理）服务器的 HTTP 代理功能
// 支持 HTTP 和 HTTPS 协议的代理转发，包含缓存、流量统计、认证等高级功能
package proxy

import (
	"bufio"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"ehang.io/nps/bridge"
	"ehang.io/nps/lib/cache"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server/connection"
	"github.com/astaxie/beego/logs"
)

// httpServer HTTP代理服务器结构体
// 继承自BaseServer，提供HTTP和HTTPS协议的代理服务
type httpServer struct {
	BaseServer                    // 基础代理服务器，提供通用功能
	httpPort      int             // HTTP服务监听端口
	httpsPort     int             // HTTPS服务监听端口
	httpServer    *http.Server    // HTTP服务器实例
	httpsServer   *http.Server    // HTTPS服务器实例
	httpsListener net.Listener    // HTTPS监听器
	useCache      bool            // 是否启用缓存功能
	addOrigin     bool            // 是否添加Origin头信息
	cache         *cache.Cache    // 缓存实例，用于存储HTTP响应
	cacheLen      int             // 缓存最大长度限制
}

// NewHttp 创建新的HTTP代理服务器实例
// bridge: 网络桥接器，用于与客户端通信
// c: 隧道配置信息
// httpPort: HTTP服务端口，0表示不启动HTTP服务
// httpsPort: HTTPS服务端口，0表示不启动HTTPS服务
// useCache: 是否启用响应缓存
// cacheLen: 缓存大小限制
// addOrigin: 是否在请求头中添加Origin信息
// 返回: 初始化完成的HTTP服务器实例
func NewHttp(bridge *bridge.Bridge, c *file.Tunnel, httpPort, httpsPort int, useCache bool, cacheLen int, addOrigin bool) *httpServer {
	httpServer := &httpServer{
		BaseServer: BaseServer{
			task:   c,
			bridge: bridge,
			Mutex:  sync.Mutex{},
		},
		httpPort:  httpPort,
		httpsPort: httpsPort,
		useCache:  useCache,
		cacheLen:  cacheLen,
		addOrigin: addOrigin,
	}
	// 如果启用缓存，初始化缓存实例
	if useCache {
		httpServer.cache = cache.New(cacheLen)
	}
	return httpServer
}

// Start 启动HTTP代理服务器
// 根据配置启动HTTP和/或HTTPS服务
// 返回: 启动过程中的错误
func (s *httpServer) Start() error {
	var err error
	// 读取错误页面内容，用于连接失败时显示
	if s.errorContent, err = common.ReadAllFromFile(filepath.Join(common.GetRunPath(), "web", "static", "page", "error.html")); err != nil {
		s.errorContent = []byte("npx 404")
	}
	
	// 启动HTTP服务
	if s.httpPort > 0 {
		s.httpServer = s.NewServer(s.httpPort, "http")
		go func() {
			// 获取HTTP监听器
			l, err := connection.GetHttpListener()
			if err != nil {
				logs.Error(err)
				os.Exit(0)
			}
			// 启动HTTP服务器
			err = s.httpServer.Serve(l)
			if err != nil {
				logs.Error(err)
				os.Exit(0)
			}
		}()
	}
	
	// 启动HTTPS服务
	if s.httpsPort > 0 {
		s.httpsServer = s.NewServer(s.httpsPort, "https")
		go func() {
			// 获取HTTPS监听器
			s.httpsListener, err = connection.GetHttpsListener()
			if err != nil {
				logs.Error(err)
				os.Exit(0)
			}
			// 启动HTTPS服务器
			logs.Error(NewHttpsServer(s.httpsListener, s.bridge, s.useCache, s.cacheLen).Start())
		}()
	}
	return nil
}

// Close 关闭HTTP代理服务器
// 优雅关闭所有HTTP和HTTPS服务
// 返回: 关闭过程中的错误
func (s *httpServer) Close() error {
	// 关闭HTTPS监听器
	if s.httpsListener != nil {
		s.httpsListener.Close()
	}
	// 关闭HTTPS服务器
	if s.httpsServer != nil {
		s.httpsServer.Close()
	}
	// 关闭HTTP服务器
	if s.httpServer != nil {
		s.httpServer.Close()
	}
	return nil
}

// handleTunneling 处理HTTP隧道连接
// 将HTTP连接升级为原始TCP连接，用于代理转发
// w: HTTP响应写入器
// r: HTTP请求对象
func (s *httpServer) handleTunneling(w http.ResponseWriter, r *http.Request) {
	// 检查是否支持连接劫持（Hijacking）
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	// 劫持HTTP连接，获取原始TCP连接
	c, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
	// 处理HTTP代理请求
	s.handleHttp(conn.NewConn(c), r)
}

// handleHttp 处理HTTP代理请求的核心方法
// 实现完整的HTTP代理逻辑：认证、路由、转发、缓存等
// c: 客户端连接对象
// r: HTTP请求对象
func (s *httpServer) handleHttp(c *conn.Conn, r *http.Request) {
	var (
		host       *file.Host      // 目标主机配置
		target     net.Conn        // 目标服务器连接
		err        error           // 错误信息
		connClient io.ReadWriteCloser // 客户端连接包装器
		scheme     = r.URL.Scheme  // 请求协议（http/https）
		lk         *conn.Link      // 连接链路信息
		targetAddr string          // 目标地址
		lenConn    *conn.LenConn   // 带长度统计的连接
		isReset    bool            // 是否需要重置连接
		wg         sync.WaitGroup  // 等待组，用于协程同步
	)
	
	// 确保连接在函数结束时被正确关闭
	defer func() {
		if connClient != nil {
			connClient.Close()
		} else {
			s.writeConnFail(c.Conn)
		}
		c.Close()
	}()

// reset标签：用于连接重置时的跳转点
reset:
	// 如果是重置连接，需要重新增加客户端连接计数
	if isReset {
		host.Client.AddConn()
	}
	
	// 根据请求的Host头查找对应的主机配置
	if host, err = file.GetDb().GetInfoByHost(r.Host, r); err != nil {
		logs.Notice("the url %s %s %s can't be parsed!", r.URL.Scheme, r.Host, r.RequestURI)
		return
	}
	
	// 检查客户端的流量限制和连接数限制
	if err := s.CheckFlowAndConnNum(host.Client); err != nil {
		logs.Warn("client id %d, host id %d, error %s, when https connection", host.Client.Id, host.Id, err.Error())
		return
	}
	
	// 如果不是重置连接，增加客户端连接计数
	if !isReset {
		defer host.Client.AddConn()
	}
	
	// 执行HTTP基础认证检查
	if err = s.auth(r, c, host.Client.Cnf.U, host.Client.Cnf.P); err != nil {
		logs.Warn("auth error", err, r.RemoteAddr)
		return
	}
	
	// 获取目标服务器的随机地址（支持负载均衡）
	if targetAddr, err = host.Target.GetRandomTarget(); err != nil {
		logs.Warn(err.Error())
		return
	}
	
	// 创建连接链路信息，包含目标地址、加密、压缩等配置
	lk = conn.NewLink("http", targetAddr, host.Client.Cnf.Crypt, host.Client.Cnf.Compress, r.RemoteAddr, host.Target.LocalProxy)
	
	// 通过桥接器向客户端发送连接信息，建立代理通道
	if target, err = s.bridge.SendLinkInfo(host.Client.Id, lk, nil); err != nil {
		logs.Notice("connect to target %s error %s", lk.Host, err)
		return
	}
	
	// 创建带加密、压缩和限速功能的客户端连接
	connClient = conn.GetConn(target, lk.Crypt, lk.Compress, host.Client.Rate, true)

	// 启动协程：从目标服务器读取响应并转发给客户端
	go func() {
		wg.Add(1)
		isReset = false
		defer connClient.Close()
		defer func() {
			wg.Done()
			if !isReset {
				c.Close()
			}
		}()
		
		// 持续读取目标服务器的响应
		for {
			if resp, err := http.ReadResponse(bufio.NewReader(connClient), r); err != nil || resp == nil || r == nil {
				// 如果读取响应失败或连接断开，退出循环
				return
			} else {
				// 如果启用了缓存且请求的是静态资源（包含文件扩展名）
				if s.useCache && r.URL != nil && strings.Contains(r.URL.Path, ".") {
					// 将响应序列化为字节数组
					b, err := httputil.DumpResponse(resp, true)
					if err != nil {
						return
					}
					// 直接写入响应到客户端
					c.Write(b)
					// 更新流量统计
					host.Flow.Add(0, int64(len(b)))
					// 将响应存储到缓存中
					s.cache.Add(filepath.Join(host.Host, r.URL.Path), b)
				} else {
					// 创建带长度统计的连接包装器
					lenConn := conn.NewLenConn(c)
					// 将响应写入客户端连接
					if err := resp.Write(lenConn); err != nil {
						logs.Error(err)
						return
					}
					// 更新流量统计
					host.Flow.Add(0, int64(lenConn.Len))
				}
			}
		}
	}()

	// 主循环：处理来自客户端的请求
	for {
		// 如果启用了缓存，检查是否有缓存的响应
		if s.useCache {
			if v, ok := s.cache.Get(filepath.Join(host.Host, r.URL.Path)); ok {
				// 直接返回缓存的响应
				n, err := c.Write(v.([]byte))
				if err != nil {
					break
				}
				logs.Trace("%s request, method %s, host %s, url %s, remote address %s, return cache", r.URL.Scheme, r.Method, r.Host, r.URL.Path, c.RemoteAddr().String())
				// 更新流量统计
				host.Flow.Add(0, int64(n))
				// 如果连接设置为关闭或未设置，则关闭连接
				if strings.ToLower(r.Header.Get("Connection")) == "close" || strings.ToLower(r.Header.Get("Connection")) == "" {
					break
				}
				goto readReq
			}
		}

		// 修改请求的Host头和自定义Header，设置代理相关配置
		common.ChangeHostAndHeader(r, host.HostChange, host.HeaderChange, c.Conn.RemoteAddr().String(), s.addOrigin)
		logs.Trace("%s request, method %s, host %s, url %s, remote address %s, target %s", r.URL.Scheme, r.Method, r.Host, r.URL.Path, c.RemoteAddr().String(), lk.Host)
		
		// 将修改后的请求转发给目标服务器
		lenConn = conn.NewLenConn(connClient)
		if err := r.Write(lenConn); err != nil {
			logs.Error(err)
			break
		}
		// 更新出站流量统计
		host.Flow.Add(int64(lenConn.Len), 0)

	// readReq标签：读取下一个请求
	readReq:
		// 从客户端连接读取下一个HTTP请求
		if r, err = http.ReadRequest(bufio.NewReader(c)); err != nil {
			break
		}
		// 保持原始协议类型
		r.URL.Scheme = scheme
		// 修复请求方法（处理可能的字符丢失问题）
		r.Method = resetReqMethod(r.Method)
		
		// 重新查找主机配置（支持动态路由）
		if hostTmp, err := file.GetDb().GetInfoByHost(r.Host, r); err != nil {
			logs.Notice("the url %s %s %s can't be parsed!", r.URL.Scheme, r.Host, r.RequestURI)
			break
		} else if host != hostTmp {
			// 如果主机配置发生变化，需要重置连接
			host = hostTmp
			isReset = true
			connClient.Close()
			goto reset
		}
	}
	// 等待响应处理协程完成
	wg.Wait()
}

// resetReqMethod 修复HTTP请求方法
// 处理某些情况下请求方法字符丢失的问题
// method: 原始请求方法
// 返回: 修复后的请求方法
func resetReqMethod(method string) string {
	if method == "ET" {
		return "GET"
	}
	if method == "OST" {
		return "POST"
	}
	return method
}

// NewServer 创建HTTP服务器实例
// port: 监听端口
// scheme: 协议类型（http/https）
// 返回: 配置完成的HTTP服务器实例
func (s *httpServer) NewServer(port int, scheme string) *http.Server {
	return &http.Server{
		Addr: ":" + strconv.Itoa(port),
		// 设置请求处理器，将所有请求转发给隧道处理方法
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Scheme = scheme
			s.handleTunneling(w, r)
		}),
		// 禁用HTTP/2，确保与代理功能的兼容性
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
}
