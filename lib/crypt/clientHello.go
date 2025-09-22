// Package crypt 提供了TLS加密相关的功能
// 本文件实现了TLS ClientHello消息的解析功能，用于处理TLS握手过程中客户端发送的Hello消息
package crypt

import (
	"strings"
)

// CurveID 表示椭圆曲线标识符，用于TLS握手中的椭圆曲线密钥交换
type CurveID uint16

// SignatureScheme 表示签名算法标识符，用于TLS握手中的数字签名
type SignatureScheme uint16

// TLS扩展类型和状态类型常量定义
const (
	// statusTypeOCSP OCSP状态请求类型
	statusTypeOCSP uint8 = 1

	// TLS扩展类型标识符 (RFC 5246, RFC 6066等)
	extensionServerName          uint16 = 0     // 服务器名称指示(SNI)扩展
	extensionStatusRequest       uint16 = 5     // 证书状态请求扩展
	extensionSupportedCurves     uint16 = 10    // 支持的椭圆曲线扩展
	extensionSupportedPoints     uint16 = 11    // 支持的椭圆曲线点格式扩展
	extensionSignatureAlgorithms uint16 = 13    // 支持的签名算法扩展
	extensionALPN                uint16 = 16    // 应用层协议协商(ALPN)扩展
	extensionSCT                 uint16 = 18    // 签名证书时间戳扩展 https://tools.ietf.org/html/rfc6962#section-6
	extensionSessionTicket       uint16 = 35    // 会话票据扩展
	extensionNextProtoNeg        uint16 = 13172 // 下一协议协商扩展 (非IANA分配)
	extensionRenegotiationInfo   uint16 = 0xff01 // 重新协商信息扩展

	// scsvRenegotiation 安全重新协商信号密码套件值
	scsvRenegotiation uint16 = 0x00ff
)

// ClientHelloMsg 表示TLS握手过程中客户端发送的ClientHello消息
// 该结构体包含了客户端支持的TLS版本、加密套件、扩展等信息
type ClientHelloMsg struct {
	// raw 原始的ClientHello消息字节数据
	raw []byte
	
	// vers 客户端支持的TLS协议版本
	vers uint16
	
	// random 客户端生成的32字节随机数，用于密钥生成
	random []byte
	
	// sessionId 会话ID，用于会话恢复
	sessionId []byte
	
	// cipherSuites 客户端支持的密码套件列表
	cipherSuites []uint16
	
	// compressionMethods 客户端支持的压缩方法列表
	compressionMethods []uint8
	
	// nextProtoNeg 是否支持下一协议协商(NPN)
	nextProtoNeg bool
	
	// serverName 服务器名称指示(SNI)，用于虚拟主机
	serverName string
	
	// ocspStapling 是否支持OCSP装订
	ocspStapling bool
	
	// scts 是否支持签名证书时间戳
	scts bool
	
	// supportedCurves 客户端支持的椭圆曲线列表
	supportedCurves []CurveID
	
	// supportedPoints 客户端支持的椭圆曲线点格式列表
	supportedPoints []uint8
	
	// ticketSupported 是否支持会话票据
	ticketSupported bool
	
	// sessionTicket 会话票据数据
	sessionTicket []uint8
	
	// supportedSignatureAlgorithms 客户端支持的签名算法列表
	supportedSignatureAlgorithms []SignatureScheme
	
	// secureRenegotiation 安全重新协商数据
	secureRenegotiation []byte
	
	// secureRenegotiationSupported 是否支持安全重新协商
	secureRenegotiationSupported bool
	
	// alpnProtocols 应用层协议协商(ALPN)支持的协议列表
	alpnProtocols []string
}

// GetServerName 返回ClientHello消息中的服务器名称指示(SNI)
// SNI用于在单个IP地址上托管多个SSL证书的虚拟主机场景
func (m *ClientHelloMsg) GetServerName() string {
	return m.serverName
}

