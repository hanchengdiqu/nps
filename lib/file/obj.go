// Package file 定义了NPS系统中的核心数据结构和对象
// 包含客户端、隧道、主机、流量控制等核心组件的定义
package file

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ehang.io/nps/lib/rate"
	"github.com/pkg/errors"
)

// Flow 流量统计结构体，用于记录和控制数据流量
type Flow struct {
	ExportFlow int64        // 出站流量（字节数）
	InletFlow  int64        // 入站流量（字节数）
	FlowLimit  int64        // 流量限制（字节数）
	sync.RWMutex            // 读写锁，保证并发安全
}

// Add 添加流量统计
// in: 入站流量增量
// out: 出站流量增量
func (s *Flow) Add(in, out int64) {
	s.Lock()
	defer s.Unlock()
	s.InletFlow += int64(in)
	s.ExportFlow += int64(out)
}

// Config 客户端配置结构体
type Config struct {
	U        string // 用户名
	P        string // 密码
	Compress bool   // 是否启用压缩
	Crypt    bool   // 是否启用加密
}

// Client 客户端结构体，表示一个连接到NPS服务器的客户端
type Client struct {
	Cnf             *Config    // 客户端配置
	Id              int        // 客户端唯一标识ID
	VerifyKey       string     // 验证密钥
	Addr            string     // 客户端IP地址
	Remark          string     // 备注信息
	Status          bool       // 是否允许连接
	IsConnect       bool       // 是否已连接
	RateLimit       int        // 速率限制（KB/s）
	Flow            *Flow      // 流量统计
	Rate            *rate.Rate // 速率限制器
	NoStore         bool       // 是否不存储到文件
	NoDisplay       bool       // 是否不在Web界面显示
	MaxConn         int        // 允许的最大连接数
	NowConn         int32      // 当前连接数
	WebUserName     string     // Web登录用户名
	WebPassword     string     // Web登录密码
	ConfigConnAllow bool       // 是否允许通过配置文件连接
	MaxTunnelNum    int        // 最大隧道数量
	Version         string     // 客户端版本
	sync.RWMutex               // 读写锁，保证并发安全
}

// NewClient 创建新的客户端实例
// vKey: 验证密钥
// noStore: 是否不存储到文件
// noDisplay: 是否不在Web界面显示
func NewClient(vKey string, noStore bool, noDisplay bool) *Client {
	return &Client{
		Cnf:       new(Config),
		Id:        0,
		VerifyKey: vKey,
		Addr:      "",
		Remark:    "",
		Status:    true,
		IsConnect: false,
		RateLimit: 0,
		Flow:      new(Flow),
		Rate:      nil,
		NoStore:   noStore,
		RWMutex:   sync.RWMutex{},
		NoDisplay: noDisplay,
	}
}

// CutConn 增加当前连接数计数
func (s *Client) CutConn() {
	atomic.AddInt32(&s.NowConn, 1)
}

// AddConn 减少当前连接数计数（方法名可能有歧义，实际是减少连接数）
func (s *Client) AddConn() {
	atomic.AddInt32(&s.NowConn, -1)
}

// GetConn 尝试获取连接，检查是否超过最大连接数限制
// 返回true表示可以建立连接，false表示已达到连接数上限
func (s *Client) GetConn() bool {
	if s.MaxConn == 0 || int(s.NowConn) < s.MaxConn {
		s.CutConn()
		return true
	}
	return false
}

// HasTunnel 检查客户端是否已存在指定的隧道
// t: 要检查的隧道对象
// 返回true表示隧道已存在
func (s *Client) HasTunnel(t *Tunnel) (exist bool) {
	GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		v := value.(*Tunnel)
		if v.Client.Id == s.Id && v.Port == t.Port && t.Port != 0 {
			exist = true
			return false
		}
		return true
	})
	return
}

// GetTunnelNum 获取客户端的隧道数量
func (s *Client) GetTunnelNum() (num int) {
	GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		v := value.(*Tunnel)
		if v.Client.Id == s.Id {
			num++
		}
		return true
	})
	return
}

