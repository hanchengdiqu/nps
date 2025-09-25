// nps.go
// 该文件为 nps 服务端可执行程序入口，负责：
// 1) 读取配置并初始化日志/性能分析；
// 2) 配置并注册系统服务（systemd/sysv/Windows Service）；
// 3) 处理命令行子命令（install/start/stop/restart/uninstall/reload/update）；
// 4) 启动 Web 管理端、桥接端口、TLS 等核心服务。
// 注意：本文件仅涉及进程生命周期与服务管理，不包含业务具体实现。
package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/install"
	"ehang.io/nps/lib/version"
	"ehang.io/nps/server"
	"ehang.io/nps/server/connection"
	"ehang.io/nps/server/tool"
	"ehang.io/nps/web/routers"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/daemon"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"

	"github.com/kardianos/service"
)

var (
	level string
	ver   = flag.Bool("version", false, "show current version")
)

// main 为程序入口：
// - 解析命令行参数（如 -version）；
// - 读取 nps.conf 配置并初始化日志；
// - 准备系统服务配置，并根据子命令执行服务控制；
// - 在无服务模式或服务框架内启动核心 run()。
func main() {
	flag.Parse()
	// init log
	if *ver {
		common.PrintVersion()
		return
	}

	// 20250921 修改特性：配置文件查找策略：先查找安装路径，然后查找当前目录，然后都没有，就用内置配置，这样不会报错
	// 读取配置文件 conf/nps.conf；若失败则使用内置默认配置初始化。
	//先查找安装路径，然后查找当前目录，然后都没有，就用内置配置
	confPath := filepath.Join(common.GetRunPath(), "conf", "nps.conf")
	if err := beego.LoadAppConfig("ini", confPath); err != nil {
		logs.Warning("load config file error: %s, using built-in default config (struct)", err.Error())
		// 使用结构体生成内置默认配置，直接构建内存配置，避免文件 IO。
		cfg := DefaultConfig{
			AppName:                 "npx",
			RunMode:                 "dev",
			HttpProxyIp:             "0.0.0.0",
			HttpProxyPort:           65508,
			HttpsProxyPort:          65443,
			HttpsJustProxy:          true,
			HttpsDefaultCertFile:    "conf/server.pem",
			HttpsDefaultKeyFile:     "conf/server.key",
			BridgeType:              "tcp",
			BridgePort:              65203,
			BridgeIp:                "0.0.0.0",
			PublicVKey:              "123@163.com",
			LogLevel:                7,
			LogPath:                 "",
			WebHost:                 "a.o.com",
			WebUsername:             "admin",
			WebPassword:             "1234567890123abc@163.com",
			WebPort:                 65202,
			WebIp:                   "0.0.0.0",
			WebBaseURL:              "",
			WebOpenSSL:              false,
			WebCertFile:             "conf/server.pem",
			WebKeyFile:              "conf/server.key",
			AuthCryptKey:            "1234567812345678",
			AllowUserLogin:          false,
			AllowUserRegister:       false,
			AllowUserChangeUsername: false,
			AllowFlowLimit:          false,
			AllowRateLimit:          false,
			AllowTunnelNumLimit:     false,
			AllowLocalProxy:         false,
			AllowConnectionNumLimit: false,
			AllowMultiIP:            false,
			SystemInfoDisplay:       false,
			HttpCache:               false,
			HttpCacheLength:         100,
			HttpAddOriginHeader:     false,
			DisconnectTimeout:       60,
		}
		// 将默认配置直接写入全局 beego.AppConfig，不进行文件读写。
		cfg.ApplyToAppConfig()
	} else {
		logs.Info("配置文件为：" + confPath)
	}
	common.InitPProfFromFile()
	if level = beego.AppConfig.String("log_level"); level == "" {
		level = "7"
	}
	logs.Reset()
	logs.EnableFuncCallDepth(true)
	logs.SetLogFuncCallDepth(3)
	logPath := beego.AppConfig.String("log_path")
	if logPath == "" {
		logPath = common.GetLogPath()
	}
	// Windows 下日志文件路径需要将单反斜杠转义为双反斜杠，否则部分日志后端解析会出错。
	if common.IsWindows() {
		logPath = strings.Replace(logPath, "\\", "\\\\", -1)
	}
	// init service
	options := make(service.KeyValue)
	// 构造系统服务配置（跨平台），用于安装/启动/停止等控制。
	progName := programBaseName()
	svcConfig := &service.Config{
		Name:        progName,
		DisplayName: progName + "内网穿透代理服务器",
		Description: "一款轻量级、功能强大的服务器。",
		Option:      options,
	}
	svcConfig.Arguments = append(svcConfig.Arguments, "service")
	// 若以“service”参数运行，则将日志输出到文件；否则输出到控制台。
	if len(os.Args) > 1 && os.Args[1] == "service" {
		_ = logs.SetLogger(logs.AdapterFile, `{"level":`+level+`,"filename":"`+logPath+`","daily":false,"maxlines":100000,"color":true}`)
	} else {
		_ = logs.SetLogger(logs.AdapterConsole, `{"level":`+level+`,"color":true}`)
	}
	// 非 Windows 平台：声明 service 依赖关系，并注入 systemd/sysv 启动脚本模板。
	if !common.IsWindows() {
		svcConfig.Dependencies = []string{
			"Requires=network.target",
			"After=network-online.target syslog.target"}
		svcConfig.Option["SystemdScript"] = install.SystemdScript
		svcConfig.Option["SysvScript"] = install.SysvScript
	}
	// 创建服务程序实例（实现 service.Interface 接口）。
	prg := &nps{}
	prg.exit = make(chan struct{})
	s, err := service.New(prg, svcConfig)
	if err != nil {
		// 创建系统服务失败时，打印错误并以普通进程方式继续运行。
		logs.Error(err, "service function disabled")
		run()
		// run without service
		wg := sync.WaitGroup{}
		wg.Add(1)
		wg.Wait()
		return
	}
	if len(os.Args) > 1 && os.Args[1] != "service" {
		// 解析子命令，对服务进行对应的控制或维护操作。
		switch os.Args[1] {
		case "reload":
			// 触发热重载/后台守护逻辑（根据平台实现）。
			daemon.InitDaemon("nps", common.GetRunPath(), common.GetTmpPath())
			return
		case "install":
			// 安装服务：若已存在则先停止并卸载，再按平台正确注册。
			// uninstall before
			_ = service.Control(s, "stop")
			_ = service.Control(s, "uninstall")

			var binPath string
			// 判断是否使用 --force 参数强制重新安装
			if len(os.Args) > 2 && os.Args[2] == "--force" {
				// 强制重新安装 NPS 服务端二进制文件
				binPath = install.ReInstallNps()
			} else {
				// 安装 NPS 服务端二进制文件
				binPath = install.InstallNps()
			}

			svcConfig.Executable = binPath
			s, err := service.New(prg, svcConfig)
			if err != nil {
				logs.Error(err)
				return
			}
			err = service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			if service.Platform() == "unix-systemv" {
				// unix-systemv 平台：创建运行级别软链接以便系统自启/停止。
				logs.Info("unix-systemv service")
				confPath := "/etc/init.d/" + svcConfig.Name
				os.Symlink(confPath, "/etc/rc.d/S90"+svcConfig.Name)
				os.Symlink(confPath, "/etc/rc.d/K02"+svcConfig.Name)
			}
			return
		case "start", "restart", "stop":
			// 启动/重启/停止服务：在 unix-systemv 与其他平台分别处理。
			if service.Platform() == "unix-systemv" {
				logs.Info("unix-systemv service")
				cmd := exec.Command("/etc/init.d/"+svcConfig.Name, os.Args[1])
				err := cmd.Run()
				if err != nil {
					logs.Error(err)
				}
				return
			}
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			return
		case "uninstall":
			// 卸载服务：调用服务控制接口，unix-systemv 需清理 rc.d 软链接。
			err := service.Control(s, os.Args[1])
			if err != nil {
				logs.Error("Valid actions: %q\n%s", service.ControlAction, err.Error())
			}
			if service.Platform() == "unix-systemv" {
				// unix-systemv 平台：删除运行级别软链接，清理残留。
				logs.Info("unix-systemv service")
				os.Remove("/etc/rc.d/S90" + svcConfig.Name)
				os.Remove("/etc/rc.d/K02" + svcConfig.Name)
			}
			return
		case "update":
			// 在线更新二进制并替换至安装路径（具体由 install 包处理）。
			install.UpdateNps()
			return
		default:
			// 未知命令：提示不支持。
			logs.Error("command is not support")
			return
		}
	}
	// 以系统服务方式启动并阻塞运行，直到被 Stop() 或系统控制。
	_ = s.Run()
}

