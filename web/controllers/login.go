// Package controllers 包含NPS Web管理界面的控制器
// 本文件实现了用户登录、注册、登出等认证相关功能
package controllers

import (
	"math/rand"  // 用于生成随机数，清理IP记录时使用
	"net"        // 用于网络地址解析
	"sync"       // 提供并发安全的数据结构
	"time"       // 时间相关操作

	"ehang.io/nps/lib/common" // NPS通用工具库
	"ehang.io/nps/lib/file"   // NPS文件和数据库操作库
	"ehang.io/nps/server"     // NPS服务器核心库
	"github.com/astaxie/beego" // Beego Web框架
)

// LoginController 登录控制器
// 继承自beego.Controller，处理用户登录、注册、登出等认证相关请求
type LoginController struct {
	beego.Controller
}

// ipRecord 全局IP记录映射表，用于记录每个IP的登录失败次数和最后登录时间
// 使用sync.Map确保并发安全
var ipRecord sync.Map

// record 记录结构体，存储单个IP的登录相关信息
type record struct {
	hasLoginFailTimes int       // 登录失败次数
	lastLoginTime     time.Time // 最后一次登录尝试时间
}

// Index 登录页面首页处理方法
// 功能说明：
// 1. 尝试隐式登录（当配置为无认证模式时，即用户名和密码为空时自动登录）
// 2. 如果隐式登录成功，直接重定向到主页面
// 3. 如果隐式登录失败，显示登录页面
// 4. 设置模板变量：web基础URL和是否允许用户注册
func (self *LoginController) Index() {
	// 尝试隐式登录，当配置为无认证模式时（用户名和密码为空）会成功
	webBaseUrl := beego.AppConfig.String("web_base_url")
	if self.doLogin("", "", false) {
		// 隐式登录成功，重定向到主页面
		self.Redirect(webBaseUrl+"/index/index", 302)
	}
	// 设置模板变量
	self.Data["web_base_url"] = webBaseUrl                                        // Web基础URL
	self.Data["register_allow"], _ = beego.AppConfig.Bool("allow_user_register") // 是否允许用户注册
	self.TplName = "login/index.html"                                            // 指定登录页面模板
}

// Verify 用户登录验证方法
// 功能说明：
// 1. 接收前端提交的用户名和密码
// 2. 调用doLogin方法进行登录验证
// 3. 返回JSON格式的登录结果（成功或失败）
// 请求方式：POST
// 参数：username（用户名）、password（密码）
// 返回：JSON格式的状态和消息
func (self *LoginController) Verify() {
	// 获取前端提交的用户名和密码
	username := self.GetString("username")
	password := self.GetString("password")
	
	// 执行登录验证（explicit=true表示显式登录）
	if self.doLogin(username, password, true) {
		// 登录成功，返回成功状态
		self.Data["json"] = map[string]interface{}{"status": 1, "msg": "login success"}
	} else {
		// 登录失败，返回失败状态
		self.Data["json"] = map[string]interface{}{"status": 0, "msg": "username or password incorrect"}
	}
	// 以JSON格式返回结果
	self.ServeJSON()
}

