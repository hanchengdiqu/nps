package main

// 本文件是 npc（nps 客户端）的入口程序。它负责：
// 1) 解析命令行参数与环境变量，初始化日志与性能分析；
// 2) 将进程作为系统服务（Windows Service、systemd、SysV）运行并处理安装/启动/停止等子命令；
// 3) 按两种模式启动客户端：
//    - 直连模式：通过 -server 与 -vkey 直接连接到 nps 服务器；
//    - 配置文件模式：通过 -config 指定配置文件启动；
// 4) 处理附加功能：注册本机 IP、查询任务状态、检测 NAT 类型、在线升级等。

import (
	// 业务模块
	"ehang.io/nps/client"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/config"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/install"
	"ehang.io/nps/lib/version"

	// 标准库与第三方库
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/astaxie/beego/logs"
	"github.com/ccding/go-stun/stun"
	"github.com/kardianos/service"
)

// 命令行参数定义
// 说明：除英文原有描述外，补充中文注释以便理解。
var (
	// 服务器地址，格式 ip:port，例如 1.2.3.4:8024
	serverAddr = flag.String("server", "", "Server addr (ip:port)")
	// 客户端配置文件路径（与 -server/-vkey 二选一）。
	configPath = flag.String("config", "", "Configuration file path")
	// 与服务端匹配的验证密钥。
	verifyKey = flag.String("vkey", "", "Authentication key")
	// 日志输出方式（stdout|file），注意真正生效由 -debug 决定。
	logType = flag.String("log", "stdout", "Log output mode（stdout|file）")
	// 与服务端的连接类型（kcp|tcp）。
	connType = flag.String("type", "tcp", "Connection type with the server（kcp|tcp）")
	// 连接服务端时使用的 socks5 代理，例如 socks5://user:pass@127.0.0.1:1080
	proxyUrl = flag.String("proxy", "", "proxy socks5 url(eg:socks5://111:222@127.0.0.1:9007)")
	// 日志级别 0~7（数值越大日志越多）。
	logLevel = flag.String("log_level", "7", "log level 0~7")
	// 向服务端注册本机 IP 的持续时间（小时），用于临时映射。
	registerTime = flag.Int("time", 2, "register time long /h")
	// P2P 场景下本地监听端口。
	localPort = flag.Int("local_port", 2000, "p2p local port")
	// P2P 密码（设置后进入本地直连/密钥模式）。
	password = flag.String("password", "", "p2p password flag")
	// P2P 目标地址，例如 127.0.0.1:22。
	target = flag.String("target", "", "p2p target")
	// 本地服务类型（默认 p2p），与 -password、-target 配合使用。
	localType = flag.String("local_type", "p2p", "p2p target")
	// 当 -debug=false 时的日志文件路径（默认放置在系统推荐位置）。
	logPath = flag.String("log_path", "", "npc log path")
	// 是否开启调试（true 则输出到控制台；false 则写入文件）。
	debug = flag.Bool("debug", true, "npc debug")
	// pprof 地址，形如 ip:port，设置后可通过 http 采集性能数据。
	pprofAddr = flag.String("pprof", "", "PProf debug addr (ip:port)")
	// STUN 服务器地址，用于 NAT 类型侦测。
	stunAddr = flag.String("stun_addr", "stun.stunprotocol.org:3478", "stun server address (eg:stun.stunprotocol.org:3478)")
	// 输出当前版本信息后退出。
	ver = flag.Bool("version", false, "show current version")
	// 心跳检查超时：未收到检查包的次数达到该阈值后断开客户端。
	disconnectTime = flag.Int("disconnect_timeout", 60, "not receiving check packet times, until timeout will disconnect the client")
)

