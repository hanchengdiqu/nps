package tool

import (
	"math"
	"strconv"
	"time"

	"ehang.io/nps/lib/common"
	"github.com/astaxie/beego"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

var (
	// ports 存储允许使用的端口列表
	ports        []int
	// ServerStatus 存储服务器状态历史数据，最多保存1440条记录（24小时的数据）
	ServerStatus []map[string]interface{}
)

// StartSystemInfo 启动系统信息收集服务
// 根据配置决定是否启用系统信息显示功能
func StartSystemInfo() {
	// 检查配置文件中是否启用了系统信息显示功能
	if b, err := beego.AppConfig.Bool("system_info_display"); err == nil && b {
		// 初始化服务器状态切片，预分配容量为1500
		ServerStatus = make([]map[string]interface{}, 0, 1500)
		// 启动后台协程持续收集服务器状态
		go getSeverStatus()
	}
}

// InitAllowPort 初始化允许使用的端口列表
// 从配置文件中读取 allow_ports 配置项并解析端口范围
func InitAllowPort() {
	// 从配置文件中获取允许的端口配置字符串
	p := beego.AppConfig.String("allow_ports")
	// 解析端口字符串并转换为整数切片
	ports = common.GetPorts(p)
}

// TestServerPort 测试指定端口是否可用
// 参数:
//   p: 要测试的端口号
//   m: 协议类型 ("p2p", "secret", "udp", "tcp")
// 返回:
//   b: 端口是否可用
func TestServerPort(p int, m string) (b bool) {
	// P2P和密钥模式直接返回true，不需要端口验证
	if m == "p2p" || m == "secret" {
		return true
	}
	// 检查端口号是否在有效范围内
	if p > 65535 || p < 0 {
		return false
	}
	// 如果配置了允许端口列表，检查端口是否在允许列表中
	if len(ports) != 0 {
		if !common.InIntArr(ports, p) {
			return false
		}
	}
	// 根据协议类型进行相应的端口测试
	if m == "udp" {
		// 测试UDP端口是否可用
		b = common.TestUdpPort(p)
	} else {
		// 测试TCP端口是否可用
		b = common.TestTcpPort(p)
	}
	return
}

// getSeverStatus 持续收集服务器状态信息
// 在后台协程中运行，定期收集系统性能指标
func getSeverStatus() {
	for {
		// 根据当前数据量调整收集频率
		// 数据量少时每秒收集一次，数据量多时每分钟收集一次
		if len(ServerStatus) < 10 {
			time.Sleep(time.Second)
		} else {
			time.Sleep(time.Minute)
		}
		
		// 获取CPU使用率（每个核心的百分比）
		cpuPercet, _ := cpu.Percent(0, true)
		var cpuAll float64
		// 计算所有CPU核心的平均使用率
		for _, v := range cpuPercet {
			cpuAll += v
		}
		
		// 创建状态数据映射
		m := make(map[string]interface{})
		
		// 获取系统负载信息
		loads, _ := load.Avg()
		m["load1"] = loads.Load1   // 1分钟平均负载
		m["load5"] = loads.Load5   // 5分钟平均负载
		m["load15"] = loads.Load15 // 15分钟平均负载
		
		// 计算并存储CPU平均使用率（四舍五入到整数）
		m["cpu"] = math.Round(cpuAll / float64(len(cpuPercet)))
		
		// 获取交换内存使用情况
		swap, _ := mem.SwapMemory()
		m["swap_mem"] = math.Round(swap.UsedPercent) // 交换内存使用百分比
		
		// 获取虚拟内存使用情况
		vir, _ := mem.VirtualMemory()
		m["virtual_mem"] = math.Round(vir.UsedPercent) // 虚拟内存使用百分比
		
		// 获取网络协议连接统计
		conn, _ := net.ProtoCounters(nil)
		
		// 获取网络I/O统计（用于计算网络流量）
		io1, _ := net.IOCounters(false)
		time.Sleep(time.Millisecond * 500) // 等待500毫秒
		io2, _ := net.IOCounters(false)
		
		// 计算网络I/O速率（字节/秒）
		if len(io2) > 0 && len(io1) > 0 {
			// 发送速率 = (当前发送字节数 - 500ms前发送字节数) * 2
			m["io_send"] = (io2[0].BytesSent - io1[0].BytesSent) * 2
			// 接收速率 = (当前接收字节数 - 500ms前接收字节数) * 2
			m["io_recv"] = (io2[0].BytesRecv - io1[0].BytesRecv) * 2
		}
		
		// 获取当前时间并格式化为 HH:MM:SS 格式
		t := time.Now()
		m["time"] = strconv.Itoa(t.Hour()) + ":" + strconv.Itoa(t.Minute()) + ":" + strconv.Itoa(t.Second())

		// 存储各种网络协议的当前连接数
		for _, v := range conn {
			m[v.Protocol] = v.Stats["CurrEstab"] // 当前建立的连接数
		}
		
		// 维护数据量限制：最多保存1440条记录（24小时的数据）
		if len(ServerStatus) >= 1440 {
			// 删除最旧的记录（滑动窗口）
			ServerStatus = ServerStatus[1:]
		}
		
		// 添加新的状态记录
		ServerStatus = append(ServerStatus, m)
	}
}
