// Package controllers 包含了NPS Web管理界面的控制器
// 本文件实现了认证相关的控制器功能
package controllers

import (
	"encoding/hex"
	"time"

	"ehang.io/nps/lib/crypt"
	"github.com/astaxie/beego"
)

// AuthController 认证控制器
// 负责处理客户端认证相关的请求，包括获取加密认证密钥和服务器时间
type AuthController struct {
	beego.Controller
}

// GetAuthKey 获取加密的认证密钥
// 该方法用于向客户端提供经过AES加密的认证密钥
// 客户端可以使用此密钥进行后续的身份验证
// 返回JSON格式：
// - status: 状态码，1表示成功，0表示失败
// - crypt_auth_key: 十六进制编码的加密认证密钥（成功时）
// - crypt_type: 加密类型，固定为"aes cbc"（成功时）
func (s *AuthController) GetAuthKey() {
	m := make(map[string]interface{})
	// 使用defer确保无论函数如何退出都会返回JSON响应
	defer func() {
		s.Data["json"] = m
		s.ServeJSON()
	}()
	
	// 从配置文件中获取加密密钥，必须是16字节长度（AES-128要求）
	if cryptKey := beego.AppConfig.String("auth_crypt_key"); len(cryptKey) != 16 {
		// 加密密钥长度不正确，返回失败状态
		m["status"] = 0
		return
	} else {
		// 使用AES算法加密认证密钥
		b, err := crypt.AesEncrypt([]byte(beego.AppConfig.String("auth_key")), []byte(cryptKey))
		if err != nil {
			// 加密失败，返回失败状态
			m["status"] = 0
			return
		}
		// 加密成功，返回加密后的密钥和相关信息
		m["status"] = 1
		m["crypt_auth_key"] = hex.EncodeToString(b) // 将加密结果转换为十六进制字符串
		m["crypt_type"] = "aes cbc"                 // 标识加密算法类型
		return
	}
}

// GetTime 获取服务器当前时间
// 该方法用于向客户端提供服务器的当前Unix时间戳
// 客户端可以使用此时间进行时间同步或验证时效性
// 返回JSON格式：
// - time: Unix时间戳（秒）
func (s *AuthController) GetTime() {
	m := make(map[string]interface{})
	m["time"] = time.Now().Unix() // 获取当前Unix时间戳
	s.Data["json"] = m
	s.ServeJSON()
}
