package controllers

import (
	"ehang.io/nps/server"
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