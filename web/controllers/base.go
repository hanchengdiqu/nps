/*
Package controllers 提供了NPS（内网穿透服务）Web管理界面的控制器实现。

本包包含了Web界面的基础控制器BaseController，它为所有其他控制器提供了通用功能，包括：
- 用户身份验证和权限控制
- 会话管理
- 模板渲染
- AJAX响应处理
- 参数获取和验证

BaseController是所有其他控制器的基类，确保了统一的安全性和功能性。
*/
package controllers

import (
	"html"
	"math"
	"strconv"
	"strings"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server"
	"github.com/astaxie/beego"
)

// BaseController 是所有Web控制器的基类，提供通用的功能和安全控制
// 
// 它继承自beego.Controller，并扩展了以下功能：
// - 统一的身份验证和权限控制
// - 会话管理和用户状态跟踪  
// - 模板渲染和数据传递
// - AJAX响应的标准化处理
// - 参数获取和类型转换的便捷方法
//
// 所有其他控制器都应该继承此基类以确保功能一致性和安全性
type BaseController struct {
	beego.Controller
	controllerName string // 当前控制器名称（小写，去掉Controller后缀）
	actionName     string // 当前动作名称（小写）
}

// Prepare 是beego框架的钩子方法，在每个请求处理前自动调用
// 
// 此方法负责：
// 1. 初始化控制器和动作名称
// 2. 执行身份验证和权限检查
// 3. 设置会话状态和用户权限
// 4. 加载配置参数到模板数据中
// 5. 为非管理员用户执行额外的权限验证
//
// 身份验证支持两种方式：
// - Web API验证：通过MD5(authKey+timestamp)进行验证，时间戳限制在20秒内
// - 会话验证：检查用户登录状态
func (s *BaseController) Prepare() {
	s.Data["web_base_url"] = beego.AppConfig.String("web_base_url")
	controllerName, actionName := s.GetControllerAndAction()
	s.controllerName = strings.ToLower(controllerName[0 : len(controllerName)-10])
	s.actionName = strings.ToLower(actionName)
	// web api verify
	// param 1 is md5(authKey+Current timestamp)
	// param 2 is timestamp (It's limited to 20 seconds.)
	md5Key := s.getEscapeString("auth_key")
	timestamp := s.GetIntNoErr("timestamp")
	configKey := beego.AppConfig.String("auth_key")
	timeNowUnix := time.Now().Unix()
	if !(md5Key != "" && (math.Abs(float64(timeNowUnix-int64(timestamp))) <= 20) && (crypt.Md5(configKey+strconv.Itoa(timestamp)) == md5Key)) {
		if s.GetSession("auth") != true {
			s.Redirect(beego.AppConfig.String("web_base_url")+"/login/index", 302)
		}
	} else {
		s.SetSession("isAdmin", true)
		s.Data["isAdmin"] = true
	}
	if s.GetSession("isAdmin") != nil && !s.GetSession("isAdmin").(bool) {
		s.Ctx.Input.SetData("client_id", s.GetSession("clientId").(int))
		s.Ctx.Input.SetParam("client_id", strconv.Itoa(s.GetSession("clientId").(int)))
		s.Data["isAdmin"] = false
		s.Data["username"] = s.GetSession("username")
		s.CheckUserAuth()
	} else {
		s.Data["isAdmin"] = true
	}
	s.Data["https_just_proxy"], _ = beego.AppConfig.Bool("https_just_proxy")
	s.Data["allow_user_login"], _ = beego.AppConfig.Bool("allow_user_login")
	s.Data["allow_flow_limit"], _ = beego.AppConfig.Bool("allow_flow_limit")
	s.Data["allow_rate_limit"], _ = beego.AppConfig.Bool("allow_rate_limit")
	s.Data["allow_connection_num_limit"], _ = beego.AppConfig.Bool("allow_connection_num_limit")
	s.Data["allow_multi_ip"], _ = beego.AppConfig.Bool("allow_multi_ip")
	s.Data["system_info_display"], _ = beego.AppConfig.Bool("system_info_display")
	s.Data["allow_tunnel_num_limit"], _ = beego.AppConfig.Bool("allow_tunnel_num_limit")
	s.Data["allow_local_proxy"], _ = beego.AppConfig.Bool("allow_local_proxy")
	s.Data["allow_user_change_username"], _ = beego.AppConfig.Bool("allow_user_change_username")
}

