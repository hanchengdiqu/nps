// Package goroutine 提供了用于NPS内网穿透服务的协程池管理功能
// 主要用于高效地管理连接之间的数据复制操作，通过协程池来控制并发数量，
// 避免创建过多的goroutine导致系统资源耗尽
package goroutine

import (
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"github.com/panjf2000/ants/v2"
	"io"
	"net"
	"sync"
)

// connGroup 表示一个连接组，用于在两个连接之间复制数据
// 这是协程池中单个任务的数据结构
type connGroup struct {
	src io.ReadWriteCloser // 源连接，数据从这里读取
	dst io.ReadWriteCloser // 目标连接，数据写入到这里
	wg  *sync.WaitGroup    // 等待组，用于同步操作完成
	n   *int64             // 传输的字节数统计
}

// newConnGroup 创建一个新的连接组实例
// dst: 目标连接
// src: 源连接
// wg: 等待组
// n: 字节数统计指针
func newConnGroup(dst, src io.ReadWriteCloser, wg *sync.WaitGroup, n *int64) connGroup {
	return connGroup{
		src: src,
		dst: dst,
		wg:  wg,
		n:   n,
	}
}

// copyConnGroup 是协程池中执行的工作函数，负责在连接组之间复制数据
// 这个函数会被协程池调用来处理单向的数据复制
// group: 传入的连接组接口，需要转换为connGroup类型
func copyConnGroup(group interface{}) {
	cg, ok := group.(connGroup)
	if !ok {
		return
	}
	var err error
	// 使用缓冲复制来提高性能，并记录传输的字节数
	*cg.n, err = common.CopyBuffer(cg.dst, cg.src)
	if err != nil {
		// 发生错误时关闭连接
		cg.src.Close()
		cg.dst.Close()
		//logs.Warn("close npc by copy from nps", err, c.connId)
	}
	// 通知等待组任务完成
	cg.wg.Done()
}

// Conns 表示一对需要进行双向数据复制的连接
// 通常用于代理场景，在客户端连接和服务端连接之间转发数据
type Conns struct {
	conn1 io.ReadWriteCloser // mux connection - 多路复用连接（通常是内部连接）
	conn2 net.Conn           // outside connection - 外部连接（通常是客户端连接）
	flow  *file.Flow         // 流量统计对象，用于记录传输的数据量
	wg    *sync.WaitGroup    // 等待组，用于同步双向复制操作的完成
}

// NewConns 创建一个新的连接对实例
// c1: 第一个连接（通常是mux连接）
// c2: 第二个连接（通常是外部连接）
// flow: 流量统计对象
// wg: 等待组
func NewConns(c1 io.ReadWriteCloser, c2 net.Conn, flow *file.Flow, wg *sync.WaitGroup) Conns {
	return Conns{
		conn1: c1,
		conn2: c2,
		flow:  flow,
		wg:    wg,
	}
}

// copyConns 是协程池中执行的工作函数，负责在连接对之间进行双向数据复制
// 这个函数会启动两个子任务来分别处理两个方向的数据传输
// group: 传入的连接对接口，需要转换为Conns类型
func copyConns(group interface{}) {
	conns := group.(Conns)
	wg := new(sync.WaitGroup)
	wg.Add(2) // 需要等待两个方向的复制完成
	var in, out int64
	
	// 启动第一个方向的数据复制：conn1 -> conn2
	_ = connCopyPool.Invoke(newConnGroup(conns.conn1, conns.conn2, wg, &in))
	// outside to mux : incoming - 外部到多路复用：入站流量
	
	// 启动第二个方向的数据复制：conn2 -> conn1  
	_ = connCopyPool.Invoke(newConnGroup(conns.conn2, conns.conn1, wg, &out))
	// mux to outside : outgoing - 多路复用到外部：出站流量
	
	// 等待两个方向的复制都完成
	wg.Wait()
	
	// 如果有流量统计对象，记录传输的数据量
	if conns.flow != nil {
		conns.flow.Add(in, out)
	}
	
	// 通知上层等待组任务完成
	conns.wg.Done()
}

// connCopyPool 单向连接复制的协程池
// 容量为200000个goroutine，用于处理单向的数据复制任务
// 使用阻塞模式，当池满时会等待可用的goroutine
var connCopyPool, _ = ants.NewPoolWithFunc(200000, copyConnGroup, ants.WithNonblocking(false))

// CopyConnsPool 双向连接复制的协程池  
// 容量为100000个goroutine，用于处理连接对之间的双向数据复制
// 使用阻塞模式，当池满时会等待可用的goroutine
var CopyConnsPool, _ = ants.NewPoolWithFunc(100000, copyConns, ants.WithNonblocking(false))
