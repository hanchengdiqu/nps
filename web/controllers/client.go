// Package controllers 包含NPS Web管理界面的控制器
// 本文件实现客户端管理相关的Web控制器功能
package controllers

import (
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/rate"
	"ehang.io/nps/server"
	"github.com/astaxie/beego"
)

// ClientController 客户端管理控制器
// 负责处理NPS服务端Web界面中客户端的增删改查操作
// 包括客户端列表显示、添加、编辑、删除、状态变更等功能
type ClientController struct {
	BaseController
}

// List 客户端列表页面处理
// GET请求：显示客户端列表页面
// POST请求：返回客户端列表的Ajax数据，支持分页、搜索、排序
// 同时返回服务器连接信息（IP、桥接类型、端口等）供客户端连接使用
func (s *ClientController) List() {
	if s.Ctx.Request.Method == "GET" {
		// GET请求：显示客户端列表页面
		s.Data["menu"] = "client"
		s.SetInfo("client")
		s.display("client/list")
		return
	}
	
	// POST请求：处理Ajax数据请求
	start, length := s.GetAjaxParams() // 获取分页参数
	
	// 获取当前会话的客户端ID（用于权限控制）
	clientIdSession := s.GetSession("clientId")
	var clientId int
	if clientIdSession == nil {
		clientId = 0 // 管理员账户，可查看所有客户端
	} else {
		clientId = clientIdSession.(int) // 普通用户，只能查看自己的客户端
	}
	
	// 获取客户端列表数据，支持搜索、排序
	list, cnt := server.GetClientList(start, length, s.getEscapeString("search"), s.getEscapeString("sort"), s.getEscapeString("order"), clientId)
	
	// 构建客户端连接命令信息
	cmd := make(map[string]interface{})
	ip := s.Ctx.Request.Host
	cmd["ip"] = common.GetIpByAddr(ip)                    // 服务器IP地址
	cmd["bridgeType"] = beego.AppConfig.String("bridge_type") // 桥接类型
	cmd["bridgePort"] = server.Bridge.TunnelPort              // 桥接端口
	
	// 返回Ajax表格数据
	s.AjaxTable(list, cnt, cnt, cmd)
}

// Add 添加客户端
// GET请求：显示添加客户端的表单页面
// POST请求：处理添加客户端的表单提交，创建新的客户端配置
func (s *ClientController) Add() {
	if s.Ctx.Request.Method == "GET" {
		// GET请求：显示添加客户端表单页面
		s.Data["menu"] = "client"
		s.SetInfo("add client")
		s.display()
	} else {
		// POST请求：处理表单提交，创建新客户端
		t := &file.Client{
			VerifyKey: s.getEscapeString("vkey"),           // 客户端验证密钥
			Id:        int(file.GetDb().JsonDb.GetClientId()), // 自动生成客户端ID
			Status:    true,                                // 默认启用状态
			Remark:    s.getEscapeString("remark"),         // 客户端备注
			Cnf: &file.Config{
				U:        s.getEscapeString("u"),                                 // 用户名
				P:        s.getEscapeString("p"),                                 // 密码
				Compress: common.GetBoolByStr(s.getEscapeString("compress")),     // 是否启用压缩
				Crypt:    s.GetBoolNoErr("crypt"),                                // 是否启用加密
			},
			ConfigConnAllow: s.GetBoolNoErr("config_conn_allow"), // 是否允许配置连接
			RateLimit:       s.GetIntNoErr("rate_limit"),         // 速率限制(KB/s)
			MaxConn:         s.GetIntNoErr("max_conn"),           // 最大连接数
			WebUserName:     s.getEscapeString("web_username"),   // Web登录用户名
			WebPassword:     s.getEscapeString("web_password"),   // Web登录密码
			MaxTunnelNum:    s.GetIntNoErr("max_tunnel"),         // 最大隧道数
			Flow: &file.Flow{
				ExportFlow: 0,                                    // 出口流量（初始为0）
				InletFlow:  0,                                    // 入口流量（初始为0）
				FlowLimit:  int64(s.GetIntNoErr("flow_limit")),   // 流量限制
			},
		}
		
		// 保存新客户端到数据库
		if err := file.GetDb().NewClient(t); err != nil {
			s.AjaxErr(err.Error())
			return
		}
		s.AjaxOk("add success")
	}
}
// GetClient 获取单个客户端信息
// POST请求：根据客户端ID获取客户端详细信息，用于编辑表单的数据回显
// 返回JSON格式的客户端数据
func (s *ClientController) GetClient() {
	if s.Ctx.Request.Method == "POST" {
		id := s.GetIntNoErr("id") // 获取客户端ID
		data := make(map[string]interface{})
		
		// 从数据库获取客户端信息
		if c, err := file.GetDb().GetClient(id); err != nil {
			data["code"] = 0 // 获取失败
		} else {
			data["code"] = 1 // 获取成功
			data["data"] = c // 客户端数据
		}
		
		// 返回JSON响应
		s.Data["json"] = data
		s.ServeJSON()
	}
}

