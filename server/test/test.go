// Package test 提供NPS服务器配置验证功能
// 主要职责：
// - 验证服务器配置中所有端口的可用性
// - 检查端口冲突问题
// - 验证SSL证书文件的存在性
// - 确保服务器启动前配置的正确性
package test

import (
	"log"
	"path/filepath"
	"strconv"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego"
)

// TestServerConfig 测试服务器配置的有效性
// 该函数会检查以下配置项：
// 1. 数据库中的所有任务端口（TCP/UDP）
// 2. Web管理端口
// 3. 桥接通信端口（TCP/UDP，根据bridge_type决定）
// 4. HTTP代理端口
// 5. HTTPS代理端口及SSL证书文件
// 
// 检查内容：
// - 端口是否被重复使用
// - 端口是否可用（通过实际绑定测试）
// - SSL证书文件是否存在
//
// 如果发现任何问题，程序会立即退出并输出错误信息
func TestServerConfig() {
	// 用于存储已检查的TCP端口列表，避免端口冲突
	var postTcpArr []int
	// 用于存储已检查的UDP端口列表，避免端口冲突
	var postUdpArr []int
	
	// 遍历数据库中的所有任务配置，检查任务端口
	file.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		v := value.(*file.Tunnel)
		if v.Mode == "udp" {
			// UDP模式任务，检查UDP端口
			isInArr(&postUdpArr, v.Port, v.Remark, "udp")
		} else if v.Port != 0 {
			// TCP模式任务，检查TCP端口（端口为0表示随机端口，跳过检查）
			isInArr(&postTcpArr, v.Port, v.Remark, "tcp")
		}
		return true
	})
	
	// 检查Web管理端口配置
	p, err := beego.AppConfig.Int("web_port")
	if err != nil {
		log.Fatalln("Getting web management port error :", err)
	} else {
		isInArr(&postTcpArr, p, "Web Management port", "tcp")
	}

	// 检查桥接通信端口配置
	if p := beego.AppConfig.String("bridge_port"); p != "" {
		if port, err := strconv.Atoi(p); err != nil {
			log.Fatalln("get Server and client communication portserror:", err)
		} else if beego.AppConfig.String("bridge_type") == "kcp" {
			// KCP协议使用UDP
			isInArr(&postUdpArr, port, "Server and client communication ports", "udp")
		} else {
			// 默认使用TCP
			isInArr(&postTcpArr, port, "Server and client communication ports", "tcp")
		}
	}

	// 检查HTTP代理端口配置
	if p := beego.AppConfig.String("httpProxyPort"); p != "" {
		if port, err := strconv.Atoi(p); err != nil {
			log.Fatalln("get http port error:", err)
		} else {
			isInArr(&postTcpArr, port, "https port", "tcp")
		}
	}
	
	// 检查HTTPS代理端口配置
	if p := beego.AppConfig.String("https_proxy_port"); p != "" {
		// 检查是否配置为仅代理模式（不启动HTTPS服务）
		if b, err := beego.AppConfig.Bool("https_just_proxy"); !(err == nil && b) {
			if port, err := strconv.Atoi(p); err != nil {
				log.Fatalln("get https port error", err)
			} else {
				// 验证SSL证书文件是否存在
				if beego.AppConfig.String("pemPath") != "" && !common.FileExists(filepath.Join(common.GetRunPath(), beego.AppConfig.String("pemPath"))) {
					log.Fatalf("ssl certFile %s is not exist", beego.AppConfig.String("pemPath"))
				}
				if beego.AppConfig.String("keyPath") != "" && !common.FileExists(filepath.Join(common.GetRunPath(), beego.AppConfig.String("keyPath"))) {
					log.Fatalf("ssl keyFile %s is not exist", beego.AppConfig.String("pemPath"))
				}
				isInArr(&postTcpArr, port, "http port", "tcp")
			}
		}
	}
}

// isInArr 检查端口是否可用并添加到已使用端口列表
// 参数：
//   arr: 已使用端口列表的指针
//   val: 要检查的端口号
//   remark: 端口用途描述（用于错误信息）
//   tp: 端口类型（"tcp" 或 "udp"）
//
// 功能：
// 1. 检查端口是否已被使用
// 2. 测试端口是否可用
// 3. 将端口添加到已使用列表
//
// 如果发现端口冲突或不可用，程序会立即退出
func isInArr(arr *[]int, val int, remark string, tp string) {
	// 检查端口是否已被使用
	for _, v := range *arr {
		if v == val {
			log.Fatalf("the port %d is reused,remark: %s", val, remark)
		}
	}
	
	// 根据端口类型进行可用性测试
	if tp == "tcp" {
		// 测试TCP端口是否可用
		if !common.TestTcpPort(val) {
			log.Fatalf("open the %d port error ,remark: %s", val, remark)
		}
	} else {
		// 测试UDP端口是否可用
		if !common.TestUdpPort(val) {
			log.Fatalf("open the %d port error ,remark: %s", val, remark)
		}
	}
	
	// 将端口添加到已使用列表
	*arr = append(*arr, val)
	return
}