// nps 实现 service.Interface，描述服务的生命周期与退出信号。
// 其中 exit 用于在 Stop() 时通知后台 goroutine 优雅退出。
type nps struct {
	exit chan struct{}
}

// Start 实现 service.Interface 的启动逻辑：
// 调用时机由系统服务框架决定，此处仅启动后台运行逻辑并立即返回。
func (p *nps) Start(s service.Service) error {
	_, _ = s.Status()
	go p.run()
	return nil
}

// Stop 实现 service.Interface 的停止逻辑：
// 关闭退出通道以通知后台 goroutine 结束，交由系统服务管理器完成进程回收。
func (p *nps) Stop(s service.Service) error {
	_, _ = s.Status()
	close(p.exit)
	if service.Interactive() {
		os.Exit(0)
	}
	return nil
}

// run 为服务的后台运行逻辑：
// 负责启动核心功能，并监听退出信号以便优雅停止。
func (p *nps) run() error {
	// 捕获 panic，记录调用栈以便排查，避免进程直接崩溃退出。
	defer func() {
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			logs.Warning("nps: panic serving %v: %v\n%s", err, string(buf))
		}
	}()
	run()
	select {
	case <-p.exit:
		logs.Warning("stop...")
	}
	return nil
}

// run 启动 Web 管理端、桥接服务与必要的系统组件。
func run() {
	// 初始化 Web 路由与管理后台。
	routers.Init()
	// 声明一个 Tunnel 任务，模式为 webServer，用于作为管理端与桥接逻辑的入口。
	task := &file.Tunnel{
		Mode: "webServer",
	}
	// 读取桥接端口（server 与 client 之间的通信端口）。
	bridgePort, err := beego.AppConfig.Int("bridge_port")
	if err != nil {
		logs.Error("Getting bridge_port error", err)
		os.Exit(0)
	}
	// 打印当前服务端版本与允许的客户端核心版本范围，便于兼容性排查。
	logs.Info("the version of server is %s ,allow client core version to be %s", version.VERSION, version.GetVersion())
	// 初始化连接管理模块（心跳、注册、转发等会依赖该服务）。
	connection.InitConnectionService()
	// 初始化 TLS：若未显式指定证书，将尝试按默认路径或配置加载。
	//crypt.InitTls(filepath.Join(common.GetRunPath(), "conf", "server.pem"), filepath.Join(common.GetRunPath(), "conf", "server.key"))
	crypt.InitTls()
	// 初始化端口白名单/限制策略（若配置开启）。
	tool.InitAllowPort()
	// 启动系统信息采集（监控 CPU/内存/网络等，用于后台展示与诊断）。
	tool.StartSystemInfo()
	// 读取客户端断连超时时间（秒），用于清理长时间无心跳的连接。
	timeout, err := beego.AppConfig.Int("disconnect_timeout")
	if err != nil {
		timeout = 60
	}
	go server.StartNewServer(bridgePort, task, beego.AppConfig.String("bridge_type"), timeout)
}

