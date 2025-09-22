// Package conn 提供了 NPS 网络代理系统中的连接管理和链路配置功能。
// 该包定义了连接的认证信息、链路配置参数以及连接选项等核心数据结构。
package conn

import "time"

// Secret 表示连接的认证信息，包含密码和对应的连接对象。
// 用于在 NPS 客户端和服务端之间进行身份验证。
type Secret struct {
	Password string // 认证密码
	Conn     *Conn  // 关联的连接对象
}

// NewSecret 创建一个新的 Secret 实例。
// 参数:
//   - p: 认证密码
//   - conn: 关联的连接对象
// 返回值: 新创建的 Secret 指针
func NewSecret(p string, conn *Conn) *Secret {
	return &Secret{
		Password: p,
		Conn:     conn,
	}
}

// Link 表示一个网络链路的配置信息，包含连接类型、目标地址、加密压缩选项等。
// 这是 NPS 代理系统中描述一个代理连接所有必要参数的核心数据结构。
type Link struct {
	ConnType   string  // 连接类型（如 tcp、udp、http、https、socks5 等）
	Host       string  // 目标主机地址
	Crypt      bool    // 是否启用加密传输
	Compress   bool    // 是否启用数据压缩
	LocalProxy bool    // 是否为本地代理模式
	RemoteAddr string  // 远程服务器地址
	Option     Options // 连接选项配置
}

// Option 是一个函数类型，用于配置 Options 结构体。
// 采用函数式选项模式，允许灵活地设置连接参数。
type Option func(*Options)

// Options 包含连接的可选配置参数。
// 目前主要包含超时时间设置，未来可扩展其他选项。
type Options struct {
	Timeout time.Duration // 连接超时时间
}

// defaultTimeOut 定义默认的连接超时时间为 5 秒。
var defaultTimeOut = time.Second * 5

// NewLink 创建一个新的 Link 实例，用于描述一个代理连接的完整配置。
// 参数:
//   - connType: 连接类型（tcp、udp、http、https、socks5 等）
//   - host: 目标主机地址
//   - crypt: 是否启用加密
//   - compress: 是否启用压缩
//   - remoteAddr: 远程服务器地址
//   - localProxy: 是否为本地代理模式
//   - opts: 可变参数，用于设置额外的连接选项
// 返回值: 新创建的 Link 指针
func NewLink(connType string, host string, crypt bool, compress bool, remoteAddr string, localProxy bool, opts ...Option) *Link {
	options := newOptions(opts...)

	return &Link{
		RemoteAddr: remoteAddr,
		ConnType:   connType,
		Host:       host,
		Crypt:      crypt,
		Compress:   compress,
		LocalProxy: localProxy,
		Option:     options,
	}
}

// newOptions 根据提供的选项函数创建 Options 实例。
// 首先设置默认值，然后依次应用所有提供的选项函数。
// 参数:
//   - opts: 选项函数列表
// 返回值: 配置完成的 Options 实例
func newOptions(opts ...Option) Options {
	opt := Options{
		Timeout: defaultTimeOut,
	}
	for _, o := range opts {
		o(&opt)
	}
	return opt
}

// LinkTimeout 返回一个用于设置连接超时时间的选项函数。
// 这是一个选项构造器，符合函数式选项模式。
// 参数:
//   - t: 要设置的超时时间
// 返回值: 配置超时时间的选项函数
func LinkTimeout(t time.Duration) Option {
	return func(opt *Options) {
		opt.Timeout = t
	}
}
