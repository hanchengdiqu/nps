package pmux

import (
	"testing"
	"time"

	"github.com/astaxie/beego/logs"
)

// TestPortMux_Close 测试端口复用器的关闭功能
// 这个测试函数验证PortMux的基本功能，包括启动、获取监听器和接受连接
func TestPortMux_Close(t *testing.T) {
	// 重置日志配置
	logs.Reset()
	// 启用函数调用深度显示，便于调试
	logs.EnableFuncCallDepth(true)
	// 设置日志函数调用深度为3
	logs.SetLogFuncCallDepth(3)

	// 创建一个新的端口复用器，监听8888端口，管理器主机为"Ds"
	pMux := NewPortMux(8888, "Ds")
	
	// 启动一个协程来启动端口复用器
	go func() {
		if pMux.Start() != nil {
			logs.Warn("Error")
		}
	}()
	
	// 等待3秒让端口复用器完全启动
	time.Sleep(time.Second * 3)
	
	// 启动第一个协程来测试HTTP监听器
	// 获取HTTP监听器并尝试接受连接
	go func() {
		l := pMux.GetHttpListener()
		conn, err := l.Accept()
		logs.Warn(conn, err)
	}()
	
	// 启动第二个协程来测试HTTP监听器
	// 再次获取HTTP监听器并尝试接受连接
	go func() {
		l := pMux.GetHttpListener()
		conn, err := l.Accept()
		logs.Warn(conn, err)
	}()
	
	// 启动第三个协程来测试HTTP监听器
	// 第三次获取HTTP监听器并尝试接受连接
	go func() {
		l := pMux.GetHttpListener()
		conn, err := l.Accept()
		logs.Warn(conn, err)
	}()
	
	// 在主协程中也获取HTTP监听器并尝试接受连接
	// 这个调用会阻塞，直到有连接到达或发生错误
	l := pMux.GetHttpListener()
	conn, err := l.Accept()
	logs.Warn(conn, err)
}
