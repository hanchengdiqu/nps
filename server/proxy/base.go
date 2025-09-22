// Package proxy 提供 NPS（内网穿透代理）服务器的核心代理功能
// 包含基础代理服务器实现、流量统计、连接管理、认证检查等核心功能
// 支持多种代理模式：HTTP、HTTPS、TCP、UDP、SOCKS5、P2P 等
package proxy

import (
	"errors"
	"net"
	"net/http"
	"sync"

	"ehang.io/nps/bridge"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego/logs"
)

// Service 定义了代理服务的基本生命周期接口
// 所有代理服务（HTTP、TCP、UDP等）都必须实现此接口
type Service interface {
	Start() error // 启动代理服务
	Close() error // 关闭代理服务
}

// NetBridge 定义了网络桥接接口
// 负责在客户端和代理服务器之间建立连接通道
type NetBridge interface {
	// SendLinkInfo 向指定客户端发送连接信息，建立代理通道
	// clientId: 客户端ID
	// link: 连接信息（包含目标地址、加密、压缩等配置）
	// t: 隧道配置信息
	// 返回: 与客户端建立的目标连接
	SendLinkInfo(clientId int, link *conn.Link, t *file.Tunnel) (target net.Conn, err error)
}

// BaseServer 是代理服务器的基础结构体
// 提供所有代理服务共用的核心功能：流量统计、连接管理、认证检查等
type BaseServer struct {
	id           int           // 服务器实例ID
	bridge       NetBridge     // 网络桥接器，用于与客户端通信
	task         *file.Tunnel  // 隧道配置信息
	errorContent []byte        // 错误页面内容，用于连接失败时返回
	sync.Mutex                 // 互斥锁，保护并发访问
}

// NewBaseServer 创建新的基础代理服务器实例
// bridge: 网络桥接器实例
// task: 隧道配置信息
// 返回: 初始化完成的 BaseServer 实例
func NewBaseServer(bridge *bridge.Bridge, task *file.Tunnel) *BaseServer {
	return &BaseServer{
		bridge:       bridge,
		task:         task,
		errorContent: nil,
		Mutex:        sync.Mutex{},
	}
}

// FlowAdd 增加隧道的流量统计
// 线程安全地更新隧道的入站和出站流量
// in: 入站流量（字节数）
// out: 出站流量（字节数）
func (s *BaseServer) FlowAdd(in, out int64) {
	s.Lock()
	defer s.Unlock()
	s.task.Flow.ExportFlow += out  // 累加出站流量
	s.task.Flow.InletFlow += in    // 累加入站流量
}

// FlowAddHost 增加主机的流量统计
// 用于统计特定主机的流量使用情况
// host: 主机配置信息
// in: 入站流量（字节数）
// out: 出站流量（字节数）
func (s *BaseServer) FlowAddHost(host *file.Host, in, out int64) {
	s.Lock()
	defer s.Unlock()
	host.Flow.ExportFlow += out  // 累加主机出站流量
	host.Flow.InletFlow += in    // 累加主机入站流量
}

// writeConnFail 向连接写入失败响应
// 当连接建立失败时，向客户端返回错误页面
// c: 目标连接
func (s *BaseServer) writeConnFail(c net.Conn) {
	c.Write([]byte(common.ConnectionFailBytes)) // 写入连接失败标志
	c.Write(s.errorContent)                     // 写入错误页面内容
}

// auth 执行HTTP基础认证检查
// 验证请求的用户名和密码是否匹配
// r: HTTP请求对象
// c: 连接对象
// u: 期望的用户名
// p: 期望的密码
// 返回: 认证失败时返回错误，成功时返回nil
func (s *BaseServer) auth(r *http.Request, c *conn.Conn, u, p string) error {
	// 如果配置了用户名和密码，则进行认证检查
	if u != "" && p != "" && !common.CheckAuth(r, u, p) {
		c.Write([]byte(common.UnauthorizedBytes)) // 写入401未授权响应
		c.Close()                                 // 关闭连接
		return errors.New("401 Unauthorized")
	}
	return nil
}

// CheckFlowAndConnNum 检查客户端的流量限制和连接数限制
// 确保客户端未超过配置的流量上限和最大连接数
// client: 客户端配置信息
// 返回: 超过限制时返回错误，否则返回nil
func (s *BaseServer) CheckFlowAndConnNum(client *file.Client) error {
	// 检查流量限制：如果设置了流量限制且当前流量超过限制
	if client.Flow.FlowLimit > 0 && (client.Flow.FlowLimit<<20) < (client.Flow.ExportFlow+client.Flow.InletFlow) {
		return errors.New("Traffic exceeded")
	}
	// 检查连接数限制：尝试获取新连接，如果失败说明超过最大连接数
	if !client.GetConn() {
		return errors.New("Connections exceed the current client limit")
	}
	return nil
}

// DealClient 处理客户端连接的核心方法
// 建立代理连接并开始数据转发
// c: 客户端连接
// client: 客户端配置信息
// addr: 目标地址
// rb: 预读的字节数据（用于协议预处理）
// tp: 连接类型（tcp/http等）
// f: 连接建立后的回调函数
// flow: 流量统计对象
// localProxy: 是否为本地代理模式
// 返回: 处理过程中的错误
func (s *BaseServer) DealClient(c *conn.Conn, client *file.Client, addr string, rb []byte, tp string, f func(), flow *file.Flow, localProxy bool) error {
	// 创建连接信息对象，包含目标地址、加密、压缩等配置
	link := conn.NewLink(tp, addr, client.Cnf.Crypt, client.Cnf.Compress, c.Conn.RemoteAddr().String(), localProxy)
	
	// 通过桥接器向客户端发送连接信息，建立代理通道
	if target, err := s.bridge.SendLinkInfo(client.Id, link, s.task); err != nil {
		logs.Warn("get connection from client id %d  error %s", client.Id, err.Error())
		c.Close()
		return err
	} else {
		// 如果提供了回调函数，在连接建立后执行
		if f != nil {
			f()
		}
		// 开始双向数据转发，并统计流量
		conn.CopyWaitGroup(target, c.Conn, link.Crypt, link.Compress, client.Rate, flow, true, rb)
	}
	return nil
}
