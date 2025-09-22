// Package controllers 提供NPS（内网穿透服务）Web管理界面的控制器实现
// 本文件包含主要的隧道管理和主机管理功能的HTTP处理器
package controllers

import (
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server"
	"ehang.io/nps/server/tool"

	"github.com/astaxie/beego"
)

// IndexController 主控制器，负责处理NPS系统的核心功能
// 包括隧道管理（TCP、UDP、HTTP代理、SOCKS5等）和主机管理
type IndexController struct {
	BaseController
}

// Index 显示系统仪表板页面
// 获取系统运行状态数据并渲染主页面
// URL: GET /
func (s *IndexController) Index() {
	s.Data["web_base_url"] = beego.AppConfig.String("web_base_url")
	s.Data["data"] = server.GetDashboardData()
	s.SetInfo("dashboard")
	s.display("index/index")
}

// Help 显示帮助页面
// 提供系统使用说明和相关文档
// URL: GET /index/help
func (s *IndexController) Help() {
	s.SetInfo("about")
	s.display("index/help")
}

// Tcp 显示TCP隧道管理页面
// 列出所有TCP类型的隧道配置
// URL: GET /index/tcp
func (s *IndexController) Tcp() {
	s.SetInfo("tcp")
	s.SetType("tcp")
	s.display("index/list")
}

// Udp 显示UDP隧道管理页面
// 列出所有UDP类型的隧道配置
// URL: GET /index/udp
func (s *IndexController) Udp() {
	s.SetInfo("udp")
	s.SetType("udp")
	s.display("index/list")
}

// Socks5 显示SOCKS5代理管理页面
// 列出所有SOCKS5类型的代理配置
// URL: GET /index/socks5
func (s *IndexController) Socks5() {
	s.SetInfo("socks5")
	s.SetType("socks5")
	s.display("index/list")
}

// Http 显示HTTP代理管理页面
// 列出所有HTTP代理类型的隧道配置
// URL: GET /index/http
func (s *IndexController) Http() {
	s.SetInfo("http proxy")
	s.SetType("httpProxy")
	s.display("index/list")
}

// File 显示文件服务器管理页面
// 列出所有文件服务器类型的隧道配置
// URL: GET /index/file
func (s *IndexController) File() {
	s.SetInfo("file server")
	s.SetType("file")
	s.display("index/list")
}

// Secret 显示加密隧道管理页面
// 列出所有加密类型的隧道配置
// URL: GET /index/secret
func (s *IndexController) Secret() {
	s.SetInfo("secret")
	s.SetType("secret")
	s.display("index/list")
}

// P2p 显示P2P连接管理页面
// 列出所有点对点连接类型的隧道配置
// URL: GET /index/p2p
func (s *IndexController) P2p() {
	s.SetInfo("p2p")
	s.SetType("p2p")
	s.display("index/list")
}

// Host 显示主机服务器管理页面
// 列出所有主机服务器类型的配置
// URL: GET /index/host
func (s *IndexController) Host() {
	s.SetInfo("host")
	s.SetType("hostServer")
	s.display("index/list")
}

// All 显示指定客户端的所有隧道
// 根据客户端ID显示该客户端下的所有隧道配置
// URL: GET /index/all?client_id=xxx
func (s *IndexController) All() {
	s.Data["menu"] = "client"
	clientId := s.getEscapeString("client_id")
	s.Data["client_id"] = clientId
	s.SetInfo("client id:" + clientId)
	s.display("index/list")
}

// GetTunnel 获取隧道列表数据（AJAX接口）
// 支持分页、按类型筛选、按客户端筛选和搜索功能
// 返回JSON格式的隧道列表数据供前端表格显示
// URL: POST /index/gettunnel
// 参数:
//   - client_id: 穿透隧道的客户端id
//   - type: 类型tcp udp httpProx socks5 secret p2p
//   - search: 搜索
//   - offset: 分页(第几页)
//   - limit: 条数(分页显示的条数)
func (s *IndexController) GetTunnel() {
	start, length := s.GetAjaxParams()
	taskType := s.getEscapeString("type")
	clientId := s.GetIntNoErr("client_id")
	list, cnt := server.GetTunnel(start, length, taskType, clientId, s.getEscapeString("search"))
	s.AjaxTable(list, cnt, cnt, nil)
}

