// Package pmux 提供端口复用功能，用于在同一个端口上区分不同类型的连接
// 主要用于NPS代理服务器中，在同一端口上处理客户端连接、HTTP请求、HTTPS请求和管理连接
package pmux

import (
	"net"
	"time"
)

// PortConn 是一个包装了net.Conn的结构体，用于处理端口复用场景下的连接
// 它可以缓存已读取的数据，并在后续的Read操作中先返回缓存的数据
type PortConn struct {
	Conn     net.Conn // 底层的网络连接
	rs       []byte   // 缓存的已读取数据，用于协议识别后重新提供给上层应用
	readMore bool     // 标识是否需要从底层连接继续读取更多数据
	start    int      // 当前在缓存数据中的读取位置
}

// newPortConn 创建一个新的PortConn实例
// conn: 底层的网络连接
// rs: 已经从连接中读取的数据（通常是用于协议识别的数据）
// readMore: 是否允许从底层连接继续读取更多数据
func newPortConn(conn net.Conn, rs []byte, readMore bool) *PortConn {
	return &PortConn{
		Conn:     conn,
		rs:       rs,
		readMore: readMore,
	}
}

// Read 实现了io.Reader接口，优先返回缓存的数据，然后从底层连接读取
// 这个方法的核心逻辑是：
// 1. 如果缓存中还有未读完的数据，优先返回缓存数据
// 2. 如果缓存数据已读完且允许继续读取，则从底层连接读取新数据
// 3. 如果不允许继续读取，则只返回缓存中的数据
func (pConn *PortConn) Read(b []byte) (n int, err error) {
	// 如果目标缓冲区小于剩余缓存数据，只能部分读取缓存数据
	if len(b) < len(pConn.rs)-pConn.start {
		defer func() {
			pConn.start = pConn.start + len(b)
		}()
		return copy(b, pConn.rs), nil
	}
	
	// 如果缓存中还有未读完的数据，先读取缓存数据
	if pConn.start < len(pConn.rs) {
		defer func() {
			pConn.start = len(pConn.rs)
		}()
		n = copy(b, pConn.rs[pConn.start:])
		// 如果不允许继续读取，只返回缓存数据
		if !pConn.readMore {
			return
		}
	}
	
	// 缓存数据已读完，从底层连接继续读取
	var n2 = 0
	n2, err = pConn.Conn.Read(b[n:])
	n = n + n2
	return
}

// Write 实现了io.Writer接口，直接将数据写入底层连接
// 写操作不需要特殊处理，直接透传给底层连接
func (pConn *PortConn) Write(b []byte) (n int, err error) {
	return pConn.Conn.Write(b)
}

// Close 关闭底层连接
// 实现了io.Closer接口
func (pConn *PortConn) Close() error {
	return pConn.Conn.Close()
}

// LocalAddr 返回本地网络地址
// 实现了net.Conn接口的LocalAddr方法
func (pConn *PortConn) LocalAddr() net.Addr {
	return pConn.Conn.LocalAddr()
}

// RemoteAddr 返回远程网络地址
// 实现了net.Conn接口的RemoteAddr方法
func (pConn *PortConn) RemoteAddr() net.Addr {
	return pConn.Conn.RemoteAddr()
}

// SetDeadline 设置读写操作的截止时间
// 实现了net.Conn接口的SetDeadline方法
func (pConn *PortConn) SetDeadline(t time.Time) error {
	return pConn.Conn.SetDeadline(t)
}

// SetReadDeadline 设置读操作的截止时间
// 实现了net.Conn接口的SetReadDeadline方法
func (pConn *PortConn) SetReadDeadline(t time.Time) error {
	return pConn.Conn.SetReadDeadline(t)
}

// SetWriteDeadline 设置写操作的截止时间
// 实现了net.Conn接口的SetWriteDeadline方法
func (pConn *PortConn) SetWriteDeadline(t time.Time) error {
	return pConn.Conn.SetWriteDeadline(t)
}