func main() {
	// 解析命令行与初始化日志系统
	flag.Parse()
	logs.Reset()
	logs.EnableFuncCallDepth(true)
	logs.SetLogFuncCallDepth(3)

	// 如果仅查看版本，直接输出版本信息并退出
	if *ver {
		common.PrintVersion()
		return
	}

	// 日志路径默认值
	if *logPath == "" {
		*logPath = common.GetNpcLogPath()
	}
	// Windows 下反斜杠需要转义，避免写入 beego/logs JSON 配置时被误解析
	if common.IsWindows() {
		*logPath = strings.Replace(*logPath, "\\", "\\\\", -1)
	}
	// 根据 debug 开关决定日志输出到控制台还是文件
	if *debug {
		logs.SetLogger(logs.AdapterConsole, `{"level":`+*logLevel+`,"color":true}`)
	} else {
		logs.SetLogger(logs.AdapterFile, `{"level":`+*logLevel+`,"filename":"`+*logPath+`","daily":false,"maxlines":100000,"color":true}`)
	}

	// 初始化系统服务配置（支持 Windows Service、systemd、SysV）
	options := make(service.KeyValue)
	svcConfig := &service.Config{
		Name:        "Npc",        // 服务名
		DisplayName: "nps内网穿透客户端", // 显示名
		Description: "一款轻量级、功能强大的内网穿透代理服务器。支持tcp、udp流量转发，支持内网http代理、内网socks5代理，同时支持snappy压缩、站点保护、加密传输、多路复用、header修改等。支持web图形化管理，集成多用户模式。",
		Option:      options,
	}
	if !common.IsWindows() {
		// Linux 下声明依赖，确保网络就绪后再启动
		svcConfig.Dependencies = []string{
			"Requires=network.target",
			"After=network-online.target syslog.target"}
		// 注入 systemd / SysV 启动脚本模板
		svcConfig.Option["SystemdScript"] = install.SystemdScript
		svcConfig.Option["SysvScript"] = install.SysvScript
	}
	// 将除服务控制动词外的其他参数透传给服务进程
	for _, v := range os.Args[1:] {
		switch v {
		case "install", "start", "stop", "uninstall", "restart":
			continue
		}
		// 过滤由 service 库自动注入的参数
		if !strings.Contains(v, "-service=") && !strings.Contains(v, "-debug=") {
			svcConfig.Arguments = append(svcConfig.Arguments, v)
		}
	}
	// 以服务方式运行时默认关闭 debug（写入文件）
	svcConfig.Arguments = append(svcConfig.Arguments, "-debug=false")

	// 实例化服务
	prg := &npc{
		exit: make(chan struct{}),
	}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		// 当 service 框架不可用时，退化为前台运行并阻塞
		logs.Error(err, "service function disabled")
		run()
		// 保持进程不退出
		wg := sync.WaitGroup{}
		wg.Add(1)
		wg.Wait()
		return
	}

	// 处理命令行子命令（非服务模式下执行一次性动作）
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "status":
			// 查询任务状态：npc status -config=/path/to/conf
			if len(os.Args) > 2 {
				path := strings.Replace(os.Args[2], "-config=", "", -1)
				client.GetTaskStatus(path)
			}
		case "register":
			// 将本机地址注册到服务端，便于临时穿透映射
			flag.CommandLine.Parse(os.Args[2:])
			client.RegisterLocalIp(*serverAddr, *verifyKey, *connType, *proxyUrl, *registerTime)
		case "update":
			// 在线更新 npc 可执行文件
			install.UpdateNpc()
			return
		case "nat":
			// 通过 STUN 探测 NAT 类型与公网地址
			c := stun.NewClient()
			c.SetServerAddr(*stunAddr)
			nat, host, err := c.Discover()
			if err != nil || host == nil {
				logs.Error("get nat type error", err)
				return
			}
			fmt.Printf("nat type: %s \npublic address: %s\n", nat.String(), host.String())
			os.Exit(0)
		case "start", "stop", "restart":
			// 在 OpenWrt 等环境下，使用 SysV 脚本桥接
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				cmd := exec.Command("/etc/init.d/"+svcConfig.Name, os.Args[1])
				err := cmd.Run()
				if err != nil {
					logs.Error(err)
				}
				return
			}
			// 其他平台通过 service 库控制
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			return
		case "install":
			// 重新安装服务：先停止并卸载旧服务，再安装并启动
			service.Control(s, "stop")
			service.Control(s, "uninstall")
			install.InstallNpc()
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			if service.Platform() == "unix-systemv" {
				// SysV 平台上创建启动/关闭软链接，适配启动级别
				logs.Info("unix-systemv service")
				confPath := "/etc/init.d/" + svcConfig.Name
				os.Symlink(confPath, "/etc/rc.d/S90"+svcConfig.Name)
				os.Symlink(confPath, "/etc/rc.d/K02"+svcConfig.Name)
			}
			return
		case "uninstall":
			// 卸载服务并清理由 SysV 产生的软链接
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				os.Remove("/etc/rc.d/S90" + svcConfig.Name)
				os.Remove("/etc/rc.d/K02" + svcConfig.Name)
			}
			return
		}
	}

	// 未指定子命令时，以服务方式运行
	s.Run()
}

