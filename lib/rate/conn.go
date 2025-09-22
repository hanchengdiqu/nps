// Package rate 提供了网络连接的流量控制功能
// 本文件实现了带有流量限制的连接包装器，用于控制网络连接的读写速率
package rate

import (
	"io"
)

// rateConn 是一个带有流量控制的连接包装器
// 它包装了原始的连接对象，并在读写操作时应用流量限制
type rateConn struct {
	conn io.ReadWriteCloser // 原始的网络连接
	rate *Rate              // 流量控制器，用于限制读写速率
}

// NewRateConn 创建一个新的带流量控制的连接
// 参数:
//   - conn: 原始的网络连接，必须实现 io.ReadWriteCloser 接口
//   - rate: 流量控制器，如果为 nil 则不进行流量控制
// 返回:
//   - io.ReadWriteCloser: 包装后的连接，具有流量控制功能
func NewRateConn(conn io.ReadWriteCloser, rate *Rate) io.ReadWriteCloser {
	return &rateConn{
		conn: conn,
		rate: rate,
	}
}

// Read 从连接中读取数据，并应用流量控制
// 参数:
//   - b: 用于存储读取数据的字节切片
// 返回:
//   - n: 实际读取的字节数
//   - err: 读取过程中的错误，如果有的话
// 注意: 如果设置了流量控制器，读取的字节数会被计入流量统计
func (s *rateConn) Read(b []byte) (n int, err error) {
	n, err = s.conn.Read(b)
	if s.rate != nil {
		s.rate.Get(int64(n)) // 从流量桶中消耗相应的流量配额
	}
	return
}

// Write 向连接中写入数据，并应用流量控制
// 参数:
//   - b: 要写入的字节切片
// 返回:
//   - n: 实际写入的字节数
//   - err: 写入过程中的错误，如果有的话
// 注意: 如果设置了流量控制器，写入的字节数会被计入流量统计
func (s *rateConn) Write(b []byte) (n int, err error) {
	n, err = s.conn.Write(b)
	if s.rate != nil {
		s.rate.Get(int64(n)) // 从流量桶中消耗相应的流量配额
	}
	return
}

// Close 关闭连接
// 返回:
//   - error: 关闭连接时的错误，如果有的话
// 注意: 此方法直接调用原始连接的 Close 方法，不涉及流量控制
func (s *rateConn) Close() error {
	return s.conn.Close()
}
