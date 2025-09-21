package conn

import (
	"errors"
	"io"

	"github.com/golang/snappy"
)

type SnappyConn struct {
	w *snappy.Writer
	r *snappy.Reader
	c io.Closer
}

// NewSnappyConn 使用提供的 io.ReadWriteCloser 构造一个基于 snappy 的压缩连接。
// 返回的连接实现了 io.Reader、io.Writer 和 io.Closer 接口。
func NewSnappyConn(conn io.ReadWriteCloser) *SnappyConn {
	c := new(SnappyConn)
	c.w = snappy.NewBufferedWriter(conn)
	c.r = snappy.NewReader(conn)
	c.c = conn.(io.Closer)
	return c
}

// Write 使用 snappy 压缩将数据写入到底层连接。
// 它会在写入后立即调用 Flush 以确保数据被发送出去。
func (s *SnappyConn) Write(b []byte) (n int, err error) {
	if n, err = s.w.Write(b); err != nil {
		return
	}
	if err = s.w.Flush(); err != nil {
		return
	}
	return
}

// Read 从底层连接读取数据，并使用 snappy 解压缩后写入 b。
// 行为与 io.Reader 一致，返回读取的字节数和可能的错误。
func (s *SnappyConn) Read(b []byte) (n int, err error) {
	return s.r.Read(b)
}

// Close 关闭 SnappyConn。
// 它会先关闭 snappy.Writer，再关闭底层连接，并合并两个关闭操作可能出现的错误。
func (s *SnappyConn) Close() error {
	err := s.w.Close()
	err2 := s.c.Close()
	if err != nil && err2 == nil {
		return err
	}
	if err == nil && err2 != nil {
		return err2
	}
	if err != nil && err2 != nil {
		return errors.New(err.Error() + err2.Error())
	}
	return nil
}
