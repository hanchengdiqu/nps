package pmux

import (
	"errors"
	"net"
)

// PortListener 端口监听器结构体
// 实现了net.Listener接口，用于从特定的连接通道中接受连接
// 这是端口复用器的一个组件，每种协议类型都有对应的PortListener
type PortListener struct {
	net.Listener                // 嵌入的网络监听器接口
	connCh  chan *PortConn      // 连接通道，用于接收特定类型的连接
	addr    net.Addr            // 监听器的网络地址
	isClose bool                // 监听器是否已关闭的标志
}

// NewPortListener 创建新的端口监听器
// connCh: 用于接收连接的通道，通常来自PortMux的某个协议通道
// addr: 监听器的网络地址，通常是底层TCP监听器的地址
// 返回: 初始化后的PortListener实例
func NewPortListener(connCh chan *PortConn, addr net.Addr) *PortListener {
	return &PortListener{
		connCh: connCh,
		addr:   addr,
	}
}

// Accept 接受一个连接
// 实现net.Listener接口的Accept方法
// 从连接通道中获取下一个可用的连接
// 返回: 网络连接和可能的错误
func (pListener *PortListener) Accept() (net.Conn, error) {
	// 检查监听器是否已关闭
	if pListener.isClose {
		return nil, errors.New("the listener has closed")
	}
	// 从通道中接收连接，这里会阻塞直到有连接可用
	conn := <-pListener.connCh
	if conn != nil {
		return conn, nil
	}
	// 如果接收到nil连接，说明通道已关闭
	return nil, errors.New("the listener has closed")
}

// Close 关闭监听器
// 实现net.Listener接口的Close方法
// 设置关闭标志，但不关闭底层通道（通道由PortMux管理）
// 返回: 可能的错误
func (pListener *PortListener) Close() error {
	// 检查是否已经关闭
	if pListener.isClose {
		return errors.New("the listener has closed")
	}
	// 设置关闭标志
	pListener.isClose = true
	return nil
}

// Addr 返回监听器的网络地址
// 实现net.Listener接口的Addr方法
// 返回: 监听器绑定的网络地址
func (pListener *PortListener) Addr() net.Addr {
	return pListener.addr
}