// Add 添加新隧道
// GET请求：显示添加隧道的表单页面
// POST请求：处理隧道创建逻辑，包括端口检测、客户端验证、隧道数量限制等
// URL: GET/POST /index/add
// POST参数:
//   - type: 类型tcp udp httpProx socks5 secret p2p
//   - remark: 备注
//   - port: 服务端端口
//   - target: 目标(ip:端口)
//   - client_id: 客户端id
func (s *IndexController) Add() {
	if s.Ctx.Request.Method == "GET" {
		s.Data["type"] = s.getEscapeString("type")
		s.Data["client_id"] = s.getEscapeString("client_id")
		s.SetInfo("add tunnel")
		s.display()
	} else {
		t := &file.Tunnel{
			Port:      s.GetIntNoErr("port"),
			ServerIp:  s.getEscapeString("server_ip"),
			Mode:      s.getEscapeString("type"),
			Target:    &file.Target{TargetStr: s.getEscapeString("target"), LocalProxy: s.GetBoolNoErr("local_proxy")},
			Id:        int(file.GetDb().JsonDb.GetTaskId()),
			Status:    true,
			Remark:    s.getEscapeString("remark"),
			Password:  s.getEscapeString("password"),
			LocalPath: s.getEscapeString("local_path"),
			StripPre:  s.getEscapeString("strip_pre"),
			Flow:      &file.Flow{},
		}
		if !tool.TestServerPort(t.Port, t.Mode) {
			s.AjaxErr("The port cannot be opened because it may has been occupied or is no longer allowed.")
		}
		var err error
		if t.Client, err = file.GetDb().GetClient(s.GetIntNoErr("client_id")); err != nil {
			s.AjaxErr(err.Error())
		}
		if t.Client.MaxTunnelNum != 0 && t.Client.GetTunnelNum() >= t.Client.MaxTunnelNum {
			s.AjaxErr("The number of tunnels exceeds the limit")
		}
		if err := file.GetDb().NewTask(t); err != nil {
			s.AjaxErr(err.Error())
		}
		if err := server.AddTask(t); err != nil {
			s.AjaxErr(err.Error())
		} else {
			s.AjaxOk("add success")
		}
	}
}

// GetOneTunnel 获取单个隧道详情（AJAX接口）
// 根据隧道ID返回隧道的详细配置信息
// 返回JSON格式数据，包含状态码和隧道数据
// URL: POST /index/getonetunnel
// 参数:
//   - id: 隧道的id
func (s *IndexController) GetOneTunnel() {
	id := s.GetIntNoErr("id")
	data := make(map[string]interface{})
	if t, err := file.GetDb().GetTask(id); err != nil {
		data["code"] = 0
	} else {
		data["code"] = 1
		data["data"] = t
	}
	s.Data["json"] = data
	s.ServeJSON()
}

// Edit 编辑隧道配置
// GET请求：显示编辑隧道的表单页面，预填充现有配置
// POST请求：处理隧道更新逻辑，包括端口变更检测、配置更新、服务重启等
// URL: GET/POST /index/edit
// POST参数:
//   - type: 类型tcp udp httpProx socks5 secret p2p
//   - remark: 备注
//   - port: 服务端端口
//   - target: 目标(ip:端口)
//   - client_id: 客户端id
//   - id: 隧道id
func (s *IndexController) Edit() {
	id := s.GetIntNoErr("id")
	if s.Ctx.Request.Method == "GET" {
		if t, err := file.GetDb().GetTask(id); err != nil {
			s.error()
		} else {
			s.Data["t"] = t
		}
		s.SetInfo("edit tunnel")
		s.display()
	} else {
		if t, err := file.GetDb().GetTask(id); err != nil {
			s.error()
		} else {
			if client, err := file.GetDb().GetClient(s.GetIntNoErr("client_id")); err != nil {
				s.AjaxErr("modified error,the client is not exist")
				return
			} else {
				t.Client = client
			}
			if s.GetIntNoErr("port") != t.Port {
				if !tool.TestServerPort(s.GetIntNoErr("port"), t.Mode) {
					s.AjaxErr("The port cannot be opened because it may has been occupied or is no longer allowed.")
					return
				}
				t.Port = s.GetIntNoErr("port")
			}
			t.ServerIp = s.getEscapeString("server_ip")
			t.Mode = s.getEscapeString("type")
			t.Target = &file.Target{TargetStr: s.getEscapeString("target")}
			t.Password = s.getEscapeString("password")
			t.Id = id
			t.LocalPath = s.getEscapeString("local_path")
			t.StripPre = s.getEscapeString("strip_pre")
			t.Remark = s.getEscapeString("remark")
			t.Target.LocalProxy = s.GetBoolNoErr("local_proxy")
			file.GetDb().UpdateTask(t)
			server.StopServer(t.Id)
			server.StartTask(t.Id)
		}
		s.AjaxOk("modified success")
	}
}

