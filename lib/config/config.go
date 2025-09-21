package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
)

type CommonConfig struct {
	Server           string
	VKey             string
	Tp               string //bridgeType kcp or tcp
	AutoReconnection bool
	ProxyUrl         string
	Client           *file.Client
	DisconnectTime   int
}

type LocalServer struct {
	Type     string
	Port     int
	Ip       string
	Password string
	Target   string
}

type Config struct {
	content      string
	title        []string
	CommonConfig *CommonConfig
	Hosts        []*file.Host
	Tasks        []*file.Tunnel
	Healths      []*file.Health
	LocalServer  []*LocalServer
}

// NewConfig 读取并解析配置文件
// 从给定路径加载配置内容，去除注释与空白，提取所有段标题，
// 按段落分别解析为 CommonConfig、Hosts、Tasks、Healths 以及 LocalServer（secret/p2p，无 mode）等结构。
// 支持以下段落与键：
// - [common]：server_addr、vkey、conn_type、auto_reconnection、basic_username、basic_password、
//   web_username、web_password、compress、crypt、proxy_url、rate_limit、flow_limit、max_conn、
//   remark、pprof_addr、disconnect_timeout 等
// - 其他段：如果包含 host 相关键解析为 Host，反之解析为 Tunnel
// - [secret*]/[p2p*] 且无 mode：解析为本地服务 LocalServer
// - [health*]：解析为健康检查配置
// 返回解析完成的 Config 结构体与错误信息
func NewConfig(path string) (c *Config, err error) {
	c = new(Config)
	var b []byte
	if b, err = common.ReadAllFromFile(path); err != nil {
		return
	} else {
		if c.content, err = common.ParseStr(string(b)); err != nil {
			return nil, err
		}
		if c.title, err = getAllTitle(c.content); err != nil {
			return
		}
		var nowIndex int
		var nextIndex int
		var nowContent string
		for i := 0; i < len(c.title); i++ {
			nowIndex = strings.Index(c.content, c.title[i]) + len(c.title[i])
			if i < len(c.title)-1 {
				nextIndex = strings.Index(c.content, c.title[i+1])
			} else {
				nextIndex = len(c.content)
			}
			nowContent = c.content[nowIndex:nextIndex]

			if strings.Index(getTitleContent(c.title[i]), "secret") == 0 && !strings.Contains(nowContent, "mode") {
				local := delLocalService(nowContent)
				local.Type = "secret"
				c.LocalServer = append(c.LocalServer, local)
				continue
			}
			//except mode
			if strings.Index(getTitleContent(c.title[i]), "p2p") == 0 && !strings.Contains(nowContent, "mode") {
				local := delLocalService(nowContent)
				local.Type = "p2p"
				c.LocalServer = append(c.LocalServer, local)
				continue
			}
			//health set
			if strings.Index(getTitleContent(c.title[i]), "health") == 0 {
				c.Healths = append(c.Healths, dealHealth(nowContent))
				continue
			}
			switch c.title[i] {
			case "[common]":
				c.CommonConfig = dealCommon(nowContent)
			default:
				if strings.Index(nowContent, "host") > -1 {
					h := dealHost(nowContent)
					h.Remark = getTitleContent(c.title[i])
					c.Hosts = append(c.Hosts, h)
				} else {
					t := dealTunnel(nowContent)
					t.Remark = getTitleContent(c.title[i])
					c.Tasks = append(c.Tasks, t)
				}
			}
		}
	}
	return
}

// getTitleContent 去除方括号后的段名
// 输入类似 "[common]" 或 "[section-x]" 的标题字符串，返回去掉前后方括号后的纯标题。
// 例如："[common]" -> "common"
func getTitleContent(s string) string {
	re, _ := regexp.Compile(`[\[\]]`)
	return re.ReplaceAllString(s, "")
}