// display 负责渲染HTML模板并设置相关的模板数据
//
// 参数：
//   tpl - 可选的模板名称，如果不提供则使用默认的"控制器名/动作名.html"
//
// 此方法会：
// 1. 设置模板名称（支持自定义或默认命名规则）
// 2. 获取客户端IP地址
// 3. 设置系统相关的模板变量（如桥接类型、端口等）
// 4. 根据操作系统设置可执行文件后缀
// 5. 指定布局模板为"public/layout.html"
func (s *BaseController) display(tpl ...string) {
	s.Data["web_base_url"] = beego.AppConfig.String("web_base_url")
	var tplname string
	if s.Data["menu"] == nil {
		s.Data["menu"] = s.actionName
	}
	if len(tpl) > 0 {
		tplname = strings.Join([]string{tpl[0], "html"}, ".")
	} else {
		tplname = s.controllerName + "/" + s.actionName + ".html"
	}
	ip := s.Ctx.Request.Host
	s.Data["ip"] = common.GetIpByAddr(ip)
	s.Data["bridgeType"] = beego.AppConfig.String("bridge_type")
	if common.IsWindows() {
		s.Data["win"] = ".exe"
	}
	s.Data["p"] = server.Bridge.TunnelPort
	s.Data["proxyPort"] = beego.AppConfig.String("hostPort")
	s.Layout = "public/layout.html"
	s.TplName = tplname
}

// error 渲染错误页面
//
// 当发生错误时调用此方法，会显示统一的错误页面模板
// 使用"public/error.html"作为错误页面模板
func (s *BaseController) error() {
	s.Data["web_base_url"] = beego.AppConfig.String("web_base_url")
	s.Layout = "public/layout.html"
	s.TplName = "public/error.html"
}

// getEscapeString 获取请求参数并进行HTML转义
//
// 参数：
//   key - 请求参数的键名
//
// 返回：
//   经过HTML转义的参数值，防止XSS攻击
//
// 此方法用于安全地获取用户输入，自动转义HTML特殊字符
func (s *BaseController) getEscapeString(key string) string {
	return html.EscapeString(s.GetString(key))
}

// GetIntNoErr 获取整数类型的请求参数，忽略转换错误
//
// 参数：
//   key - 请求参数的键名
//   def - 可选的默认值，当参数不存在时返回此值
//
// 返回：
//   转换后的整数值，转换失败时返回0或默认值
//
// 此方法简化了整数参数的获取，避免了错误处理的复杂性
func (s *BaseController) GetIntNoErr(key string, def ...int) int {
	strv := s.Ctx.Input.Query(key)
	if len(strv) == 0 && len(def) > 0 {
		return def[0]
	}
	val, _ := strconv.Atoi(strv)
	return val
}

// GetBoolNoErr 获取布尔类型的请求参数，忽略转换错误
//
// 参数：
//   key - 请求参数的键名
//   def - 可选的默认值，当参数不存在时返回此值
//
// 返回：
//   转换后的布尔值，转换失败时返回false或默认值
//
// 此方法简化了布尔参数的获取，避免了错误处理的复杂性
func (s *BaseController) GetBoolNoErr(key string, def ...bool) bool {
	strv := s.Ctx.Input.Query(key)
	if len(strv) == 0 && len(def) > 0 {
		return def[0]
	}
	val, _ := strconv.ParseBool(strv)
	return val
}

// AjaxOk 返回AJAX成功响应
//
// 参数：
//   str - 成功消息内容
//
// 此方法会：
// 1. 构造成功状态的JSON响应（status=1）
// 2. 设置响应内容并立即返回
// 3. 停止后续处理流程
func (s *BaseController) AjaxOk(str string) {
	s.Data["json"] = ajax(str, 1)
	s.ServeJSON()
	s.StopRun()
}

