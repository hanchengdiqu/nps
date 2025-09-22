// +build !windows

// reload.go 文件实现了 NPS 服务在非 Windows 系统上的配置热重载功能。
// 该文件通过监听 SIGUSR1 信号来实现配置文件的动态重新加载，无需重启服务。
//
// 工作原理：
// 1. 在程序启动时（init函数），创建一个信号监听器
// 2. 监听 SIGUSR1 信号（在 Unix/Linux 系统中通常用于用户自定义操作）
// 3. 当接收到该信号时，重新加载 nps.conf 配置文件
// 4. 这样可以在不停止服务的情况下更新配置
//
// 使用场景：
// - 管理员修改了 nps.conf 配置文件后，可以通过发送 SIGUSR1 信号来热重载配置
// - 配合 daemon.go 中的 reload 命令使用（kill -30 <pid> 或 kill -USR1 <pid>）
// - 避免了重启服务带来的连接中断和服务不可用时间

package daemon

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"ehang.io/nps/lib/common"
	"github.com/astaxie/beego"
)

// init 函数在包被导入时自动执行，设置信号监听器用于配置热重载
func init() {
	// 创建一个缓冲大小为1的信号通道，用于接收操作系统信号
	s := make(chan os.Signal, 1)
	
	// 注册信号监听器，监听 SIGUSR1 信号
	// SIGUSR1 是 Unix/Linux 系统中的用户自定义信号1，通常用于触发应用程序的特定操作
	signal.Notify(s, syscall.SIGUSR1)
	
	// 启动一个独立的 goroutine 来处理信号
	go func() {
		for {
			// 阻塞等待接收信号
			<-s
			
			// 当接收到 SIGUSR1 信号时，重新加载配置文件
			// 使用 beego 框架的 LoadAppConfig 方法重新加载 INI 格式的配置文件
			// 配置文件路径：<运行目录>/conf/nps.conf
			beego.LoadAppConfig("ini", filepath.Join(common.GetRunPath(), "conf", "nps.conf"))
		}
	}()
}