// dealCommon 解析 [common] 段
// 将分行的 key=value 文本转换为 CommonConfig，并填充嵌套的 file.Client 参数。
// 会将字符串转换为布尔值/整数等类型；支持 pprof 初始化与断连超时设置。
// 未识别或缺失的键保持零值。
func dealCommon(s string) *CommonConfig {
	c := &CommonConfig{}
	c.Client = file.NewClient("", true, true)
	c.Client.Cnf = new(file.Config)
	for _, v := range splitStr(s) {
		item := strings.Split(v, "=")
		if len(item) == 0 {
			continue
		} else if len(item) == 1 {
			item = append(item, "")
		}
		switch item[0] {
		case "server_addr":
			c.Server = item[1]
		case "vkey":
			c.VKey = item[1]
		case "conn_type":
			c.Tp = item[1]
		case "auto_reconnection":
			c.AutoReconnection = common.GetBoolByStr(item[1])
		case "basic_username":
			c.Client.Cnf.U = item[1]
		case "basic_password":
			c.Client.Cnf.P = item[1]
		case "web_password":
			c.Client.WebPassword = item[1]
		case "web_username":
			c.Client.WebUserName = item[1]
		case "compress":
			c.Client.Cnf.Compress = common.GetBoolByStr(item[1])
		case "crypt":
			c.Client.Cnf.Crypt = common.GetBoolByStr(item[1])
		case "proxy_url":
			c.ProxyUrl = item[1]
		case "rate_limit":
			c.Client.RateLimit = common.GetIntNoErrByStr(item[1])
		case "flow_limit":
			c.Client.Flow.FlowLimit = int64(common.GetIntNoErrByStr(item[1]))
		case "max_conn":
			c.Client.MaxConn = common.GetIntNoErrByStr(item[1])
		case "remark":
			c.Client.Remark = item[1]
		case "pprof_addr":
			common.InitPProfFromArg(item[1])
		case "disconnect_timeout":
			c.DisconnectTime = common.GetIntNoErrByStr(item[1])
		}
	}
	return c
}

// dealHost 解析 Host 段
// 根据 host、target_addr、host_change、scheme、location 及 header_ 前缀键构造 *file.Host。
// target_addr 支持逗号分隔，自动转换为换行分隔。
// header_* 键会被聚合到 HeaderChange（形如 key:value 的多行字符串）。
func dealHost(s string) *file.Host {
	h := &file.Host{}
	h.Target = new(file.Target)
	h.Scheme = "all"
	var headerChange string
	for _, v := range splitStr(s) {
		item := strings.Split(v, "=")
		if len(item) == 0 {
			continue
		} else if len(item) == 1 {
			item = append(item, "")
		}
		switch strings.TrimSpace(item[0]) {
		case "host":
			h.Host = item[1]
		case "target_addr":
			h.Target.TargetStr = strings.Replace(item[1], ",", "\n", -1)
		case "host_change":
			h.HostChange = item[1]
		case "scheme":
			h.Scheme = item[1]
		case "location":
			h.Location = item[1]
		default:
			if strings.Contains(item[0], "header") {
				headerChange += strings.Replace(item[0], "header_", "", -1) + ":" + item[1] + "\n"
			}
			h.HeaderChange = headerChange
		}
	}
	return h
}

// dealHealth 解析健康检查段
// 支持键：health_check_timeout、health_check_max_failed、health_check_interval、
// health_http_url、health_check_type、health_check_target。
func dealHealth(s string) *file.Health {
	h := &file.Health{}
	for _, v := range splitStr(s) {
		item := strings.Split(v, "=")
		if len(item) == 0 {
			continue
		} else if len(item) == 1 {
			item = append(item, "")
		}
		switch strings.TrimSpace(item[0]) {
		case "health_check_timeout":
			h.HealthCheckTimeout = common.GetIntNoErrByStr(item[1])
		case "health_check_max_failed":
			h.HealthMaxFail = common.GetIntNoErrByStr(item[1])
		case "health_check_interval":
			h.HealthCheckInterval = common.GetIntNoErrByStr(item[1])
		case "health_http_url":
			h.HttpHealthUrl = item[1]
		case "health_check_type":
			h.HealthCheckType = item[1]
		case "health_check_target":
			h.HealthCheckTarget = item[1]
		}
	}
	return h
}