// HasHost 检查客户端是否已存在指定的主机配置
// h: 要检查的主机对象
// 返回true表示主机配置已存在
func (s *Client) HasHost(h *Host) bool {
	var has bool
	GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
		v := value.(*Host)
		if v.Client.Id == s.Id && v.Host == h.Host && h.Location == v.Location {
			has = true
			return false
		}
		return true
	})
	return has
}

// Tunnel 隧道结构体，表示一个网络隧道配置
type Tunnel struct {
	Id           int           // 隧道唯一标识ID
	Port         int           // 服务器端口
	ServerIp     string        // 服务器IP地址
	Mode         string        // 隧道模式（tcp、udp、http等）
	Status       bool          // 隧道状态（启用/禁用）
	RunStatus    bool          // 运行状态
	Client       *Client       // 关联的客户端
	Ports        string        // 端口范围或多端口配置
	Flow         *Flow         // 流量统计
	Password     string        // 隧道密码
	Remark       string        // 备注信息
	TargetAddr   string        // 目标地址
	NoStore      bool          // 是否不存储到文件
	LocalPath    string        // 本地路径（用于文件服务）
	StripPre     string        // URL前缀剥离
	Target       *Target       // 目标配置
	MultiAccount *MultiAccount // 多账户配置
	Health                     // 健康检查配置
	sync.RWMutex              // 读写锁，保证并发安全
}

// Health 健康检查结构体，用于监控隧道或服务的健康状态
type Health struct {
	HealthCheckTimeout  int           // 健康检查超时时间（秒）
	HealthMaxFail       int           // 最大失败次数
	HealthCheckInterval int           // 健康检查间隔（秒）
	HealthNextTime      time.Time     // 下次检查时间
	HealthMap           map[string]int // 健康状态映射
	HttpHealthUrl       string        // HTTP健康检查URL
	HealthRemoveArr     []string      // 需要移除的不健康目标
	HealthCheckType     string        // 健康检查类型
	HealthCheckTarget   string        // 健康检查目标
	sync.RWMutex                      // 读写锁，保证并发安全
}

// Host 主机结构体，表示HTTP/HTTPS代理的主机配置
type Host struct {
	Id           int     // 主机配置唯一标识ID
	Host         string  // 主机名或域名
	HeaderChange string  // 请求头修改规则
	HostChange   string  // 主机名修改规则
	Location     string  // URL路由路径
	Remark       string  // 备注信息
	Scheme       string  // 协议类型（http、https、all）
	CertFilePath string  // SSL证书文件路径
	KeyFilePath  string  // SSL私钥文件路径
	NoStore      bool    // 是否不存储到文件
	IsClose      bool    // 是否关闭
	Flow         *Flow   // 流量统计
	Client       *Client // 关联的客户端
	Target       *Target // 目标配置
	Health       `json:"-"` // 健康检查配置（JSON序列化时忽略）
	sync.RWMutex            // 读写锁，保证并发安全
}

// Target 目标结构体，用于负载均衡和目标选择
type Target struct {
	nowIndex   int      // 当前选择的目标索引
	TargetStr  string   // 目标字符串（多个目标用换行分隔）
	TargetArr  []string // 目标数组
	LocalProxy bool     // 是否为本地代理
	sync.RWMutex        // 读写锁，保证并发安全
}

// MultiAccount 多账户结构体，用于支持多用户认证
type MultiAccount struct {
	AccountMap map[string]string // 账户映射表（用户名->密码）
}

// GetRandomTarget 获取随机目标地址，实现简单的负载均衡
// 返回目标地址和可能的错误
func (s *Target) GetRandomTarget() (string, error) {
	// 如果目标数组为空，则解析目标字符串
	if s.TargetArr == nil {
		s.TargetArr = strings.Split(s.TargetStr, "\n")
	}
	// 如果只有一个目标，直接返回
	if len(s.TargetArr) == 1 {
		return s.TargetArr[0], nil
	}
	// 如果没有可用目标，返回错误
	if len(s.TargetArr) == 0 {
		return "", errors.New("all inward-bending targets are offline")
	}
	// 使用轮询方式选择目标
	s.Lock()
	defer s.Unlock()
	if s.nowIndex >= len(s.TargetArr)-1 {
		s.nowIndex = -1
	}
	s.nowIndex++
	return s.TargetArr[s.nowIndex], nil
}
