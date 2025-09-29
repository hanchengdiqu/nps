// Package server 实现了NPS（内网穿透代理服务器）的核心服务端逻辑
// 主要功能包括：
// - 管理各种代理服务的启动、停止和监控
// - 处理客户端连接和任务调度
// - 提供系统状态监控和流量统计
// - 支持多种代理模式：TCP、UDP、HTTP、SOCKS5、P2P等
package server

import (
	"errors"  // 错误处理
	"math"    // 数学计算
	"os"      // 操作系统接口
	"strconv" // 字符串转换
	"strings" // 字符串处理
	"sync"    // 同步原语
	"time"    // 时间处理

	"ehang.io/nps/lib/backup"
	"ehang.io/nps/lib/email"
	"ehang.io/nps/lib/version" // 版本信息

	"ehang.io/nps/bridge"     // 桥接层，处理服务端与客户端通信
	"ehang.io/nps/lib/common" // 通用工具函数
	"ehang.io/nps/lib/file"   // 文件操作和数据结构

	// 邮件服务
	// 备份服务
	"ehang.io/nps/server/proxy"          // 代理服务实现
	"ehang.io/nps/server/tool"           // 服务端工具函数
	"github.com/astaxie/beego"           // Web框架
	"github.com/astaxie/beego/logs"      // 日志系统
	"github.com/shirou/gopsutil/v3/cpu"  // CPU使用率监控
	"github.com/shirou/gopsutil/v3/load" // 系统负载监控
	"github.com/shirou/gopsutil/v3/mem"  // 内存使用监控
	"github.com/shirou/gopsutil/v3/net"  // 网络统计监控
)

var (
	// Bridge 全局桥接对象，负责管理与所有客户端的连接和通信
	// 包含客户端连接管理、任务调度、健康检查等功能
	Bridge *bridge.Bridge

	// RunList 运行中的任务列表，使用sync.Map保证并发安全
	// key: 任务ID (int)
	// value: 代理服务实例 (interface{})
	RunList sync.Map //map[int]interface{}
)

// init 初始化函数，在包被导入时自动执行
// 初始化运行任务列表
func init() {
	RunList = sync.Map{}
}

// InitFromCsv 从数据库初始化任务
// 从配置文件中读取已保存的任务配置，并启动状态为开启的任务
// 同时添加公共密码客户端（如果配置了public_vkey）
func InitFromCsv() {
	// 添加公共密码客户端
	// 如果配置了public_vkey，创建一个公共客户端供所有用户使用
	if vkey := beego.AppConfig.String("public_vkey"); vkey != "" {
		c := file.NewClient(vkey, true, true) // 创建公共客户端，不存储到文件，不显示在Web界面
		file.GetDb().NewClient(c)             // 将客户端添加到数据库
		RunList.Store(c.Id, nil)              // 将客户端ID存储到运行列表
		//RunList[c.Id] = nil
	}

	// 初始化服务端文件中的服务
	// 遍历数据库中的所有任务，启动状态为开启的任务
	file.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		if value.(*file.Tunnel).Status { // 只启动状态为开启的任务
			AddTask(value.(*file.Tunnel))
		}
		return true
	})
}