// doLogin 执行登录验证的核心方法
// 功能说明：
// 1. 检查IP登录失败次数限制（防暴力破解）
// 2. 验证管理员账户（配置文件中的web_username和web_password）
// 3. 验证普通用户账户（数据库中的客户端用户）
// 4. 设置相应的会话信息
// 5. 记录登录失败次数
// 参数：
//   - username: 用户名
//   - password: 密码
//   - explicit: 是否为显式登录（true表示用户主动登录，false表示隐式登录）
// 返回值：bool - 登录是否成功
func (self *LoginController) doLogin(username, password string, explicit bool) bool {
	// 随机清理过期的IP记录
	clearIprecord()
	
	// 获取客户端IP地址
	ip, _, _ := net.SplitHostPort(self.Ctx.Request.RemoteAddr)
	
	// 检查IP登录失败次数限制
	if v, ok := ipRecord.Load(ip); ok {
		vv := v.(*record)
		// 如果距离上次登录超过60秒，重置失败次数
		if (time.Now().Unix() - vv.lastLoginTime.Unix()) >= 60 {
			vv.hasLoginFailTimes = 0
		}
		// 如果失败次数超过10次，拒绝登录
		if vv.hasLoginFailTimes >= 10 {
			return false
		}
	}
	
	var auth bool // 认证状态标志
	
	// 第一步：验证管理员账户
	if password == beego.AppConfig.String("web_password") && username == beego.AppConfig.String("web_username") {
		// 管理员登录成功，设置管理员会话
		self.SetSession("isAdmin", true)   // 标记为管理员
		self.DelSession("clientId")        // 清除客户端ID
		self.DelSession("username")        // 清除用户名
		auth = true
		// 将管理员IP注册到服务器桥接器，有效期2小时
		server.Bridge.Register.Store(common.GetIpByAddr(self.Ctx.Input.IP()), time.Now().Add(time.Hour*time.Duration(2)))
	}
	
	// 第二步：如果不是管理员且允许用户登录，验证普通用户账户
	b, err := beego.AppConfig.Bool("allow_user_login")
	if err == nil && b && !auth {
		// 遍历所有客户端，查找匹配的用户
		file.GetDb().JsonDb.Clients.Range(func(key, value interface{}) bool {
			v := value.(*file.Client)
			// 跳过未启用或隐藏的客户端
			if !v.Status || v.NoDisplay {
				return true
			}
			
			// 处理没有设置Web用户名和密码的客户端（使用VerifyKey验证）
			if v.WebUserName == "" && v.WebPassword == "" {
				if username != "user" || v.VerifyKey != password {
					return true // 继续遍历
				} else {
					auth = true // 验证成功
				}
			}
			
			// 处理设置了Web用户名和密码的客户端
			if !auth && v.WebPassword == password && v.WebUserName == username {
				auth = true
			}
			
			// 如果认证成功，设置普通用户会话
			if auth {
				self.SetSession("isAdmin", false)      // 标记为普通用户
				self.SetSession("clientId", v.Id)      // 设置客户端ID
				self.SetSession("username", v.WebUserName) // 设置用户名
				return false // 停止遍历
			}
			return true // 继续遍历
		})
	}
	
	// 第三步：处理认证结果
	if auth {
		// 认证成功
		self.SetSession("auth", true) // 设置认证状态
		ipRecord.Delete(ip)           // 清除该IP的失败记录
		return true
	}
	
	// 第四步：认证失败，记录失败次数（仅对显式登录）
	if v, load := ipRecord.LoadOrStore(ip, &record{hasLoginFailTimes: 1, lastLoginTime: time.Now()}); load && explicit {
		// IP记录已存在，增加失败次数
		vv := v.(*record)
		vv.lastLoginTime = time.Now()
		vv.hasLoginFailTimes += 1
		ipRecord.Store(ip, vv)
	}
	return false
}
// Register 用户注册方法
// 功能说明：
// 1. GET请求：显示注册页面
// 2. POST请求：处理用户注册逻辑
//    - 检查是否允许用户注册
//    - 验证用户输入（用户名、密码不能为空，用户名不能与管理员相同）
//    - 创建新的客户端记录
//    - 返回注册结果
// 请求方式：GET（显示页面）、POST（提交注册）
// POST参数：username（用户名）、password（密码）
// 返回：GET返回注册页面，POST返回JSON格式的注册结果
func (self *LoginController) Register() {
	if self.Ctx.Request.Method == "GET" {
		// GET请求：显示注册页面
		self.Data["web_base_url"] = beego.AppConfig.String("web_base_url")
		self.TplName = "login/register.html"
	} else {
		// POST请求：处理注册逻辑
		
		// 检查是否允许用户注册
		if b, err := beego.AppConfig.Bool("allow_user_register"); err != nil || !b {
			self.Data["json"] = map[string]interface{}{"status": 0, "msg": "register is not allow"}
			self.ServeJSON()
			return
		}
		
		// 验证用户输入
		if self.GetString("username") == "" || self.GetString("password") == "" || self.GetString("username") == beego.AppConfig.String("web_username") {
			self.Data["json"] = map[string]interface{}{"status": 0, "msg": "please check your input"}
			self.ServeJSON()
			return
		}
		
		// 创建新的客户端记录
		t := &file.Client{
			Id:          int(file.GetDb().JsonDb.GetClientId()), // 获取新的客户端ID
			Status:      true,                                   // 启用状态
			Cnf:         &file.Config{},                        // 配置信息
			WebUserName: self.GetString("username"),            // Web用户名
			WebPassword: self.GetString("password"),            // Web密码
			Flow:        &file.Flow{},                          // 流量统计
		}
		
		// 保存客户端到数据库
		if err := file.GetDb().NewClient(t); err != nil {
			// 注册失败
			self.Data["json"] = map[string]interface{}{"status": 0, "msg": err.Error()}
		} else {
			// 注册成功
			self.Data["json"] = map[string]interface{}{"status": 1, "msg": "register success"}
		}
		self.ServeJSON()
	}
}

// Out 用户登出方法
// 功能说明：
// 1. 清除用户的认证状态（将auth会话设置为false）
// 2. 重定向到登录页面
// 请求方式：GET
// 返回：重定向到登录页面
func (self *LoginController) Out() {
	// 清除认证状态
	self.SetSession("auth", false)
	// 重定向到登录页面
	self.Redirect(beego.AppConfig.String("web_base_url")+"/login/index", 302)
}

// clearIprecord 清理过期IP记录的函数
// 功能说明：
// 1. 使用随机机制（1%概率）触发清理操作，避免每次登录都执行清理
// 2. 清理超过60秒未活动的IP记录，释放内存
// 3. 防止ipRecord映射表无限增长
// 调用时机：每次执行doLogin时调用
func clearIprecord() {
	// 设置随机种子
	rand.Seed(time.Now().UnixNano())
	// 生成0-99的随机数
	x := rand.Intn(100)
	// 1%的概率执行清理操作
	if x == 1 {
		// 遍历所有IP记录
		ipRecord.Range(func(key, value interface{}) bool {
			v := value.(*record)
			// 如果记录超过60秒未活动，则删除
			if time.Now().Unix()-v.lastLoginTime.Unix() >= 60 {
				ipRecord.Delete(key)
			}
			return true // 继续遍历
		})
	}
}
