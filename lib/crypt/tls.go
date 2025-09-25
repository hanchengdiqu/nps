// Package crypt 提供TLS加密相关的功能
// 主要用于NPS（内网穿透服务）的安全连接，包括：
// 1. 自动生成自签名证书
// 2. 创建TLS服务端连接
// 3. 创建TLS客户端连接
// 4. 提供RSA密钥对生成功能
package crypt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/astaxie/beego/logs"
)

// cert 全局TLS证书，在InitTls()函数中初始化
// 用于所有TLS服务端连接的证书配置
var (
	cert tls.Certificate
)

// InitTls 初始化TLS证书
// 该函数会自动生成一个自签名的RSA证书，用于NPS服务的TLS加密
// 证书有效期为10年，组织名为"NPS Org"
// 如果证书生成失败，程序将直接退出
func InitTls() {
	// 生成RSA密钥对和自签名证书
	c, k, err := generateKeyPair("NPX Org")
	if err == nil {
		// 将PEM格式的证书和私钥转换为tls.Certificate
		cert, err = tls.X509KeyPair(c, k)
	}
	if err != nil {
		log.Fatalln("Error initializing crypto certs", err)
	}
}

// NewTlsServerConn 创建TLS服务端连接
// 将普通的网络连接包装成TLS加密连接，用于服务端
// 使用全局变量cert作为服务端证书
// 参数:
//
//	conn - 原始的网络连接
//
// 返回:
//
//	net.Conn - 包装后的TLS连接
func NewTlsServerConn(conn net.Conn) net.Conn {
	var err error
	if err != nil {
		logs.Error(err)
		os.Exit(0)
		return nil
	}
	// 配置TLS服务端，使用预先生成的证书
	config := &tls.Config{Certificates: []tls.Certificate{cert}}
	return tls.Server(conn, config)
}

// NewTlsClientConn 创建TLS客户端连接
// 将普通的网络连接包装成TLS加密连接，用于客户端
// 注意：此函数跳过了证书验证（InsecureSkipVerify: true）
// 这在内网穿透场景中是常见的，因为通常使用自签名证书
// 参数:
//
//	conn - 原始的网络连接
//
// 返回:
//
//	net.Conn - 包装后的TLS连接
func NewTlsClientConn(conn net.Conn) net.Conn {
	// 配置TLS客户端，跳过证书验证
	conf := &tls.Config{
		InsecureSkipVerify: true, // 跳过证书验证，适用于自签名证书
	}
	return tls.Client(conn, conf)
}

// generateKeyPair 生成RSA密钥对和自签名证书
// 该函数创建一个2048位的RSA私钥和对应的自签名X.509证书
// 证书有效期为10年，适用于TLS服务端认证
// 参数:
//
//	CommonName - 证书的通用名称，通常是域名或服务名
//
// 返回:
//
//	rawCert - PEM格式的证书数据
//	rawKey - PEM格式的私钥数据
//	err - 错误信息
//
// 注意：此函数基于Go标准库的generate_cert.go示例改编
// 参考：https://golang.org/src/crypto/tls/generate_cert.go
func generateKeyPair(CommonName string) (rawCert, rawKey []byte, err error) {
	// Create private key and self-signed certificate
	// Adapted from https://golang.org/src/crypto/tls/generate_cert.go

	// 生成2048位RSA私钥
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return
	}

	// 设置证书有效期为10年
	validFor := time.Hour * 24 * 365 * 10 // ten years
	notBefore := time.Now()               // 证书生效时间
	notAfter := notBefore.Add(validFor)   // 证书过期时间

	// 生成证书序列号（128位随机数）
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)

	// 创建证书模板
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"My Company Name LTD."}, // 组织名称
			CommonName:   CommonName,                       // 通用名称（通常是域名）
			Country:      []string{"US"},                   // 国家代码
		},
		NotBefore: notBefore, // 证书生效时间
		NotAfter:  notAfter,  // 证书过期时间

		// 密钥用途：密钥加密和数字签名
		KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		// 扩展密钥用途：服务端认证
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, // 启用基本约束
	}

	// 创建自签名证书（使用相同的模板作为颁发者和主体）
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return
	}

	// 将证书编码为PEM格式
	rawCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	// 将私钥编码为PEM格式
	rawKey = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return
}