// DealBridgeTask 处理桥接任务
// 监听桥接层的各种事件，包括：
// - 开启任务：接收新任务并启动
// - 关闭任务：停止指定任务
// - 关闭客户端：删除客户端及其所有任务
// - 秘密连接：处理秘密隧道连接
func DealBridgeTask() {
	for {
		select {
		case t := <-Bridge.OpenTask:
			// 开启新任务
			AddTask(t)
		case t := <-Bridge.CloseTask:
			// 关闭指定任务
			StopServer(t.Id)
		case id := <-Bridge.CloseClient:
			// 关闭客户端，删除其所有隧道和主机配置
			DelTunnelAndHostByClientId(id, true)
			// 如果客户端标记为不存储，则从数据库中删除
			if v, ok := file.GetDb().JsonDb.Clients.Load(id); ok {
				if v.(*file.Client).NoStore {
					file.GetDb().DelClient(id)
				}
			}
		case tunnel := <-Bridge.OpenTask:
			// 启动指定任务（与第一个case重复，可能是代码冗余）
			StartTask(tunnel.Id)
		case s := <-Bridge.SecretChan:
			// 处理秘密连接
			logs.Trace("New secret connection, addr", s.Conn.Conn.RemoteAddr())
			// 根据密码查找对应的任务
			if t := file.GetDb().GetTaskByMd5Password(s.Password); t != nil {
				if t.Status {
					// 任务状态为开启，创建基础服务器处理连接
					go proxy.NewBaseServer(Bridge, t).DealClient(s.Conn, t.Client, t.Target.TargetStr, nil, common.CONN_TCP, nil, t.Flow, t.Target.LocalProxy)
				} else {
					// 任务状态为关闭，拒绝连接
					s.Conn.Close()
					logs.Trace("This key %s cannot be processed,status is close", s.Password)
				}
			} else {
				// 未找到对应任务，拒绝连接
				logs.Trace("This key %s cannot be processed", s.Password)
				s.Conn.Close()
			}
		}
	}
}

// StartNewServer 启动新的服务器
// 参数：
//   - bridgePort: 桥接端口号
//   - cnf: 隧道配置
//   - bridgeType: 桥接类型（tcp/kcp）
//   - bridgeDisconnect: 桥接断开时间
func StartNewServer(bridgePort int, cnf *file.Tunnel, bridgeType string, bridgeDisconnect int) {
	// 创建桥接对象
	Bridge = bridge.NewTunnel(bridgePort, bridgeType, common.GetBoolByStr(beego.AppConfig.String("ip_limit")), RunList, bridgeDisconnect)

	// 启动桥接服务
	go func() {
		if err := Bridge.StartTunnel(); err != nil {
			logs.Error("start server bridge error", err)
			os.Exit(0)
		}
	}()

	// 启动P2P服务器（如果配置了P2P端口）
	if p, err := beego.AppConfig.Int("p2p_port"); err == nil {
		go proxy.NewP2PServer(p).Start()     // 启动P2P服务器
		go proxy.NewP2PServer(p + 1).Start() // 启动备用P2P服务器
		go proxy.NewP2PServer(p + 2).Start() // 启动备用P2P服务器
	}

	// 启动任务处理协程
	go DealBridgeTask()

	// 启动客户端流量处理协程
	go dealClientFlow()

	// 根据配置创建并启动对应的服务模式
	if svr := NewMode(Bridge, cnf); svr != nil {
		if err := svr.Start(); err != nil {
			logs.Error(err)
		}
		RunList.Store(cnf.Id, svr) // 将服务存储到运行列表
		//RunList[cnf.Id] = svr
	} else {
		logs.Error("Incorrect startup mode %s", cnf.Mode)
	}
}

// dealClientFlow 处理客户端流量
// 定时处理客户端数据，包括流量统计和状态更新
func dealClientFlow() {
	ticker := time.NewTicker(time.Minute) // 每分钟执行一次
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			dealClientData() // 处理客户端数据
		}
	}
}

// NewMode 根据模式名称创建新的服务器
// 参数：
//   - Bridge: 桥接对象
//   - c: 隧道配置
//
// 返回：
//   - proxy.Service: 对应的代理服务实例
func NewMode(Bridge *bridge.Bridge, c *file.Tunnel) proxy.Service {
	var service proxy.Service
	switch c.Mode {
	case "tcp", "file":
		// TCP代理模式和文件传输模式
		service = proxy.NewTunnelModeServer(proxy.ProcessTunnel, Bridge, c)
	case "socks5":
		// SOCKS5代理模式
		service = proxy.NewSock5ModeServer(Bridge, c)
	case "httpProxy":
		// HTTP代理模式
		service = proxy.NewTunnelModeServer(proxy.ProcessHttp, Bridge, c)
	case "tcpTrans":
		// TCP传输模式
		service = proxy.NewTunnelModeServer(proxy.HandleTrans, Bridge, c)
	case "udp":
		// UDP代理模式
		service = proxy.NewUdpModeServer(Bridge, c)
	case "webServer":
		// Web服务器模式
		InitFromCsv() // 初始化所有任务
		t := &file.Tunnel{
			Port:   0,
			Mode:   "httpHostServer",
			Status: true,
		}
		AddTask(t) // 添加HTTP主机服务器任务
		service = proxy.NewWebServer(Bridge)
	case "httpHostServer":
		// HTTP主机服务器模式
		httpPort, _ := beego.AppConfig.Int("http_proxy_port")          // HTTP代理端口
		httpsPort, _ := beego.AppConfig.Int("https_proxy_port")        // HTTPS代理端口
		useCache, _ := beego.AppConfig.Bool("http_cache")              // 是否使用缓存
		cacheLen, _ := beego.AppConfig.Int("http_cache_length")        // 缓存长度
		addOrigin, _ := beego.AppConfig.Bool("http_add_origin_header") // 是否添加Origin头
		service = proxy.NewHttp(Bridge, c, httpPort, httpsPort, useCache, cacheLen, addOrigin)
	}
	return service
}

