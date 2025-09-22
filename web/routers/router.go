// Package routers 定义了NPS Web管理界面的路由配置
// 该包负责初始化和配置所有Web管理界面的URL路由规则
package routers

import (
	"ehang.io/nps/web/controllers"
	"github.com/astaxie/beego"
)

// Init 初始化Web管理界面的路由配置
// 该函数根据配置文件中的web_base_url设置，决定是否使用命名空间来组织路由
// 支持以下控制器的路由配置：
// - IndexController: 主页面控制器
// - LoginController: 登录相关控制器  
// - ClientController: 客户端管理控制器
// - AuthController: 认证相关控制器
func Init() {
	// 从配置文件中获取Web基础URL路径
	web_base_url := beego.AppConfig.String("web_base_url")
	
	// 如果配置了web_base_url，则使用命名空间方式组织路由
	// 这样可以将所有Web管理界面的路由都放在指定的URL前缀下
	if len(web_base_url) > 0 {
		// 创建命名空间，所有路由都会添加web_base_url前缀
		ns := beego.NewNamespace(web_base_url,
			// 设置根路径路由，指向IndexController的Index方法，支持所有HTTP方法
			beego.NSRouter("/", &controllers.IndexController{}, "*:Index"),
			// 自动路由：根据控制器方法名自动生成路由规则
			beego.NSAutoRouter(&controllers.IndexController{}),   // 主页控制器自动路由
			beego.NSAutoRouter(&controllers.LoginController{}),   // 登录控制器自动路由
			beego.NSAutoRouter(&controllers.ClientController{}),  // 客户端管理控制器自动路由
			beego.NSAutoRouter(&controllers.AuthController{}),    // 认证控制器自动路由
		)
		// 将命名空间添加到Beego应用中
		beego.AddNamespace(ns)
	} else {
		// 如果没有配置web_base_url，则直接在根路径下注册路由
		// 设置根路径路由，指向IndexController的Index方法，支持所有HTTP方法
		beego.Router("/", &controllers.IndexController{}, "*:Index")
		// 为各个控制器注册自动路由
		beego.AutoRouter(&controllers.IndexController{})   // 主页控制器自动路由
		beego.AutoRouter(&controllers.LoginController{})   // 登录控制器自动路由
		beego.AutoRouter(&controllers.ClientController{})  // 客户端管理控制器自动路由
		beego.AutoRouter(&controllers.AuthController{})    // 认证控制器自动路由
	}
}