// DefaultConfig 作为“类”承载默认配置，不依赖任何文件。
type DefaultConfig struct {
	AppName              string
	RunMode              string
	HttpProxyIp          string
	HttpProxyPort        int
	HttpsProxyPort       int
	HttpsJustProxy       bool
	HttpsDefaultCertFile string
	HttpsDefaultKeyFile  string

	BridgeType string
	BridgePort int
	BridgeIp   string

	PublicVKey string

	LogLevel int
	LogPath  string

	WebHost     string
	WebUsername string
	WebPassword string
	WebPort     int
	WebIp       string
	WebBaseURL  string
	WebOpenSSL  bool
	WebCertFile string
	WebKeyFile  string

	AuthCryptKey string

	AllowUserLogin          bool
	AllowUserRegister       bool
	AllowUserChangeUsername bool
	AllowFlowLimit          bool
	AllowRateLimit          bool
	AllowTunnelNumLimit     bool
	AllowLocalProxy         bool
	AllowConnectionNumLimit bool
	AllowMultiIP            bool
	SystemInfoDisplay       bool
	HttpCache               bool
	HttpCacheLength         int
	HttpAddOriginHeader     bool
	DisconnectTimeout       int
}

// ApplyToAppConfig 将默认配置直接写入 beego.AppConfig。
func (c DefaultConfig) ApplyToAppConfig() {
	_ = beego.AppConfig.Set("appname", c.AppName)
	_ = beego.AppConfig.Set("runmode", c.RunMode)

	_ = beego.AppConfig.Set("http_proxy_ip", c.HttpProxyIp)
	_ = beego.AppConfig.Set("http_proxy_port", strconv.Itoa(c.HttpProxyPort))
	_ = beego.AppConfig.Set("https_proxy_port", strconv.Itoa(c.HttpsProxyPort))
	_ = beego.AppConfig.Set("https_just_proxy", strconv.FormatBool(c.HttpsJustProxy))
	_ = beego.AppConfig.Set("https_default_cert_file", c.HttpsDefaultCertFile)
	_ = beego.AppConfig.Set("https_default_key_file", c.HttpsDefaultKeyFile)

	_ = beego.AppConfig.Set("bridge_type", c.BridgeType)
	_ = beego.AppConfig.Set("bridge_port", strconv.Itoa(c.BridgePort))
	_ = beego.AppConfig.Set("bridge_ip", c.BridgeIp)

	_ = beego.AppConfig.Set("public_vkey", c.PublicVKey)

	_ = beego.AppConfig.Set("log_level", strconv.Itoa(c.LogLevel))
	if c.LogPath != "" {
		_ = beego.AppConfig.Set("log_path", c.LogPath)
	}

	_ = beego.AppConfig.Set("web_host", c.WebHost)
	_ = beego.AppConfig.Set("web_username", c.WebUsername)
	_ = beego.AppConfig.Set("web_password", c.WebPassword)
	_ = beego.AppConfig.Set("web_port", strconv.Itoa(c.WebPort))
	_ = beego.AppConfig.Set("web_ip", c.WebIp)
	_ = beego.AppConfig.Set("web_base_url", c.WebBaseURL)
	_ = beego.AppConfig.Set("web_open_ssl", strconv.FormatBool(c.WebOpenSSL))
	_ = beego.AppConfig.Set("web_cert_file", c.WebCertFile)
	_ = beego.AppConfig.Set("web_key_file", c.WebKeyFile)

	_ = beego.AppConfig.Set("auth_crypt_key", c.AuthCryptKey)

	_ = beego.AppConfig.Set("allow_user_login", strconv.FormatBool(c.AllowUserLogin))
	_ = beego.AppConfig.Set("allow_user_register", strconv.FormatBool(c.AllowUserRegister))
	_ = beego.AppConfig.Set("allow_user_change_username", strconv.FormatBool(c.AllowUserChangeUsername))

	_ = beego.AppConfig.Set("allow_flow_limit", strconv.FormatBool(c.AllowFlowLimit))
	_ = beego.AppConfig.Set("allow_rate_limit", strconv.FormatBool(c.AllowRateLimit))
	_ = beego.AppConfig.Set("allow_tunnel_num_limit", strconv.FormatBool(c.AllowTunnelNumLimit))
	_ = beego.AppConfig.Set("allow_local_proxy", strconv.FormatBool(c.AllowLocalProxy))
	_ = beego.AppConfig.Set("allow_connection_num_limit", strconv.FormatBool(c.AllowConnectionNumLimit))
	_ = beego.AppConfig.Set("allow_multi_ip", strconv.FormatBool(c.AllowMultiIP))
	_ = beego.AppConfig.Set("system_info_display", strconv.FormatBool(c.SystemInfoDisplay))

	_ = beego.AppConfig.Set("http_cache", strconv.FormatBool(c.HttpCache))
	_ = beego.AppConfig.Set("http_cache_length", strconv.Itoa(c.HttpCacheLength))
	_ = beego.AppConfig.Set("http_add_origin_header", strconv.FormatBool(c.HttpAddOriginHeader))

	_ = beego.AppConfig.Set("disconnect_timeout", strconv.Itoa(c.DisconnectTimeout))
}

// programBaseName 返回当前运行程序的基名（去除 .exe 扩展名）
func programBaseName() string {
	exe, err := os.Executable()
	if err != nil {
		base := filepath.Base(os.Args[0])
		if strings.HasSuffix(strings.ToLower(base), ".exe") {
			return strings.TrimSuffix(base, ".exe")
		}
		return base
	}
	base := filepath.Base(exe)
	if strings.HasSuffix(strings.ToLower(base), ".exe") {
		return strings.TrimSuffix(base, ".exe")
	}
	return base
}