// StopServer 停止服务器
// 参数：
//   - id: 任务ID
//
// 返回：
//   - error: 错误信息
func StopServer(id int) error {
	//if v, ok := RunList[id]; ok {
	if v, ok := RunList.Load(id); ok { // 从运行列表中查找任务
		if svr, ok := v.(proxy.Service); ok { // 类型断言为代理服务
			if err := svr.Close(); err != nil { // 关闭服务
				return err
			}
			logs.Info("stop server id %d", id)
		} else {
			logs.Warn("stop server id %d error", id)
		}
		// 更新任务状态为关闭
		if t, err := file.GetDb().GetTask(id); err != nil {
			return err
		} else {
			t.Status = false
			file.GetDb().UpdateTask(t)
		}
		//delete(RunList, id)
		RunList.Delete(id) // 从运行列表中删除
		return nil
	}
	return errors.New("task is not running")
}

// AddTask 添加任务
// 参数：
//   - t: 隧道配置
//
// 返回：
//   - error: 错误信息
func AddTask(t *file.Tunnel) error {
	// 处理秘密任务和P2P任务
	if t.Mode == "secret" || t.Mode == "p2p" {
		logs.Info("secret task %s start ", t.Remark)
		//RunList[t.Id] = nil
		RunList.Store(t.Id, nil) // 秘密任务不需要启动实际服务
		return nil
	}

	// 测试服务器端口是否可用（HTTP主机服务器模式除外）
	if b := tool.TestServerPort(t.Port, t.Mode); !b && t.Mode != "httpHostServer" {
		logs.Error("taskId %d start error port %d open failed", t.Id, t.Port)
		return errors.New("the port open error")
	}

	// 启动流量会话存储（如果配置了存储间隔）
	if minute, err := beego.AppConfig.Int("flow_store_interval"); err == nil && minute > 0 {
		go flowSession(time.Minute * time.Duration(minute))
	}

	// 创建并启动对应的服务模式
	if svr := NewMode(Bridge, t); svr != nil {
		logs.Info("tunnel task %s start mode：%s port %d", t.Remark, t.Mode, t.Port)
		//RunList[t.Id] = svr
		RunList.Store(t.Id, svr) // 将服务存储到运行列表
		go func() {
			if err := svr.Start(); err != nil {
				logs.Error("clientId %d taskId %d start error %s", t.Client.Id, t.Id, err)
				//delete(RunList, t.Id)
				RunList.Delete(t.Id) // 启动失败时从运行列表删除
				return
			}
		}()
	} else {
		return errors.New("the mode is not correct")
	}
	return nil
}

// StartTask 启动任务
// 参数：
//   - id: 任务ID
//
// 返回：
//   - error: 错误信息
func StartTask(id int) error {
	if t, err := file.GetDb().GetTask(id); err != nil {
		return err
	} else {
		AddTask(t)                 // 添加任务
		t.Status = true            // 设置任务状态为开启
		file.GetDb().UpdateTask(t) // 更新数据库
	}
	return nil
}