// AjaxErr 返回AJAX错误响应
//
// 参数：
//   str - 错误消息内容
//
// 此方法会：
// 1. 构造错误状态的JSON响应（status=0）
// 2. 设置响应内容并立即返回
// 3. 停止后续处理流程
func (s *BaseController) AjaxErr(str string) {
	s.Data["json"] = ajax(str, 0)
	s.ServeJSON()
	s.StopRun()
}

// ajax 构造标准的AJAX响应格式
//
// 参数：
//   str - 响应消息内容
//   status - 响应状态（1表示成功，0表示失败）
//
// 返回：
//   包含status和msg字段的map，用于JSON序列化
//
// 此函数定义了系统统一的AJAX响应格式
func ajax(str string, status int) map[string]interface{} {
	json := make(map[string]interface{})
	json["status"] = status
	json["msg"] = str
	return json
}

// AjaxTable 返回表格数据的AJAX响应
//
// 参数：
//   list - 表格行数据列表
//   cnt - 当前页记录数（未使用）
//   recordsTotal - 总记录数
//   kwargs - 额外的响应字段
//
// 此方法专门用于数据表格的AJAX响应，返回包含rows和total字段的JSON
// 支持通过kwargs添加额外的响应字段
func (s *BaseController) AjaxTable(list interface{}, cnt int, recordsTotal int, kwargs map[string]interface{}) {
	json := make(map[string]interface{})
	json["rows"] = list
	json["total"] = recordsTotal
	if kwargs != nil {
		for k, v := range kwargs {
			if v != nil {
				json[k] = v
			}
		}
	}
	s.Data["json"] = json
	s.ServeJSON()
	s.StopRun()
}

// GetAjaxParams 获取AJAX表格的分页参数
//
// 返回：
//   start - 起始偏移量（offset参数）
//   limit - 每页记录数（limit参数）
//
// 此方法用于获取数据表格分页所需的参数
func (s *BaseController) GetAjaxParams() (start, limit int) {
	return s.GetIntNoErr("offset"), s.GetIntNoErr("limit")
}

// SetInfo 设置模板中的name变量
//
// 参数：
//   name - 要设置的名称值
//
// 此方法用于向模板传递name数据
func (s *BaseController) SetInfo(name string) {
	s.Data["name"] = name
}

// SetType 设置模板中的type变量
//
// 参数：
//   name - 要设置的类型值
//
// 此方法用于向模板传递type数据
func (s *BaseController) SetType(name string) {
	s.Data["type"] = name
}

// CheckUserAuth 检查非管理员用户的权限
//
// 此方法对非管理员用户进行细粒度的权限控制：
//
// 1. 对于client控制器：
//    - 禁止访问add动作（添加客户端）
//    - 只允许访问自己的客户端记录
//
// 2. 对于index控制器：
//    - 检查主机(host)和隧道(tunnel)的所有权
//    - 确保用户只能访问属于自己客户端的资源
//    - 通过动作名称中是否包含"h"来区分主机和隧道操作
//
// 如果权限检查失败，会立即停止请求处理
func (s *BaseController) CheckUserAuth() {
	if s.controllerName == "client" {
		if s.actionName == "add" {
			s.StopRun()
			return
		}
		if id := s.GetIntNoErr("id"); id != 0 {
			if id != s.GetSession("clientId").(int) {
				s.StopRun()
				return
			}
		}
	}
	if s.controllerName == "index" {
		if id := s.GetIntNoErr("id"); id != 0 {
			belong := false
			if strings.Contains(s.actionName, "h") {
				if v, ok := file.GetDb().JsonDb.Hosts.Load(id); ok {
					if v.(*file.Host).Client.Id == s.GetSession("clientId").(int) {
						belong = true
					}
				}
			} else {
				if v, ok := file.GetDb().JsonDb.Tasks.Load(id); ok {
					if v.(*file.Tunnel).Client.Id == s.GetSession("clientId").(int) {
						belong = true
					}
				}
			}
			if !belong {
				s.StopRun()
			}
		}
	}
}
