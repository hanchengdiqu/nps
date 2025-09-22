// 此模块用于端口复用
// 根据协议的不同区分客户端、web管理器、HTTP和HTTPS
package pmux

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"ehang.io/nps/lib/common"
	"github.com/astaxie/beego/logs"
	"github.com/pkg/errors"
)

// HTTP方法的数字标识常量
// 这些常量用于快速识别HTTP请求的方法类型
const (
	HTTP_GET        = 716984  // GET请求标识
	HTTP_POST       = 807983  // POST请求标识
	HTTP_HEAD       = 726965  // HEAD请求标识
	HTTP_PUT        = 808585  // PUT请求标识
	HTTP_DELETE     = 686976  // DELETE请求标识
	HTTP_CONNECT    = 677978  // CONNECT请求标识
	HTTP_OPTIONS    = 798084  // OPTIONS请求标识
	HTTP_TRACE      = 848265  // TRACE请求标识
	CLIENT          = 848384  // 客户端连接标识
	ACCEPT_TIME_OUT = 10      // 接受连接的超时时间（秒）
)

// PortMux 端口复用器结构体
// 用于在单个端口上复用多种协议（HTTP、HTTPS、客户端连接、管理连接）
type PortMux struct {
	net.Listener                    // 嵌入的网络监听器
	port        int                 // 监听的端口号
	isClose     bool                // 是否已关闭的标志
	managerHost string              // 管理器主机地址
	clientConn  chan *PortConn      // 客户端连接通道
	httpConn    chan *PortConn      // HTTP连接通道
	httpsConn   chan *PortConn      // HTTPS连接通道
	managerConn chan *PortConn      // 管理器连接通道
}

// NewPortMux 创建新的端口复用器
// port: 要监听的端口号
// managerHost: 管理器主机地址，用于区分管理连接和普通HTTP连接
// 返回: 初始化后的PortMux实例
func NewPortMux(port int, managerHost string) *PortMux {
	pMux := &PortMux{
		managerHost: managerHost,
		port:        port,
		clientConn:  make(chan *PortConn),
		httpConn:    make(chan *PortConn),
		httpsConn:   make(chan *PortConn),
		managerConn: make(chan *PortConn),
	}
	pMux.Start()
	return pMux
}

// Start 启动端口复用器
// 开始监听指定端口，并为每个新连接启动处理协程
// 返回: 启动过程中的错误，如果有的话
func (pMux *PortMux) Start() error {
	// 端口复用仅基于TCP协议
	tcpAddr, err := net.ResolveTCPAddr("tcp", "0.0.0.0:"+strconv.Itoa(pMux.port))
	if err != nil {
		return err
	}
	pMux.Listener, err = net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		logs.Error(err)
		os.Exit(0)
	}
	// 启动接受连接的协程
	go func() {
		for {
			conn, err := pMux.Listener.Accept()
			if err != nil {
				logs.Warn(err)
				// 发生错误时关闭复用器
				pMux.Close()
			}
			// 为每个连接启动处理协程
			go pMux.process(conn)
		}
	}()
	return nil
}

// process 处理单个连接
// 根据连接的前几个字节识别协议类型，并将连接分发到相应的通道
// conn: 要处理的网络连接
func (pMux *PortMux) process(conn net.Conn) {
	// 根据不同的标识进行识别
	// 读取前3个字节
	buf := make([]byte, 3)
	if n, err := io.ReadFull(conn, buf); err != nil || n != 3 {
		return
	}
	var ch chan *PortConn  // 目标通道
	var rs []byte          // 要传递的数据
	var buffer bytes.Buffer
	var readMore = false   // 是否需要读取更多数据

	// 根据前3个字节的数值判断协议类型
	switch common.BytesToNum(buf) {
	case HTTP_CONNECT, HTTP_DELETE, HTTP_GET, HTTP_HEAD, HTTP_OPTIONS, HTTP_POST, HTTP_PUT, HTTP_TRACE: 
		// HTTP请求和管理器连接
		buffer.Reset()
		r := bufio.NewReader(conn)
		buffer.Write(buf)
		// 逐行读取HTTP头部
		for {
			b, _, err := r.ReadLine()
			if err != nil {
				logs.Warn("read line error", err.Error())
				conn.Close()
				break
			}
			buffer.Write(b)
			buffer.Write([]byte("\r\n"))
			// 查找Host头部来区分管理连接和普通HTTP连接
			if strings.Index(string(b), "Host:") == 0 || strings.Index(string(b), "host:") == 0 {
				// 移除host和空格的影响
				str := strings.Replace(string(b), "Host:", "", -1)
				str = strings.Replace(str, "host:", "", -1)
				str = strings.TrimSpace(str)
				// 判断是否与管理器域名相同
				if common.GetIpByAddr(str) == pMux.managerHost {
					ch = pMux.managerConn
				} else {
					ch = pMux.httpConn
				}
				// 读取缓冲区中剩余的数据
				b, _ := r.Peek(r.Buffered())
				buffer.Write(b)
				rs = buffer.Bytes()
				break
			}
		}
	case CLIENT: 
		// 客户端连接
		ch = pMux.clientConn
	default: 
		// HTTPS连接（默认情况）
		readMore = true
		ch = pMux.httpsConn
	}
	
	// 如果没有读取到额外数据，使用原始的3字节
	if len(rs) == 0 {
		rs = buf
	}
	
	// 设置超时定时器，防止通道阻塞
	timer := time.NewTimer(ACCEPT_TIME_OUT)
	select {
	case <-timer.C:
		// 超时，丢弃连接
	case ch <- newPortConn(conn, rs, readMore):
		// 成功发送到对应通道
	}
}

// Close 关闭端口复用器
// 关闭所有通道和底层监听器
// 返回: 关闭过程中的错误，如果有的话
func (pMux *PortMux) Close() error {
	if pMux.isClose {
		return errors.New("the port pmux has closed")
	}
	pMux.isClose = true
	// 关闭所有连接通道
	close(pMux.clientConn)
	close(pMux.httpsConn)
	close(pMux.httpConn)
	close(pMux.managerConn)
	return pMux.Listener.Close()
}

// GetClientListener 获取客户端连接监听器
// 返回: 用于接受客户端连接的监听器
func (pMux *PortMux) GetClientListener() net.Listener {
	return NewPortListener(pMux.clientConn, pMux.Listener.Addr())
}

// GetHttpListener 获取HTTP连接监听器
// 返回: 用于接受HTTP连接的监听器
func (pMux *PortMux) GetHttpListener() net.Listener {
	return NewPortListener(pMux.httpConn, pMux.Listener.Addr())
}

// GetHttpsListener 获取HTTPS连接监听器
// 返回: 用于接受HTTPS连接的监听器
func (pMux *PortMux) GetHttpsListener() net.Listener {
	return NewPortListener(pMux.httpsConn, pMux.Listener.Addr())
}

// GetManagerListener 获取管理器连接监听器
// 返回: 用于接受管理器连接的监听器
func (pMux *PortMux) GetManagerListener() net.Listener {
	return NewPortListener(pMux.managerConn, pMux.Listener.Addr())
}