// DelTask 删除任务
// 参数：
//   - id: 任务ID
//
// 返回：
//   - error: 错误信息
func DelTask(id int) error {
	//if _, ok := RunList[id]; ok {
	if _, ok := RunList.Load(id); ok { // 检查任务是否在运行
		if err := StopServer(id); err != nil { // 停止服务器
			return err
		}
	}
	return file.GetDb().DelTask(id) // 从数据库删除任务
}

// GetTunnel 分页获取隧道列表
// 参数：
//   - start: 起始位置
//   - length: 每页长度
//   - typeVal: 类型过滤
//   - clientId: 客户端ID过滤
//   - search: 搜索关键词
//
// 返回：
//   - []*file.Tunnel: 隧道列表
//   - int: 总数量
func GetTunnel(start, length int, typeVal string, clientId int, search string) ([]*file.Tunnel, int) {
	list := make([]*file.Tunnel, 0)
	var cnt int

	// 获取所有任务键
	keys := file.GetMapKeys(file.GetDb().JsonDb.Tasks, false, "", "")
	for _, key := range keys {
		if value, ok := file.GetDb().JsonDb.Tasks.Load(key); ok {
			v := value.(*file.Tunnel)

			// 类型过滤
			if (typeVal != "" && v.Mode != typeVal || (clientId != 0 && v.Client.Id != clientId)) || (typeVal == "" && clientId != v.Client.Id) {
				continue
			}

			// 搜索过滤
			if search != "" && !(v.Id == common.GetIntNoErrByStr(search) || v.Port == common.GetIntNoErrByStr(search) || strings.Contains(v.Password, search) || strings.Contains(v.Remark, search)) {
				continue
			}

			cnt++

			// 检查客户端连接状态
			if _, ok := Bridge.Client.Load(v.Client.Id); ok {
				v.Client.IsConnect = true
			} else {
				v.Client.IsConnect = false
			}

			// 分页处理
			if start--; start < 0 {
				if length--; length >= 0 {
					//if _, ok := RunList[v.Id]; ok {
					if _, ok := RunList.Load(v.Id); ok { // 检查任务运行状态
						v.RunStatus = true
					} else {
						v.RunStatus = false
					}
					list = append(list, v)
				}
			}
		}
	}
	return list, cnt
}

// GetClientList 获取客户端列表
// 参数：
//   - start: 起始位置
//   - length: 每页长度
//   - search: 搜索关键词
//   - sort: 排序字段
//   - order: 排序顺序
//   - clientId: 客户端ID过滤
//
// 返回：
//   - list: 客户端列表
//   - cnt: 总数量
func GetClientList(start, length int, search, sort, order string, clientId int) (list []*file.Client, cnt int) {
	list, cnt = file.GetDb().GetClientList(start, length, search, sort, order, clientId)
	dealClientData() // 处理客户端数据
	return
}

// dealClientData 处理客户端数据
// 更新客户端连接状态、版本信息和流量统计
func dealClientData() {
	file.GetDb().JsonDb.Clients.Range(func(key, value interface{}) bool {
		v := value.(*file.Client)

		// 检查客户端连接状态
		if vv, ok := Bridge.Client.Load(v.Id); ok {
			v.IsConnect = true
			v.Version = vv.(*bridge.Client).Version // 更新客户端版本
		} else {
			v.IsConnect = false
		}

		// 重置流量统计
		v.Flow.InletFlow = 0
		v.Flow.ExportFlow = 0

		// 统计主机流量
		file.GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
			h := value.(*file.Host)
			if h.Client.Id == v.Id {
				v.Flow.InletFlow += h.Flow.InletFlow
				v.Flow.ExportFlow += h.Flow.ExportFlow
			}
			return true
		})

		// 统计任务流量
		file.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
			t := value.(*file.Tunnel)
			if t.Client.Id == v.Id {
				v.Flow.InletFlow += t.Flow.InletFlow
				v.Flow.ExportFlow += t.Flow.ExportFlow
			}
			return true
		})
		return true
	})
	return
}