// Stop 停止指定隧道服务
// 根据隧道ID停止对应的隧道服务，但保留配置信息
// URL: POST /index/stop
// 参数:
//   - id: 隧道id
func (s *IndexController) Stop() {
	id := s.GetIntNoErr("id")
	if err := server.StopServer(id); err != nil {
		s.AjaxErr("stop error")
	}
	s.AjaxOk("stop success")
}

// Del 删除指定隧道
// 根据隧道ID彻底删除隧道配置和服务
// URL: POST /index/del
// 参数:
//   - id: 隧道id
func (s *IndexController) Del() {
	id := s.GetIntNoErr("id")
	if err := server.DelTask(id); err != nil {
		s.AjaxErr("delete error")
	}
	s.AjaxOk("delete success")
}

// Start 启动指定隧道服务
// 根据隧道ID启动对应的隧道服务
// URL: POST /index/start
// 参数:
//   - id: 隧道id
func (s *IndexController) Start() {
	id := s.GetIntNoErr("id")
	if err := server.StartTask(id); err != nil {
		s.AjaxErr("start error")
	}
	s.AjaxOk("start success")
}

// HostList 主机列表管理
// GET请求：显示主机列表页面
// POST请求：返回主机列表数据（AJAX接口），支持分页、按客户端筛选和搜索
// URL: GET/POST /index/hostlist
// POST参数:
//   - search: 搜索(可以搜域名/备注什么的)
//   - offset: 分页(第几页)
//   - limit: 条数(分页显示的条数)
func (s *IndexController) HostList() {
	if s.Ctx.Request.Method == "GET" {
		s.Data["client_id"] = s.getEscapeString("client_id")
		s.Data["menu"] = "host"
		s.SetInfo("host list")
		s.display("index/hlist")
	} else {
		start, length := s.GetAjaxParams()
		clientId := s.GetIntNoErr("client_id")
		list, cnt := file.GetDb().GetHost(start, length, clientId, s.getEscapeString("search"))
		s.AjaxTable(list, cnt, cnt, nil)
	}
}

// GetHost 获取单个主机详情（AJAX接口）
// 根据主机ID返回主机的详细配置信息
// 返回JSON格式数据，包含状态码和主机数据
// URL: POST /index/gethost
// 参数:
//   - id: 主机id
func (s *IndexController) GetHost() {
	if s.Ctx.Request.Method == "POST" {
		data := make(map[string]interface{})
		if h, err := file.GetDb().GetHostById(s.GetIntNoErr("id")); err != nil {
			data["code"] = 0
		} else {
			data["data"] = h
			data["code"] = 1
		}
		s.Data["json"] = data
		s.ServeJSON()
	}
}

// DelHost 删除指定主机配置
// 根据主机ID删除主机配置信息
// URL: POST /index/delhost
// 参数:
//   - id: 需要删除的域名解析id
func (s *IndexController) DelHost() {
	id := s.GetIntNoErr("id")
	if err := file.GetDb().DelHost(id); err != nil {
		s.AjaxErr("delete error")
	}
	s.AjaxOk("delete success")
}

