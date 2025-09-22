// Package connection 提供NPS服务器的连接管理功能
// 负责管理各种类型的网络监听器，包括桥接连接、HTTP代理、HTTPS代理和Web管理界面
package connection

import (
	"net"
	"os"
	"strconv"

	"ehang.io/nps/lib/pmux"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

// 全局变量定义
var (
	// pMux 端口复用器，用于在同一端口上处理多种协议
	//这行代码声明了一个名为 pMux 的变量，它的类型是指向 pmux.PortMux 结构体的指针。
	pMux *pmux.PortMux

	// bridgePort 桥接端口，客户端连接到服务器的主要端口
	bridgePort string

	// httpsPort HTTPS代理端口
	httpsPort string

	// httpPort HTTP代理端口
	httpPort string

	// webPort Web管理界面端口
	webPort string
)

// InitConnectionService 初始化连接服务
// 从配置文件中读取各种端口配置，并根据端口复用情况初始化端口复用器
// 如果HTTP、HTTPS或Web管理端口与桥接端口相同，则启用端口复用功能
func InitConnectionService() {
	// 从配置文件读取各种端口配置
	bridgePort = beego.AppConfig.String("bridge_port")
	httpsPort = beego.AppConfig.String("https_proxy_port")
	httpPort = beego.AppConfig.String("http_proxy_port")
	webPort = beego.AppConfig.String("web_port")

	// 检查是否需要启用端口复用
	// 当HTTP、HTTPS或Web管理端口与桥接端口相同时，使用端口复用器
	if httpPort == bridgePort || httpsPort == bridgePort || webPort == bridgePort {
		port, err := strconv.Atoi(bridgePort)
		if err != nil {
			logs.Error(err)
			os.Exit(0)
		}
		// 创建端口复用器实例
		pMux = pmux.NewPortMux(port, beego.AppConfig.String("web_host"))
	}
}

/*
其他函数返回类型方式
// 单个返回值
func getName() string

// 多个返回值
func getNameAndAge() (string, int)

// 命名返回值
func divide(a, b int) (result int, err error)

// 无返回值
func printMessage()*
*/

// GetBridgeListener 获取桥接监听器
// 用于客户端连接到服务器的主要通道
// 参数:
//
//	tp - 桥接类型（如tcp、kcp等）
//
// 返回:
//
//	net.Listener - 网络监听器
//	error - 错误信息
func GetBridgeListener(tp string) (net.Listener, error) {
	logs.Info("server start, the bridge type is %s, the bridge port is %s", tp, bridgePort)
	var p int
	var err error

	// 将端口字符串转换为整数
	if p, err = strconv.Atoi(bridgePort); err != nil {
		return nil, err
	}

	// 如果启用了端口复用，使用复用器的客户端监听器
	if pMux != nil {
		return pMux.GetClientListener(), nil
	}

	// 否则创建独立的TCP监听器
	return net.ListenTCP("tcp", &net.TCPAddr{net.ParseIP(beego.AppConfig.String("bridge_ip")), p, ""})
}

// GetHttpListener 获取HTTP代理监听器
// 用于处理HTTP代理请求
// 返回:
//
//	net.Listener - 网络监听器
//	error - 错误信息
func GetHttpListener() (net.Listener, error) {
	// 如果启用了端口复用且HTTP端口与桥接端口相同，使用复用器
	if pMux != nil && httpPort == bridgePort {
		logs.Info("start http listener, port is", bridgePort)
		return pMux.GetHttpListener(), nil
	}

	// 否则在独立端口上创建HTTP监听器
	logs.Info("start http listener, port is", httpPort)
	return getTcpListener(beego.AppConfig.String("http_proxy_ip"), httpPort)
}

// GetHttpsListener 获取HTTPS代理监听器
// 用于处理HTTPS代理请求
// 返回:
//
//	net.Listener - 网络监听器
//	error - 错误信息
func GetHttpsListener() (net.Listener, error) {
	// 如果启用了端口复用且HTTPS端口与桥接端口相同，使用复用器
	if pMux != nil && httpsPort == bridgePort {
		logs.Info("start https listener, port is", bridgePort)
		return pMux.GetHttpsListener(), nil
	}

	// 否则在独立端口上创建HTTPS监听器
	logs.Info("start https listener, port is", httpsPort)
	return getTcpListener(beego.AppConfig.String("http_proxy_ip"), httpsPort)
}

// GetWebManagerListener 获取Web管理界面监听器
// 用于提供Web管理界面服务
// 返回:
//
//	net.Listener - 网络监听器
//	error - 错误信息
func GetWebManagerListener() (net.Listener, error) {
	// 如果启用了端口复用且Web端口与桥接端口相同，使用复用器
	if pMux != nil && webPort == bridgePort {
		logs.Info("Web management start, access port is", bridgePort)
		return pMux.GetManagerListener(), nil
	}

	// 否则在独立端口上创建Web管理监听器
	logs.Info("web management start, access port is", webPort)
	return getTcpListener(beego.AppConfig.String("web_ip"), webPort)
}

// getTcpListener 创建TCP监听器的辅助函数
// 根据指定的IP地址和端口创建TCP监听器
// 参数:
//
//	ip - 监听的IP地址，如果为空则默认为"0.0.0.0"
//	p - 监听的端口号（字符串格式）
//
// 返回:
//
//	net.Listener - TCP监听器
//	error - 错误信息
func getTcpListener(ip, p string) (net.Listener, error) {
	// 将端口字符串转换为整数
	port, err := strconv.Atoi(p)
	if err != nil {
		logs.Error(err)
		os.Exit(0)
	}

	// 如果IP地址为空，默认监听所有网络接口
	if ip == "" {
		ip = "0.0.0.0"
	}

	// 创建并返回TCP监听器
	return net.ListenTCP("tcp", &net.TCPAddr{net.ParseIP(ip), port, ""})
}