// DelTunnelAndHostByClientId 根据客户端ID删除所有隧道和主机
// 参数：
//   - clientId: 客户端ID
//   - justDelNoStore: 是否只删除不存储的项目
func DelTunnelAndHostByClientId(clientId int, justDelNoStore bool) {
	var ids []int

	// 删除客户端的所有任务
	file.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		v := value.(*file.Tunnel)
		if justDelNoStore && !v.NoStore { // 如果只删除不存储的项目，跳过需要存储的项目
			return true
		}
		if v.Client.Id == clientId {
			ids = append(ids, v.Id)
		}
		return true
	})
	for _, id := range ids {
		DelTask(id) // 删除任务
	}

	// 删除客户端的所有主机配置
	ids = ids[:0] // 重置切片
	file.GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
		v := value.(*file.Host)
		if justDelNoStore && !v.NoStore { // 如果只删除不存储的项目，跳过需要存储的项目
			return true
		}
		if v.Client.Id == clientId {
			ids = append(ids, v.Id)
		}
		return true
	})
	for _, id := range ids {
		file.GetDb().DelHost(id) // 删除主机配置
	}
}

// DelClientConnect 关闭客户端连接
// 参数：
//   - clientId: 客户端ID
func DelClientConnect(clientId int) {
	Bridge.DelClient(clientId)
}

// GetDashboardData 获取仪表板数据
// 返回系统状态、流量统计、任务数量等监控信息
// 返回：
//   - map[string]interface{}: 包含各种监控数据的映射
func GetDashboardData() map[string]interface{} {
	data := make(map[string]interface{})

	// 基本信息
	data["version"] = version.VERSION                                       // 版本信息
	data["hostCount"] = common.GeSynctMapLen(file.GetDb().JsonDb.Hosts)     // 主机数量
	data["clientCount"] = common.GeSynctMapLen(file.GetDb().JsonDb.Clients) // 客户端数量

	// 如果配置了公共密钥，客户端数量减1（排除公共客户端）
	if beego.AppConfig.String("public_vkey") != "" {
		data["clientCount"] = data["clientCount"].(int) - 1
	}

	// 处理客户端数据
	dealClientData()

	// 统计在线客户端和流量
	c := 0
	var in, out int64
	file.GetDb().JsonDb.Clients.Range(func(key, value interface{}) bool {
		v := value.(*file.Client)
		if v.IsConnect {
			c += 1 // 在线客户端计数
		}
		in += v.Flow.InletFlow   // 入站流量
		out += v.Flow.ExportFlow // 出站流量
		return true
	})
	data["clientOnlineCount"] = c
	data["inletFlowCount"] = int(in)
	data["exportFlowCount"] = int(out)

	// 统计各种类型的任务数量
	var tcp, udp, secret, socks5, p2p, http int
	file.GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		switch value.(*file.Tunnel).Mode {
		case "tcp":
			tcp += 1
		case "socks5":
			socks5 += 1
		case "httpProxy":
			http += 1
		case "udp":
			udp += 1
		case "p2p":
			p2p += 1
		case "secret":
			secret += 1
		}
		return true
	})

	data["tcpC"] = tcp
	data["udpCount"] = udp
	data["socks5Count"] = socks5
	data["httpProxyCount"] = http
	data["secretCount"] = secret
	data["p2pCount"] = p2p

	// 配置信息
	data["bridgeType"] = beego.AppConfig.String("bridge_type")
	data["httpProxyPort"] = beego.AppConfig.String("http_proxy_port")
	data["httpsProxyPort"] = beego.AppConfig.String("https_proxy_port")
	data["ipLimit"] = beego.AppConfig.String("ip_limit")
	data["flowStoreInterval"] = beego.AppConfig.String("flow_store_interval")
	data["serverIp"] = beego.AppConfig.String("p2p_ip")
	data["p2pPort"] = beego.AppConfig.String("p2p_port")
	data["logLevel"] = beego.AppConfig.String("log_level")

	// 统计TCP连接数
	tcpCount := 0
	file.GetDb().JsonDb.Clients.Range(func(key, value interface{}) bool {
		tcpCount += int(value.(*file.Client).NowConn)
		return true
	})
	data["tcpCount"] = tcpCount

	// 系统监控信息
	cpuPercet, _ := cpu.Percent(0, true) // CPU使用率
	var cpuAll float64
	for _, v := range cpuPercet {
		cpuAll += v
	}
	loads, _ := load.Avg() // 系统负载
	data["load"] = loads.String()
	data["cpu"] = math.Round(cpuAll / float64(len(cpuPercet))) // 平均CPU使用率

	swap, _ := mem.SwapMemory() // 交换内存
	data["swap_mem"] = math.Round(swap.UsedPercent)
	vir, _ := mem.VirtualMemory() // 虚拟内存
	data["virtual_mem"] = math.Round(vir.UsedPercent)

	// 网络统计
	conn, _ := net.ProtoCounters(nil)  // 协议计数器
	io1, _ := net.IOCounters(false)    // 网络IO统计
	time.Sleep(time.Millisecond * 500) // 等待500毫秒
	io2, _ := net.IOCounters(false)    // 再次获取网络IO统计
	if len(io2) > 0 && len(io1) > 0 {
		// 计算网络IO速率（字节/秒）
		data["io_send"] = (io2[0].BytesSent - io1[0].BytesSent) * 2
		data["io_recv"] = (io2[0].BytesRecv - io1[0].BytesRecv) * 2
	}

	// 协议连接统计
	for _, v := range conn {
		data[v.Protocol] = v.Stats["CurrEstab"] // 当前建立的连接数
	}

	// 系统状态图表数据
	var fg int
	if len(tool.ServerStatus) >= 10 {
		fg = len(tool.ServerStatus) / 10
		for i := 0; i <= 9; i++ {
			data["sys"+strconv.Itoa(i+1)] = tool.ServerStatus[i*fg] // 系统状态数据点
		}
	}
	return data
}