// AddHost 添加新主机配置
// GET请求：显示添加主机的表单页面
// POST请求：处理主机创建逻辑，包括主机域名、目标地址、SSL证书等配置
// URL: GET/POST /index/addhost
// POST参数:
//   - remark: 备注
//   - host: 域名
//   - scheme: 协议类型(三种 all http https)
//   - location: url路由 空则为不限制
//   - client_id: 客户端id
//   - target: 内网目标(ip:端口)
//   - header: request header 请求头
//   - hostchange: request host 请求主机
func (s *IndexController) AddHost() {
	if s.Ctx.Request.Method == "GET" {
		s.Data["client_id"] = s.getEscapeString("client_id")
		s.Data["menu"] = "host"
		s.SetInfo("add host")
		s.display("index/hadd")
	} else {
		h := &file.Host{
			Id:           int(file.GetDb().JsonDb.GetHostId()),
			Host:         s.getEscapeString("host"),
			Target:       &file.Target{TargetStr: s.getEscapeString("target"), LocalProxy: s.GetBoolNoErr("local_proxy")},
			HeaderChange: s.getEscapeString("header"),
			HostChange:   s.getEscapeString("hostchange"),
			Remark:       s.getEscapeString("remark"),
			Location:     s.getEscapeString("location"),
			Flow:         &file.Flow{},
			Scheme:       s.getEscapeString("scheme"),
			KeyFilePath:  s.getEscapeString("key_file_path"),
			CertFilePath: s.getEscapeString("cert_file_path"),
		}
		var err error
		if h.Client, err = file.GetDb().GetClient(s.GetIntNoErr("client_id")); err != nil {
			s.AjaxErr("add error the client can not be found")
		}
		if err := file.GetDb().NewHost(h); err != nil {
			s.AjaxErr("add fail" + err.Error())
		}
		s.AjaxOk("add success")
	}
}

// EditHost 编辑主机配置
// GET请求：显示编辑主机的表单页面，预填充现有配置
// POST请求：处理主机更新逻辑，包括域名重复检测、配置更新等
// URL: GET/POST /index/edithost
// POST参数:
//   - remark: 备注
//   - host: 域名
//   - scheme: 协议类型(三种 all http https)
//   - location: url路由 空则为不限制
//   - client_id: 客户端id
//   - target: 内网目标(ip:端口)
//   - header: request header 请求头
//   - hostchange: request host 请求主机
//   - id: 需要修改的域名解析id
func (s *IndexController) EditHost() {
	id := s.GetIntNoErr("id")
	if s.Ctx.Request.Method == "GET" {
		s.Data["menu"] = "host"
		if h, err := file.GetDb().GetHostById(id); err != nil {
			s.error()
		} else {
			s.Data["h"] = h
		}
		s.SetInfo("edit")
		s.display("index/hedit")
	} else {
		if h, err := file.GetDb().GetHostById(id); err != nil {
			s.error()
		} else {
			if h.Host != s.getEscapeString("host") {
				tmpHost := new(file.Host)
				tmpHost.Host = s.getEscapeString("host")
				tmpHost.Location = s.getEscapeString("location")
				tmpHost.Scheme = s.getEscapeString("scheme")
				if file.GetDb().IsHostExist(tmpHost) {
					s.AjaxErr("host has exist")
					return
				}
			}
			if client, err := file.GetDb().GetClient(s.GetIntNoErr("client_id")); err != nil {
				s.AjaxErr("modified error,the client is not exist")
			} else {
				h.Client = client
			}
			h.Host = s.getEscapeString("host")
			h.Target = &file.Target{TargetStr: s.getEscapeString("target")}
			h.HeaderChange = s.getEscapeString("header")
			h.HostChange = s.getEscapeString("hostchange")
			h.Remark = s.getEscapeString("remark")
			h.Location = s.getEscapeString("location")
			h.Scheme = s.getEscapeString("scheme")
			h.KeyFilePath = s.getEscapeString("key_file_path")
			h.CertFilePath = s.getEscapeString("cert_file_path")
			h.Target.LocalProxy = s.GetBoolNoErr("local_proxy")
			file.GetDb().JsonDb.StoreHostToJsonFile()
		}
		s.AjaxOk("modified success")
	}
}