// Edit 编辑客户端
// GET请求：显示编辑客户端的表单页面，预填充现有数据
// POST请求：处理编辑客户端的表单提交，更新客户端配置
// 包含权限验证、用户名重复检查、验证密钥重复检查等安全机制
func (s *ClientController) Edit() {
	id := s.GetIntNoErr("id") // 获取要编辑的客户端ID
	
	if s.Ctx.Request.Method == "GET" {
		// GET请求：显示编辑表单页面
		s.Data["menu"] = "client"
		if c, err := file.GetDb().GetClient(id); err != nil {
			s.error() // 客户端不存在，显示错误页面
		} else {
			s.Data["c"] = c // 将客户端数据传递给模板
		}
		s.SetInfo("edit client")
		s.display()
	} else {
		// POST请求：处理表单提交，更新客户端配置
		if c, err := file.GetDb().GetClient(id); err != nil {
			s.error()
			s.AjaxErr("client ID not found")
			return
		} else {
			// 检查Web登录用户名是否重复
			if s.getEscapeString("web_username") != "" {
				if s.getEscapeString("web_username") == beego.AppConfig.String("web_username") || !file.GetDb().VerifyUserName(s.getEscapeString("web_username"), c.Id) {
					s.AjaxErr("web login username duplicate, please reset")
					return
				}
			}
			
			// 管理员权限检查和特权操作
			if s.GetSession("isAdmin").(bool) {
				// 检查验证密钥是否重复
				if !file.GetDb().VerifyVkey(s.getEscapeString("vkey"), c.Id) {
					s.AjaxErr("Vkey duplicate, please reset")
					return
				}
				// 管理员可以修改的高级配置
				c.VerifyKey = s.getEscapeString("vkey")                   // 验证密钥
				c.Flow.FlowLimit = int64(s.GetIntNoErr("flow_limit"))     // 流量限制
				c.RateLimit = s.GetIntNoErr("rate_limit")                 // 速率限制
				c.MaxConn = s.GetIntNoErr("max_conn")                     // 最大连接数
				c.MaxTunnelNum = s.GetIntNoErr("max_tunnel")              // 最大隧道数
			}
			
			// 所有用户都可以修改的基本配置
			c.Remark = s.getEscapeString("remark")                                   // 备注
			c.Cnf.U = s.getEscapeString("u")                                         // 用户名
			c.Cnf.P = s.getEscapeString("p")                                         // 密码
			c.Cnf.Compress = common.GetBoolByStr(s.getEscapeString("compress"))     // 压缩设置
			c.Cnf.Crypt = s.GetBoolNoErr("crypt")                                    // 加密设置
			
			// 检查是否允许用户修改Web登录用户名
			b, err := beego.AppConfig.Bool("allow_user_change_username")
			if s.GetSession("isAdmin").(bool) || (err == nil && b) {
				c.WebUserName = s.getEscapeString("web_username") // Web登录用户名
			}
			c.WebPassword = s.getEscapeString("web_password")     // Web登录密码
			c.ConfigConnAllow = s.GetBoolNoErr("config_conn_allow") // 配置连接允许
			
			// 更新速率限制器
			if c.Rate != nil {
				c.Rate.Stop() // 停止旧的速率限制器
			}
			if c.RateLimit > 0 {
				// 创建新的速率限制器（转换为字节/秒）
				c.Rate = rate.NewRate(int64(c.RateLimit * 1024))
				c.Rate.Start()
			} else {
				// 默认速率限制（16MB/s）
				c.Rate = rate.NewRate(int64(2 << 23))
				c.Rate.Start()
			}
			
			// 保存配置到文件
			file.GetDb().JsonDb.StoreClientsToJsonFile()
		}
		s.AjaxOk("save success")
	}
}

// ChangeStatus 更改客户端状态
// 用于启用或禁用客户端，当禁用客户端时会断开其所有连接
func (s *ClientController) ChangeStatus() {
	id := s.GetIntNoErr("id") // 获取客户端ID
	
	if client, err := file.GetDb().GetClient(id); err == nil {
		client.Status = s.GetBoolNoErr("status") // 更新客户端状态
		
		// 如果禁用客户端，断开其所有连接
		if client.Status == false {
			server.DelClientConnect(client.Id)
		}
		s.AjaxOk("modified success")
		return
	}
	s.AjaxErr("modified fail")
}

// Del 删除客户端
// 删除客户端及其相关的所有隧道、主机配置和连接
// 这是一个彻底的清理操作，会移除客户端的所有相关数据
func (s *ClientController) Del() {
	id := s.GetIntNoErr("id") // 获取要删除的客户端ID
	
	// 从数据库删除客户端记录
	if err := file.GetDb().DelClient(id); err != nil {
		s.AjaxErr("delete error")
		return
	}
	
	// 删除客户端相关的隧道和主机配置
	server.DelTunnelAndHostByClientId(id, false)
	
	// 断开客户端的所有连接
	server.DelClientConnect(id)
	
	s.AjaxOk("delete success")
}