// flowSession 流量会话存储
// 定时将流量数据存储到JSON文件
// 参数：
//   - m: 存储间隔时间
func flowSession(m time.Duration) {
	ticker := time.NewTicker(m)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// 存储各种数据到JSON文件
			file.GetDb().JsonDb.StoreHostToJsonFile()    // 存储主机数据
			file.GetDb().JsonDb.StoreTasksToJsonFile()   // 存储任务数据
			file.GetDb().JsonDb.StoreClientsToJsonFile() // 存储客户端数据
		}
	}
}

// StartBackupService 启动备份服务
func StartBackupService() {
	if !beego.AppConfig.DefaultBool("email_backup_enabled", false) {
		logs.Info("Email backup service is disabled")
		return
	}

	interval := beego.AppConfig.DefaultInt("email_backup_interval", 24) // 默认24小时
	if interval <= 0 {
		logs.Warning("Invalid backup interval: %d hours, backup service disabled", interval)
		return
	}

	logs.Info("Starting email backup service, interval: %d hours", interval)

	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Hour)
		defer ticker.Stop()

		// 立即执行一次备份
		performBackup()

		for {
			select {
			case <-ticker.C:
				performBackup()
			}
		}
	}()
}

// performBackup 执行备份
func performBackup() {
	logs.Info("Starting scheduled backup...")

	// 在创建备份前，先将内存中的数据存储到JSON文件
	logs.Info("Storing current data to JSON files...")
	file.GetDb().JsonDb.StoreHostToJsonFile()    // 存储主机数据
	file.GetDb().JsonDb.StoreTasksToJsonFile()   // 存储任务数据
	file.GetDb().JsonDb.StoreClientsToJsonFile() // 存储客户端数据
	logs.Info("Data stored to JSON files successfully")

	// 创建备份
	backupService := backup.NewBackupService()
	backupPath, err := backupService.CreateBackup()
	if err != nil {
		logs.Error("Failed to create backup: %v", err)
		return
	}

	// 发送邮件
	emailService := email.NewEmailService()
	if err := emailService.SendBackupEmail([]string{backupPath}); err != nil {
		logs.Error("Failed to send backup email: %v", err)
	} else {
		logs.Info("Backup email sent successfully")
	}

	// 清理旧备份
	if err := backupService.CleanOldBackups(); err != nil {
		logs.Error("Failed to clean old backups: %v", err)
	}
}