// dealTunnel 解析隧道段
// 根据 server_port/server_ip/mode/target_addr/target_port/target_ip/password/local_path/strip_pre 等键
// 构造 *file.Tunnel。若设置 multi_account 为文件路径，将读取并解析为多账号映射。
func dealTunnel(s string) *file.Tunnel {
	t := &file.Tunnel{}
	t.Target = new(file.Target)
	for _, v := range splitStr(s) {
		item := strings.Split(v, "=")
		if len(item) == 0 {
			continue
		} else if len(item) == 1 {
			item = append(item, "")
		}
		switch strings.TrimSpace(item[0]) {
		case "server_port":
			t.Ports = item[1]
		case "server_ip":
			t.ServerIp = item[1]
		case "mode":
			t.Mode = item[1]
		case "target_addr":
			t.Target.TargetStr = strings.Replace(item[1], ",", "\n", -1)
		case "target_port":
			t.Target.TargetStr = item[1]
		case "target_ip":
			t.TargetAddr = item[1]
		case "password":
			t.Password = item[1]
		case "local_path":
			t.LocalPath = item[1]
		case "strip_pre":
			t.StripPre = item[1]
		case "multi_account":
			t.MultiAccount = &file.MultiAccount{}
			if common.FileExists(item[1]) {
				if b, err := common.ReadAllFromFile(item[1]); err != nil {
					panic(err)
				} else {
					if content, err := common.ParseStr(string(b)); err != nil {
						panic(err)
					} else {
						t.MultiAccount.AccountMap = dealMultiUser(content)
					}
				}
			}
		}
	}
	return t

}

// dealMultiUser 解析多账户映射
// 将多行的 key=value 格式内容解析为账号映射 map[user]password。
// 空值键会被保留为对应的空字符串。
func dealMultiUser(s string) map[string]string {
	multiUserMap := make(map[string]string)
	for _, v := range splitStr(s) {
		item := strings.Split(v, "=")
		if len(item) == 0 {
			continue
		} else if len(item) == 1 {
			item = append(item, "")
		}
		multiUserMap[strings.TrimSpace(item[0])] = item[1]
	}
	return multiUserMap
}

// delLocalService 解析本地服务（secret/p2p，无 mode）
// 支持键：local_port、local_ip、password、target_addr。
func delLocalService(s string) *LocalServer {
	l := new(LocalServer)
	for _, v := range splitStr(s) {
		item := strings.Split(v, "=")
		if len(item) == 0 {
			continue
		} else if len(item) == 1 {
			item = append(item, "")
		}
		switch item[0] {
		case "local_port":
			l.Port = common.GetIntNoErrByStr(item[1])
		case "local_ip":
			l.Ip = item[1]
		case "password":
			l.Password = item[1]
		case "target_addr":
			l.Target = item[1]
		}
	}
	return l
}

// getAllTitle 提取所有段标题并校验唯一性
// 使用正则按行匹配形如 [title] 的段名，保持出现顺序返回。
// 若存在重复段名，返回错误。
func getAllTitle(content string) (arr []string, err error) {
	var re *regexp.Regexp
	re, err = regexp.Compile(`(?m)^\[[^\[\]\r\n]+\]`)
	if err != nil {
		return
	}
	arr = re.FindAllString(content, -1)
	m := make(map[string]bool)
	for _, v := range arr {
		if _, ok := m[v]; ok {
			err = errors.New(fmt.Sprintf("Item names %s are not allowed to be duplicated", v))
			return
		}
		m[v] = true
	}
	return
}

// splitStr 将配置片段按行拆分
// 在 Windows 上优先使用 \r\n 拆分；若结果行数过少（<3）则回退为按 \n 拆分。
// 返回拆分得到的每一行（保持顺序）。
func splitStr(s string) (configDataArr []string) {
	if common.IsWindows() {
		configDataArr = strings.Split(s, "\r\n")
	}
	if len(configDataArr) < 3 {
		configDataArr = strings.Split(s, "\n")
	}
	return
}