// npc 封装了作为系统服务运行时需要的生命周期控制。
// Start 在服务启动时被调用，Stop 在服务停止时被调用。
type npc struct {
	exit chan struct{} // 用于通知后台循环优雅退出
}

// Start 启动后台逻辑。
func (p *npc) Start(s service.Service) error {
	go p.run()
	return nil
}

// Stop 通知后台退出；如果在交互模式（前台运行），直接结束进程。
func (p *npc) Stop(s service.Service) error {
	close(p.exit)
	if service.Interactive() {
		os.Exit(0)
	}
	return nil
}

// run 包装真实的 run()，并增加 panic 保护与退出监听。
func (p *npc) run() error {
	defer func() {
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			logs.Warning("npc: panic serving %v: %v\n%s", err, string(buf))
		}
	}()

	// 启动主逻辑
	run()
	// 等待退出信号
	select {
	case <-p.exit:
		logs.Warning("stop...")
	}
	return nil
}

// run 启动 npc 的核心流程。
// 启动顺序：
// 1) 开启 pprof（如指定）；
// 2) 若指定了 -password，则进入「本地直连/密钥模式」，直接启动一个本地 server；
// 3) 否则：
//   - 若提供 -server 与 -vkey（且未指定 -config），则以直连模式连接服务端；
//   - 否则从配置文件启动（默认从系统路径读取）。
func run() {
	// 启动 pprof（若设置了 -pprof）
	common.InitPProfFromArg(*pprofAddr)

	// 密钥直连模式（通常用于临时启动一个本地端口转发/打洞服务）
	if *password != "" {
		commonConfig := new(config.CommonConfig)
		commonConfig.Server = *serverAddr
		commonConfig.VKey = *verifyKey
		commonConfig.Tp = *connType

		// 构建一个本地服务配置，由客户端直接启动
		localServer := new(config.LocalServer)
		localServer.Type = *localType
		localServer.Password = *password
		localServer.Target = *target
		localServer.Port = *localPort

		// 由于 StartLocalServer 期望一个完整的客户端配置结构体，这里最小化填充必要字段
		commonConfig.Client = new(file.Client)
		commonConfig.Client.Cnf = new(file.Config)

		go client.StartLocalServer(localServer, commonConfig)
		return
	}

	// 常规模式下，优先从环境变量读取 server/vkey 作为缺省值
	env := common.GetEnvMap()
	if *serverAddr == "" {
		*serverAddr, _ = env["NPC_SERVER_ADDR"]
	}
	if *verifyKey == "" {
		*verifyKey, _ = env["NPC_SERVER_VKEY"]
	}

	// 打印版本信息，便于排查问题
	logs.Info("the version of client is %s, the core version of client is %s", version.VERSION, version.GetVersion())

	// 直连模式：给定 server 和 vkey 且未指定 config
	if *verifyKey != "" && *serverAddr != "" && *configPath == "" {
		go func() {
			for {
				// NewRPClient 返回一个可重用的客户端实例；Start 阻塞直到连接结束
				client.NewRPClient(*serverAddr, *verifyKey, *connType, *proxyUrl, nil, *disconnectTime).Start()
				// 连接结束后等待 5 秒重连，避免频繁重试
				logs.Info("Client closed! It will be reconnected in five seconds")
				time.Sleep(time.Second * 5)
			}
		}()
	} else {
		// 配置文件模式：若未指定路径，则使用默认路径
		if *configPath == "" {
			*configPath = common.GetConfigPath()
		}
		go client.StartFromFile(*configPath)
	}
}