// Unmarshal 解析TLS ClientHello消息的原始字节数据
// 该方法按照TLS协议规范解析ClientHello消息，提取出各种字段和扩展信息
//
// 参数:
//   data - 包含ClientHello消息的原始字节数据
//
// 返回值:
//   bool - 解析成功返回true，解析失败返回false
//
// 解析过程包括:
//   1. 基本字段解析：版本号、随机数、会话ID
//   2. 密码套件和压缩方法解析
//   3. TLS扩展解析：SNI、ALPN、OCSP等
func (m *ClientHelloMsg) Unmarshal(data []byte) bool {
	// 检查最小长度：4字节消息头 + 2字节版本 + 32字节随机数 + 1字节会话ID长度 + 至少3字节其他数据
	if len(data) < 42 {
		return false
	}
	
	// 保存原始数据
	m.raw = data
	
	// 解析TLS版本号（2字节，大端序）
	m.vers = uint16(data[4])<<8 | uint16(data[5])
	
	// 解析客户端随机数（32字节）
	m.random = data[6:38]
	
	// 解析会话ID长度和会话ID
	sessionIdLen := int(data[38])
	if sessionIdLen > 32 || len(data) < 39+sessionIdLen {
		return false
	}
	m.sessionId = data[39 : 39+sessionIdLen]
	data = data[39+sessionIdLen:]
	
	// 解析密码套件
	if len(data) < 2 {
		return false
	}
	// cipherSuiteLen is the number of bytes of cipher suite numbers. Since
	// they are uint16s, the number must be even.
	cipherSuiteLen := int(data[0])<<8 | int(data[1])
	if cipherSuiteLen%2 == 1 || len(data) < 2+cipherSuiteLen {
		return false
	}
	numCipherSuites := cipherSuiteLen / 2
	m.cipherSuites = make([]uint16, numCipherSuites)
	for i := 0; i < numCipherSuites; i++ {
		m.cipherSuites[i] = uint16(data[2+2*i])<<8 | uint16(data[3+2*i])
		// 检查是否支持安全重新协商
		if m.cipherSuites[i] == scsvRenegotiation {
			m.secureRenegotiationSupported = true
		}
	}
	data = data[2+cipherSuiteLen:]
	
	// 解析压缩方法
	if len(data) < 1 {
		return false
	}
	compressionMethodsLen := int(data[0])
	if len(data) < 1+compressionMethodsLen {
		return false
	}
	m.compressionMethods = data[1 : 1+compressionMethodsLen]
	data = data[1+compressionMethodsLen:]

	// 初始化扩展相关字段为默认值
	m.nextProtoNeg = false
	m.serverName = ""
	m.ocspStapling = false
	m.ticketSupported = false
	m.sessionTicket = nil
	m.supportedSignatureAlgorithms = nil
	m.alpnProtocols = nil
	m.scts = false

	// 检查是否有扩展数据
	if len(data) == 0 {
		// ClientHello is optionally followed by extension data
		return true
	}
	if len(data) < 2 {
		return false
	}

	// 解析扩展数据长度
	extensionsLength := int(data[0])<<8 | int(data[1])
	data = data[2:]
	if extensionsLength != len(data) {
		return false
	}

	// 逐个解析TLS扩展
	for len(data) != 0 {
		if len(data) < 4 {
			return false
		}
		// 解析扩展类型和长度
		extension := uint16(data[0])<<8 | uint16(data[1])
		length := int(data[2])<<8 | int(data[3])
		data = data[4:]
		if len(data) < length {
			return false
		}

		switch extension {
		case extensionServerName:
			// 解析服务器名称指示(SNI)扩展
			d := data[:length]
			if len(d) < 2 {
				return false
			}
			namesLen := int(d[0])<<8 | int(d[1])
			d = d[2:]
			if len(d) != namesLen {
				return false
			}
			for len(d) > 0 {
				if len(d) < 3 {
					return false
				}
				nameType := d[0]
				nameLen := int(d[1])<<8 | int(d[2])
				d = d[3:]
				if len(d) < nameLen {
					return false
				}
				if nameType == 0 {
					m.serverName = string(d[:nameLen])
					// An SNI value may not include a
					// trailing dot. See
					// https://tools.ietf.org/html/rfc6066#section-3.
					if strings.HasSuffix(m.serverName, ".") {
						return false
					}
					break
				}
				d = d[nameLen:]
			}
		case extensionNextProtoNeg:
			// 解析下一协议协商扩展
			if length > 0 {
				return false
			}
			m.nextProtoNeg = true
		case extensionStatusRequest:
			// 解析证书状态请求扩展(OCSP)
			m.ocspStapling = length > 0 && data[0] == statusTypeOCSP
		case extensionSupportedCurves:
			// 解析支持的椭圆曲线扩展
			// https://tools.ietf.org/html/rfc4492#section-5.5.1
			if length < 2 {
				return false
			}
			l := int(data[0])<<8 | int(data[1])
			if l%2 == 1 || length != l+2 {
				return false
			}
			numCurves := l / 2
			m.supportedCurves = make([]CurveID, numCurves)
			d := data[2:]
			for i := 0; i < numCurves; i++ {
				m.supportedCurves[i] = CurveID(d[0])<<8 | CurveID(d[1])
				d = d[2:]
			}
		case extensionSupportedPoints:
			// 解析支持的椭圆曲线点格式扩展
			// https://tools.ietf.org/html/rfc4492#section-5.5.2
			if length < 1 {
				return false
			}
			l := int(data[0])
			if length != l+1 {
				return false
			}
			m.supportedPoints = make([]uint8, l)
			copy(m.supportedPoints, data[1:])
		case extensionSessionTicket:
			// 解析会话票据扩展
			// https://tools.ietf.org/html/rfc5077#section-3.2
			m.ticketSupported = true
			m.sessionTicket = data[:length]
		case extensionSignatureAlgorithms:
			// 解析支持的签名算法扩展
			// https://tools.ietf.org/html/rfc5246#section-7.4.1.4.1
			if length < 2 || length&1 != 0 {
				return false
			}
			l := int(data[0])<<8 | int(data[1])
			if l != length-2 {
				return false
			}
			n := l / 2
			d := data[2:]
			m.supportedSignatureAlgorithms = make([]SignatureScheme, n)
			for i := range m.supportedSignatureAlgorithms {
				m.supportedSignatureAlgorithms[i] = SignatureScheme(d[0])<<8 | SignatureScheme(d[1])
				d = d[2:]
			}
		case extensionRenegotiationInfo:
			// 解析重新协商信息扩展
			if length == 0 {
				return false
			}
			d := data[:length]
			l := int(d[0])
			d = d[1:]
			if l != len(d) {
				return false
			}

			m.secureRenegotiation = d
			m.secureRenegotiationSupported = true
		case extensionALPN:
			// 解析应用层协议协商(ALPN)扩展
			if length < 2 {
				return false
			}
			l := int(data[0])<<8 | int(data[1])
			if l != length-2 {
				return false
			}
			d := data[2:length]
			for len(d) != 0 {
				stringLen := int(d[0])
				d = d[1:]
				if stringLen == 0 || stringLen > len(d) {
					return false
				}
				m.alpnProtocols = append(m.alpnProtocols, string(d[:stringLen]))
				d = d[stringLen:]
			}
		case extensionSCT:
			// 解析签名证书时间戳扩展
			m.scts = true
			if length != 0 {
				return false
			}
		}
		data = data[length:]
	}

	return true
}
