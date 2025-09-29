package controllers

import (
	"time"

	"ehang.io/nps/lib/backup"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/email"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

// StatusController 系统状态控制器
// 提供系统运行状态的JSON接口
// 路由示例：GET /status/info
// 通过自动路由机制注册（参见 web/routers/router.go）
type StatusController struct {
	BaseController
}

// Info 返回系统运行状态数据（JSON）
// URL: GET /status/info
// 响应：
//   - code: 1 表示成功
//   - data: server.GetDashboardData() 返回的数据
func (s *StatusController) Info() {
	data := make(map[string]interface{})
	data["code"] = 1
	data["data"] = server.GetDashboardData()
	s.Data["json"] = data
	s.ServeJSON()
}

// Stats 返回第三方对接所需的统计数据（JSON）
// URL: GET /status/stats
// 响应：
//   - code: 1 表示成功
//   - data: 包含活跃客户端数量、客户端总数量、活跃隧道数量、隧道总数量、今日流量、域名解析数量
func (s *StatusController) Stats() {
	data := make(map[string]interface{})

	// 获取客户端统计
	totalClients := common.GeSynctMapLen(file.GetDb().JsonDb.Clients)
	// 如果配置了公共密钥，客户端数量减1（排除公共客户端）
	if beego.AppConfig.String("public_vkey") != "" {
		totalClients = totalClients - 1
	}

	// 统计活跃客户端数量
	activeClients := 0
	file.GetDb().JsonDb.Clients.Range(func(key, value interface{}) bool {
		v := value.(*file.Client)
		if vv, ok := server.Bridge.Client.Load(v.Id); ok && vv != nil {
			activeClients++
		}
		return true
	})

	// 获取隧道统计
	totalTunnels := common.GeSynctMapLen(file.GetDb().JsonDb.Tasks)

	// 统计活跃隧道数量（正在运行的隧道）
	activeTunnels := 0
	server.RunList.Range(func(key, value interface{}) bool {
		activeTunnels++
		return true
	})

	// 统计今日流量（入站+出站）
	var todayInFlow, todayOutFlow int64
	file.GetDb().JsonDb.Clients.Range(func(key, value interface{}) bool {
		v := value.(*file.Client)
		todayInFlow += v.Flow.InletFlow
		todayOutFlow += v.Flow.ExportFlow
		return true
	})

	// 统计域名解析数量（主机配置数量）
	domainCount := common.GeSynctMapLen(file.GetDb().JsonDb.Hosts)

	// 构建响应数据
	statsData := map[string]interface{}{
		"active_clients":   activeClients,              // 活跃的客户端数量
		"total_clients":    totalClients,               // 客户端总数量（包括活跃和不活跃的）
		"active_tunnels":   activeTunnels,              // 活跃的隧道数量
		"total_tunnels":    totalTunnels,               // 隧道的总数量
		"today_in_flow":    todayInFlow,                // 今日入站流量（字节）
		"today_out_flow":   todayOutFlow,               // 今日出站流量（字节）
		"today_total_flow": todayInFlow + todayOutFlow, // 今日总流量（字节）
		"domain_count":     domainCount,                // 域名解析数量
		"timestamp":        time.Now().Unix(),          // 当前时间戳
	}

	data["code"] = 1
	data["data"] = statsData
	s.Data["json"] = data
	s.ServeJSON()
}

// Backup 执行数据库备份并发送邮件（JSON）
// URL: POST /status/backup
// 响应：
//   - code: 1 表示成功，0 表示失败
//   - message: 操作结果描述
//   - data: 包含备份文件路径和操作时间戳
func (s *StatusController) Backup() {
	data := make(map[string]interface{})

	// 检查邮件备份是否启用
	//if !beego.AppConfig.DefaultBool("email_backup_enabled", false) {
	//	data["code"] = 0
	//	data["message"] = "邮件备份功能未启用"
	//	s.Data["json"] = data
	//	s.ServeJSON()
	//	return
	//}

	logs.Info("Manual backup triggered via API")

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
		data["code"] = 0
		data["message"] = "创建备份失败: " + err.Error()
		s.Data["json"] = data
		s.ServeJSON()
		return
	}

	// 发送邮件
	emailService := email.NewEmailService()
	if err := emailService.SendBackupEmail([]string{backupPath}); err != nil {
		logs.Error("Failed to send backup email: %v", err)
		data["code"] = 0
		data["message"] = "发送备份邮件失败: " + err.Error()
		s.Data["json"] = data
		s.ServeJSON()
		return
	}

	logs.Info("Manual backup email sent successfully")

	// 清理旧备份
	if err := backupService.CleanOldBackups(); err != nil {
		logs.Error("Failed to clean old backups: %v", err)
		// 清理失败不影响主要功能，只记录日志
	}

	// 构建成功响应
	backupData := map[string]interface{}{
		"backup_path": backupPath,
		"timestamp":   time.Now().Unix(),
		"status":      "success",
	}

	data["code"] = 1
	data["message"] = "备份已成功创建并发送邮件"
	data["data"] = backupData
	s.Data["json"] = data
	s.ServeJSON()
}
