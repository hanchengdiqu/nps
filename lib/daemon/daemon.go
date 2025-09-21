// Package daemon 提供跨平台的简单守护/后台运行管理能力。
//
// 该包不依赖系统服务管理器（如 systemd、launchd、Windows Service），
// 而是通过以下方式实现最基础的进程管理：
// 1) 使用 PID 文件记录已启动进程的 PID（pidPath/f.pid）。
// 2) 通过命令行子命令（start/stop/restart/status/reload）控制目标进程。
// 3) 针对 Unix 与 Windows 采用不同的系统命令来查询或终止进程：
//   - Unix: 使用 ps/awk/grep 查询进程，kill 发送信号（-30 为自定义重载，-9 为强杀）。
//   - Windows: 使用 tasklist 查询、taskkill 终止进程。
//
// 注意：本包只做最小化的进程控制与 PID 文件维护，不负责守护子进程崩溃后的重启等高级能力。
package daemon

import (
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"ehang.io/nps/lib/common"
)

// InitDaemon 作为统一入口解析命令行并派发子命令。
// 参数：
// - f: 进程名（用于生成 pid 文件名 f.pid，亦用于日志提示）。
// - runPath: 运行/配置目录，仅用于日志显示，便于定位当前使用的配置。
// - pidPath: PID 文件目录。
// 命令行约定：
// - os.Args[1] 必须是子命令：start|stop|restart|status|reload。
// - 之后的其余参数（从 os.Args[2] 开始）会原样传给被启动的子进程；同时会自动追加 -log=file。
// 行为：
// - start: 后台拉起一个新的当前二进制进程，并将其 PID 写入 pidPath/f.pid；本父进程退出（Exit 0）。
// - stop: 读取 pid 文件并终止对应进程（Windows 用 taskkill，Unix 用 kill -9）；本父进程退出（Exit 0）。
// - restart: 依次 stop 再 start；本父进程退出（Exit 0）。
// - status: 检查 pid 文件并在系统进程列表中查找 PID，打印运行状态并退出（Exit 0）。
// - reload: 在 Unix 上向进程发送信号 30（SIGUSR1/USR1，依不同平台而定），用于热加载配置；Windows 下仅做 pid 文件存在性检查。
// 注意：如果命令行参数不足（len(os.Args)<2），函数直接返回，不做任何操作。
func InitDaemon(f string, runPath string, pidPath string) {
	if len(os.Args) < 2 {
		return
	}
	var args []string
	args = append(args, os.Args[0])
	if len(os.Args) >= 2 {
		args = append(args, os.Args[2:]...)
	}
	args = append(args, "-log=file")
	switch os.Args[1] {
	case "start":
		start(args, f, pidPath, runPath)
		os.Exit(0)
	case "stop":
		stop(f, args[0], pidPath)
		os.Exit(0)
	case "restart":
		stop(f, args[0], pidPath)
		start(args, f, pidPath, runPath)
		os.Exit(0)
	case "status":
		if status(f, pidPath) {
			log.Printf("%s is running", f)
		} else {
			log.Printf("%s is not running", f)
		}
		os.Exit(0)
	case "reload":
		reload(f, pidPath)
		os.Exit(0)
	}
}

// reload 在 Unix 系统上向已运行的进程发送用户自定义信号以触发热加载。
// 参数：
// - f: 进程名（用于定位 pid 文件 pidPath/f.pid）。
// - pidPath: PID 文件目录。
// 行为：
// - 当 f=="nps" 且非 Windows 且进程不在运行时，直接打印失败并返回，避免误发信号。
// - 读取 pid 文件并执行 `kill -30 <pid>`；其中 30 通常映射为 USR1（具体编号因平台不同可能有差异，本项目固定用 30）。
// - 如果 pid 文件不存在，函数会 Fatal 退出。
// - 执行成功打印 "reload success"，否则打印 "reload fail"。
func reload(f string, pidPath string) {
	if f == "npx" && !common.IsWindows() && !status(f, pidPath) {
		log.Println("reload fail")
		return
	}
	var c *exec.Cmd
	var err error
	b, err := ioutil.ReadFile(filepath.Join(pidPath, f+".pid"))
	if err == nil {
		c = exec.Command("/bin/bash", "-c", `kill -30 `+string(b))
	} else {
		log.Fatalln("reload error,pid file does not exist")
	}
	if c.Run() == nil {
		log.Println("reload success")
	} else {
		log.Println("reload fail")
	}
}

