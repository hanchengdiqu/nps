// Package conn 提供了 NPS 网络代理系统中的网络监听器创建和连接处理功能。
// 该文件实现了 TCP 和 KCP（基于 UDP 的可靠传输协议）两种网络监听器的创建和管理，
// 为 NPS 代理服务器提供底层的网络连接接受和处理能力。
package conn

import (
	"net"
	"strings"

	"github.com/astaxie/beego/logs"
	"github.com/xtaci/kcp-go"
)

// NewTcpListenerAndProcess 创建一个 TCP 监听器并开始处理连接。
// 该函数会在指定地址上创建 TCP 监听器，然后调用 Accept 函数持续接受新连接。
// 每个新连接都会在独立的 goroutine 中通过提供的处理函数进行处理。
//
// 参数:
//   - addr: 监听地址，格式为 "host:port"（如 ":8080" 或 "127.0.0.1:8080"）
//   - f: 连接处理函数，每个新连接都会调用此函数进行处理
//   - listener: 指向 net.Listener 的指针，用于返回创建的监听器对象
//
// 返回值:
//   - error: 如果监听器创建失败则返回错误，否则返回 nil
//
// 注意: 此函数会阻塞执行，持续接受新连接直到监听器被关闭
func NewTcpListenerAndProcess(addr string, f func(c net.Conn), listener *net.Listener) error {
	var err error
	*listener, err = net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	Accept(*listener, f)
	return nil
}

// NewKcpListenerAndProcess 创建一个 KCP 监听器并开始处理连接。
// KCP 是基于 UDP 的可靠传输协议，提供比 TCP 更低的延迟和更好的网络适应性。
// 该函数会创建 KCP 监听器，并为每个连接配置优化的传输参数。
//
// 参数:
//   - addr: 监听地址，格式为 "host:port"
//   - f: 连接处理函数，每个新连接都会调用此函数进行处理
//
// 返回值:
//   - error: 如果监听器创建失败则返回错误，否则返回 nil
//
// 注意:
//   - 此函数会阻塞执行，持续接受新连接
//   - KCP 参数: dataShards=150, parityShards=3，提供前向纠错能力
//   - 每个连接都会通过 SetUdpSession 进行性能优化配置
func NewKcpListenerAndProcess(addr string, f func(c net.Conn)) error {
	kcpListener, err := kcp.ListenWithOptions(addr, nil, 150, 3)
	if err != nil {
		logs.Error(err)
		return err
	}
	for {
		c, err := kcpListener.AcceptKCP()
		SetUdpSession(c)
		if err != nil {
			logs.Warn(err)
			continue
		}
		go f(c)
	}
	return nil
}

// Accept 持续接受监听器上的新连接并进行处理。
// 该函数是一个通用的连接接受循环，适用于任何实现了 net.Listener 接口的监听器。
// 每个新连接都会在独立的 goroutine 中通过提供的处理函数进行处理。
//
// 参数:
//   - l: 网络监听器，实现 net.Listener 接口
//   - f: 连接处理函数，每个新连接都会调用此函数进行处理
//
// 行为:
//   - 持续循环接受新连接
//   - 对于每个成功的连接，启动新的 goroutine 调用处理函数
//   - 优雅处理监听器关闭和多路复用器关闭的情况
//   - 记录警告日志但继续运行，除非遇到致命错误
//
// 退出条件:
//   - 监听器被关闭（"use of closed network connection"）
//   - 多路复用器被关闭（"the mux has closed"）
//   - 接收到 nil 连接
//
// 注意: 此函数会阻塞执行直到监听器被关闭或发生致命错误
func Accept(l net.Listener, f func(c net.Conn)) {
	for {
		c, err := l.Accept()
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				break
			}
			if strings.Contains(err.Error(), "the mux has closed") {
				break
			}
			logs.Warn(err)
			continue
		}
		if c == nil {
			logs.Warn("nil connection")
			break
		}
		go f(c)
	}
}
