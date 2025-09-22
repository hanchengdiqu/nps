// Package crypt 提供了NPS项目中使用的加密相关功能
// 
// 本包主要包含以下功能：
// 1. AES加密/解密：使用AES-CBC模式进行数据的对称加密和解密
// 2. PKCS5填充：处理AES加密时的数据块填充和去填充操作
// 3. MD5哈希：生成字符串的MD5摘要，用于数据完整性校验
// 4. 随机字符串生成：生成指定长度的随机字符串，用作验证密钥等
//
// 这些加密功能主要用于NPS内网穿透工具中的数据传输安全保护，
// 确保客户端与服务端之间的通信数据不被第三方窃取或篡改。
package crypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"math/rand"
	"time"
)

// AesEncrypt 使用AES算法对数据进行加密
// 
// 参数：
//   origData: 待加密的原始数据
//   key: 加密密钥，长度必须为16、24或32字节（对应AES-128、AES-192、AES-256）
//
// 返回值：
//   []byte: 加密后的数据
//   error: 如果加密过程中出现错误则返回错误信息
//
// 加密过程：
//   1. 创建AES密码块
//   2. 使用PKCS5填充方式对原始数据进行填充，使其长度为块大小的整数倍
//   3. 使用CBC模式进行加密，初始化向量(IV)使用密钥的前blockSize个字节
//   4. 返回加密后的数据
func AesEncrypt(origData, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()
	origData = PKCS5Padding(origData, blockSize)
	blockMode := cipher.NewCBCEncrypter(block, key[:blockSize])
	crypted := make([]byte, len(origData))
	blockMode.CryptBlocks(crypted, origData)
	return crypted, nil
}

// AesDecrypt 使用AES算法对数据进行解密
//
// 参数：
//   crypted: 待解密的加密数据
//   key: 解密密钥，必须与加密时使用的密钥相同
//
// 返回值：
//   []byte: 解密后的原始数据
//   error: 如果解密过程中出现错误则返回错误信息
//
// 解密过程：
//   1. 创建AES密码块
//   2. 使用CBC模式进行解密，初始化向量(IV)使用密钥的前blockSize个字节
//   3. 对解密后的数据进行PKCS5去填充处理
//   4. 返回原始数据
func AesDecrypt(crypted, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()
	blockMode := cipher.NewCBCDecrypter(block, key[:blockSize])
	origData := make([]byte, len(crypted))
	blockMode.CryptBlocks(origData, crypted)
	err, origData = PKCS5UnPadding(origData)
	return origData, err
}

// PKCS5Padding 对数据进行PKCS5填充
//
// PKCS5填充是一种块密码填充方案，用于将数据长度补齐到块大小的整数倍。
// 填充的字节数等于需要填充的字节数值本身。
//
// 参数：
//   ciphertext: 需要填充的原始数据
//   blockSize: 块大小（通常为16字节）
//
// 返回值：
//   []byte: 填充后的数据
//
// 示例：
//   如果原始数据长度为13字节，块大小为16字节，则需要填充3个字节，
//   每个填充字节的值都是3（即0x03 0x03 0x03）
func PKCS5Padding(ciphertext []byte, blockSize int) []byte {
	padding := blockSize - len(ciphertext)%blockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(ciphertext, padtext...)
}

// PKCS5UnPadding 移除PKCS5填充
//
// 从解密后的数据中移除PKCS5填充，恢复原始数据。
// 通过读取最后一个字节的值来确定填充的字节数，然后移除相应数量的填充字节。
//
// 参数：
//   origData: 包含填充的解密数据
//
// 返回值：
//   error: 如果去填充过程中出现错误（如填充格式不正确）则返回错误信息
//   []byte: 去除填充后的原始数据
//
// 注意：
//   函数会验证填充的有效性，如果填充字节数大于数据长度，则返回错误
func PKCS5UnPadding(origData []byte) (error, []byte) {
	length := len(origData)
	unpadding := int(origData[length-1])
	if (length - unpadding) < 0 {
		return errors.New("len error"), nil
	}
	return nil, origData[:(length - unpadding)]
}

// Md5 生成字符串的MD5哈希值
//
// MD5是一种广泛使用的密码散列函数，可以产生出一个128位（16字节）的散列值，
// 用于确保信息传输完整一致。本函数将MD5哈希值转换为32位的十六进制字符串。
//
// 参数：
//   s: 需要计算MD5哈希的字符串
//
// 返回值：
//   string: 32位十六进制格式的MD5哈希字符串（小写）
//
// 注意：
//   MD5算法已被认为在密码学上不够安全，不建议用于安全敏感的场景，
//   但仍可用于数据完整性校验等非安全场景
func Md5(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// GetRandomString 生成指定长度的随机字符串
//
// 生成由数字和小写字母组成的随机字符串，常用于生成验证密钥、会话ID等。
// 字符集包含：0-9数字和a-z小写字母，共36个字符。
//
// 参数：
//   l: 需要生成的随机字符串长度
//
// 返回值：
//   string: 指定长度的随机字符串
//
// 特点：
//   - 使用当前时间纳秒作为随机种子，确保每次调用都能生成不同的随机字符串
//   - 字符集为数字和小写字母，避免了大小写混淆的问题
//   - 适用于生成临时密钥、验证码、会话标识等场景
func GetRandomString(l int) string {
	str := "0123456789abcdefghijklmnopqrstuvwxyz"
	bytes := []byte(str)
	result := []byte{}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < l; i++ {
		result = append(result, bytes[r.Intn(len(bytes))])
	}
	return string(result)
}