// status 根据 pid 文件和系统进程列表判断目标进程是否在运行。
// 返回：true 表示系统中存在与 pid 文件一致的 PID；否则为 false。
// 实现：
// - 读取 pidPath/f.pid，若失败直接返回 false；
// - Unix: 通过 ps -ax | awk '{ print $1 }' | grep <pid> 来判断；
// - Windows: 通过 tasklist 输出文本中是否包含该 PID 来判断；
// 注意：PID 文件可能已过期（进程已退出或 PID 被复用），此方法仅做基础判断，不保证与具体二进制一一对应。
func status(f string, pidPath string) bool {
	var cmd *exec.Cmd
	b, err := ioutil.ReadFile(filepath.Join(pidPath, f+".pid"))
	if err == nil {
		if !common.IsWindows() {
			cmd = exec.Command("/bin/sh", "-c", "ps -ax | awk '{ print $1 }' | grep "+string(b))
		} else {
			cmd = exec.Command("tasklist")
		}
		out, _ := cmd.Output()
		if strings.Index(string(out), string(b)) > -1 {
			return true
		}
	}
	return false
}

// start 启动一个新的当前二进制进程（子进程），并将其 PID 写入 pid 文件。
// 参数：
// - osArgs: 传递给子进程的参数（已由 InitDaemon 填充，包括 -log=file）。
// - f: 进程名，用于日志与 pid 文件名。
// - pidPath: PID 文件目录。
// - runPath: 运行/配置路径，仅用于日志输出。
// 行为：
//   - 若已在运行（status 为 true）则直接返回，避免重复启动；
//   - 使用 exec.Command(osArgs[0], osArgs[1:]...) 启动子进程（Start 而非 Run，不阻塞等待），
//     成功后将 PID 写入 pidPath/f.pid（0600 权限）；
//   - 该函数不负责守护与自动重启，仅做一次性启动与 PID 写入。
func start(osArgs []string, f string, pidPath, runPath string) {
	if status(f, pidPath) {
		log.Printf(" %s is running", f)
		return
	}
	cmd := exec.Command(osArgs[0], osArgs[1:]...)
	cmd.Start()
	if cmd.Process.Pid > 0 {
		log.Println("start ok , pid:", cmd.Process.Pid, "config path:", runPath)
		d1 := []byte(strconv.Itoa(cmd.Process.Pid))
		ioutil.WriteFile(filepath.Join(pidPath, f+".pid"), d1, 0600)
	} else {
		log.Println("start error")
	}
}

// stop 根据 pid 文件终止正在运行的目标进程。
// 参数：
// - f: 进程名（用于定位 pid 文件 pidPath/f.pid）。
// - p: 当前可执行文件路径，仅在 Windows 下使用，用于提取镜像名并通过 taskkill 结束进程。
// - pidPath: PID 文件目录。
// 行为：
// - 若进程不在运行（status 为 false），直接返回；
// - Windows: 执行 taskkill /F /IM <exeName> 强制按进程镜像名终止（可能影响同名的其它进程，需谨慎）；
// - Unix: 读取 pid 文件并执行 kill -9 <pid> 强制终止；若 pid 文件不存在则 Fatal 退出；
// - 执行后输出 stop ok 或 stop error 及错误详情。
func stop(f string, p string, pidPath string) {
	if !status(f, pidPath) {
		log.Printf(" %s is not running", f)
		return
	}
	var c *exec.Cmd
	var err error
	if common.IsWindows() {
		p := strings.Split(p, `\`)
		c = exec.Command("taskkill", "/F", "/IM", p[len(p)-1])
	} else {
		b, err := ioutil.ReadFile(filepath.Join(pidPath, f+".pid"))
		if err == nil {
			c = exec.Command("/bin/bash", "-c", `kill -9 `+string(b))
		} else {
			log.Fatalln("stop error,pid file does not exist")
		}
	}
	err = c.Run()
	if err != nil {
		log.Println("stop error,", err)
	} else {
		log.Println("stop ok")
	}
}
