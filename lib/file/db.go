// Package file 提供了NPS系统的数据库操作功能
// 主要包括客户端、任务和主机的管理操作
package file

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/crypt"
	"ehang.io/nps/lib/rate"
)

// DbUtils 数据库工具结构体，封装了JSON数据库的操作
type DbUtils struct {
	JsonDb *JsonDb // JSON数据库实例
}

var (
	Db   *DbUtils    // 全局数据库实例
	once sync.Once   // 确保数据库只初始化一次的同步原语
)

// GetDb 获取数据库实例（单例模式）
// 使用sync.Once确保数据库只初始化一次，线程安全
// 返回值: *DbUtils - 数据库工具实例
func GetDb() *DbUtils {
	once.Do(func() {
		// 创建JSON数据库实例
		jsonDb := NewJsonDb(common.GetRunPath())
		// 从文件加载客户端数据
		jsonDb.LoadClientFromJsonFile()
		// 从文件加载任务数据
		jsonDb.LoadTaskFromJsonFile()
		// 从文件加载主机数据
		jsonDb.LoadHostFromJsonFile()
		// 初始化数据库工具实例
		Db = &DbUtils{JsonDb: jsonDb}
	})
	return Db
}

// GetMapKeys 从sync.Map中获取所有键值，支持排序
// 参数:
//   m - sync.Map实例
//   isSort - 是否需要排序
//   sortKey - 排序字段名
//   order - 排序顺序（asc/desc）
// 返回值: []int - 键值列表
func GetMapKeys(m sync.Map, isSort bool, sortKey, order string) (keys []int) {
	// 如果指定了排序字段且需要排序，使用专门的排序函数
	if sortKey != "" && isSort {
		return sortClientByKey(m, sortKey, order)
	}
	// 遍历sync.Map获取所有键值
	m.Range(func(key, value interface{}) bool {
		keys = append(keys, key.(int))
		return true
	})
	// 对键值进行升序排序
	sort.Ints(keys)
	return
}

// GetClientList 获取客户端列表，支持分页、搜索和排序
// 参数:
//   start - 起始位置（用于分页）
//   length - 返回数量（用于分页）
//   search - 搜索关键词（可搜索ID、验证密钥、备注）
//   sort - 排序字段
//   order - 排序顺序
//   clientId - 指定客户端ID（0表示不限制）
// 返回值: 
//   []*Client - 客户端列表
//   int - 符合条件的总数量
func (s *DbUtils) GetClientList(start, length int, search, sort, order string, clientId int) ([]*Client, int) {
	list := make([]*Client, 0)
	var cnt int
	// 获取排序后的键值列表
	keys := GetMapKeys(s.JsonDb.Clients, true, sort, order)
	for _, key := range keys {
		if value, ok := s.JsonDb.Clients.Load(key); ok {
			v := value.(*Client)
			// 跳过不显示的客户端
			if v.NoDisplay {
				continue
			}
			// 如果指定了客户端ID，只返回匹配的客户端
			if clientId != 0 && clientId != v.Id {
				continue
			}
			// 搜索过滤：支持按ID、验证密钥、备注搜索
			if search != "" && !(v.Id == common.GetIntNoErrByStr(search) || strings.Contains(v.VerifyKey, search) || strings.Contains(v.Remark, search)) {
				continue
			}
			cnt++
			// 分页处理：跳过start个记录
			if start--; start < 0 {
				// 返回length个记录
				if length--; length >= 0 {
					list = append(list, v)
				}
			}
		}
	}
	return list, cnt
}

// GetIdByVerifyKey 根据验证密钥获取客户端ID
// 参数:
//   vKey - 验证密钥
//   addr - 客户端地址
// 返回值:
//   id - 客户端ID
//   err - 错误信息
func (s *DbUtils) GetIdByVerifyKey(vKey string, addr string) (id int, err error) {
	var exist bool
	// 遍历所有客户端查找匹配的验证密钥
	s.JsonDb.Clients.Range(func(key, value interface{}) bool {
		v := value.(*Client)
		// 验证密钥匹配且客户端状态为启用
		if common.Getverifyval(v.VerifyKey) == vKey && v.Status {
			// 更新客户端地址
			v.Addr = common.GetIpByAddr(addr)
			id = v.Id
			exist = true
			return false // 找到后停止遍历
		}
		return true
	})
	if exist {
		return
	}
	return 0, errors.New("not found")
}

// NewTask 创建新的隧道任务
// 参数:
//   t - 隧道对象
// 返回值:
//   err - 错误信息
func (s *DbUtils) NewTask(t *Tunnel) (err error) {
	// 检查secret模式和p2p模式的密码唯一性
	s.JsonDb.Tasks.Range(func(key, value interface{}) bool {
		v := value.(*Tunnel)
		if (v.Mode == "secret" || v.Mode == "p2p") && v.Password == t.Password {
			err = errors.New(fmt.Sprintf("secret mode keys %s must be unique", t.Password))
			return false // 发现重复密码，停止遍历
		}
		return true
	})
	if err != nil {
		return
	}
	// 初始化流量统计
	t.Flow = new(Flow)
	// 存储任务到内存
	s.JsonDb.Tasks.Store(t.Id, t)
	// 持久化到文件
	s.JsonDb.StoreTasksToJsonFile()
	return
}

// UpdateTask 更新隧道任务
// 参数:
//   t - 隧道对象
// 返回值:
//   error - 错误信息
func (s *DbUtils) UpdateTask(t *Tunnel) error {
	// 更新内存中的任务
	s.JsonDb.Tasks.Store(t.Id, t)
	// 持久化到文件
	s.JsonDb.StoreTasksToJsonFile()
	return nil
}

// DelTask 删除隧道任务
// 参数:
//   id - 任务ID
// 返回值:
//   error - 错误信息
func (s *DbUtils) DelTask(id int) error {
	// 从内存中删除任务
	s.JsonDb.Tasks.Delete(id)
	// 持久化到文件
	s.JsonDb.StoreTasksToJsonFile()
	return nil
}

// GetTaskByMd5Password 根据MD5加密的密码获取隧道任务
// 参数:
//   p - MD5加密后的密码
// 返回值:
//   t - 隧道对象（如果找到）
func (s *DbUtils) GetTaskByMd5Password(p string) (t *Tunnel) {
	// 遍历所有任务查找匹配的MD5密码
	s.JsonDb.Tasks.Range(func(key, value interface{}) bool {
		if crypt.Md5(value.(*Tunnel).Password) == p {
			t = value.(*Tunnel)
			return false // 找到后停止遍历
		}
		return true
	})
	return
}

// GetTask 根据ID获取隧道任务
// 参数:
//   id - 任务ID
// 返回值:
//   t - 隧道对象
//   err - 错误信息
func (s *DbUtils) GetTask(id int) (t *Tunnel, err error) {
	if v, ok := s.JsonDb.Tasks.Load(id); ok {
		t = v.(*Tunnel)
		return
	}
	err = errors.New("not found")
	return
}

// DelHost 删除主机配置
// 参数:
//   id - 主机ID
// 返回值:
//   error - 错误信息
func (s *DbUtils) DelHost(id int) error {
	// 从内存中删除主机
	s.JsonDb.Hosts.Delete(id)
	// 持久化到文件
	s.JsonDb.StoreHostToJsonFile()
	return nil
}

// IsHostExist 检查主机配置是否已存在
// 参数:
//   h - 主机对象
// 返回值:
//   bool - 是否存在
func (s *DbUtils) IsHostExist(h *Host) bool {
	var exist bool
	// 遍历所有主机检查是否存在相同配置
	s.JsonDb.Hosts.Range(func(key, value interface{}) bool {
		v := value.(*Host)
		// 检查主机名、路径和协议是否冲突（排除自身）
		if v.Id != h.Id && v.Host == h.Host && h.Location == v.Location && (v.Scheme == "all" || v.Scheme == h.Scheme) {
			exist = true
			return false // 发现冲突，停止遍历
		}
		return true
	})
	return exist
}

// NewHost 创建新的主机配置
// 参数:
//   t - 主机对象
// 返回值:
//   error - 错误信息
func (s *DbUtils) NewHost(t *Host) error {
	// 如果未设置路径，默认为根路径
	if t.Location == "" {
		t.Location = "/"
	}
	// 检查主机配置是否已存在
	if s.IsHostExist(t) {
		return errors.New("host has exist")
	}
	// 初始化流量统计
	t.Flow = new(Flow)
	// 存储主机到内存
	s.JsonDb.Hosts.Store(t.Id, t)
	// 持久化到文件
	s.JsonDb.StoreHostToJsonFile()
	return nil
}

// GetHost 获取主机列表，支持分页和搜索
// 参数:
//   start - 起始位置（用于分页）
//   length - 返回数量（用于分页）
//   id - 指定客户端ID（0表示不限制）
//   search - 搜索关键词（可搜索ID、主机名、备注）
// 返回值:
//   []*Host - 主机列表
//   int - 符合条件的总数量
func (s *DbUtils) GetHost(start, length int, id int, search string) ([]*Host, int) {
	list := make([]*Host, 0)
	var cnt int
	// 获取所有主机键值（不排序）
	keys := GetMapKeys(s.JsonDb.Hosts, false, "", "")
	for _, key := range keys {
		if value, ok := s.JsonDb.Hosts.Load(key); ok {
			v := value.(*Host)
			// 搜索过滤：支持按ID、主机名、备注搜索
			if search != "" && !(v.Id == common.GetIntNoErrByStr(search) || strings.Contains(v.Host, search) || strings.Contains(v.Remark, search)) {
				continue
			}
			// 客户端ID过滤
			if id == 0 || v.Client.Id == id {
				cnt++
				// 分页处理：跳过start个记录
				if start--; start < 0 {
					// 返回length个记录
					if length--; length >= 0 {
						list = append(list, v)
					}
				}
			}
		}
	}
	return list, cnt
}

// DelClient 删除客户端
// 参数:
//   id - 客户端ID
// 返回值:
//   error - 错误信息
func (s *DbUtils) DelClient(id int) error {
	// 从内存中删除客户端
	s.JsonDb.Clients.Delete(id)
	// 持久化到文件
	s.JsonDb.StoreClientsToJsonFile()
	return nil
}

// NewClient 创建新的客户端
// 参数:
//   c - 客户端对象
// 返回值:
//   error - 错误信息
func (s *DbUtils) NewClient(c *Client) error {
	var isNotSet bool
	// 检查Web登录用户名是否重复
	if c.WebUserName != "" && !s.VerifyUserName(c.WebUserName, c.Id) {
		return errors.New("web login username duplicate, please reset")
	}
reset:
	// 如果验证密钥为空或需要重新设置，生成随机密钥
	if c.VerifyKey == "" || isNotSet {
		isNotSet = true
		c.VerifyKey = crypt.GetRandomString(16)
	}
	// 设置速率限制
	if c.RateLimit == 0 {
		// 默认速率限制：16MB/s
		c.Rate = rate.NewRate(int64(2 << 23))
	} else if c.Rate == nil {
		// 根据配置设置速率限制（KB转换为字节）
		c.Rate = rate.NewRate(int64(c.RateLimit * 1024))
	}
	// 启动速率限制器
	c.Rate.Start()
	// 验证密钥唯一性
	if !s.VerifyVkey(c.VerifyKey, c.Id) {
		if isNotSet {
			// 如果是自动生成的密钥重复，重新生成
			goto reset
		}
		return errors.New("Vkey duplicate, please reset")
	}
	// 如果ID为0，自动分配新ID
	if c.Id == 0 {
		c.Id = int(s.JsonDb.GetClientId())
	}
	// 初始化流量统计
	if c.Flow == nil {
		c.Flow = new(Flow)
	}
	// 存储客户端到内存
	s.JsonDb.Clients.Store(c.Id, c)
	// 持久化到文件
	s.JsonDb.StoreClientsToJsonFile()
	return nil
}

// VerifyVkey 验证验证密钥的唯一性
// 参数:
//   vkey - 验证密钥
//   id - 客户端ID（排除自身）
// 返回值:
//   res - 是否唯一（true表示唯一）
func (s *DbUtils) VerifyVkey(vkey string, id int) (res bool) {
	res = true
	// 遍历所有客户端检查验证密钥是否重复
	s.JsonDb.Clients.Range(func(key, value interface{}) bool {
		v := value.(*Client)
		// 如果找到相同的验证密钥且不是同一个客户端
		if v.VerifyKey == vkey && v.Id != id {
			res = false
			return false // 发现重复，停止遍历
		}
		return true
	})
	return res
}

// VerifyUserName 验证Web登录用户名的唯一性
// 参数:
//   username - 用户名
//   id - 客户端ID（排除自身）
// 返回值:
//   res - 是否唯一（true表示唯一）
func (s *DbUtils) VerifyUserName(username string, id int) (res bool) {
	res = true
	// 遍历所有客户端检查用户名是否重复
	s.JsonDb.Clients.Range(func(key, value interface{}) bool {
		v := value.(*Client)
		// 如果找到相同的用户名且不是同一个客户端
		if v.WebUserName == username && v.Id != id {
			res = false
			return false // 发现重复，停止遍历
		}
		return true
	})
	return res
}

// UpdateClient 更新客户端信息
// 参数:
//   t - 客户端对象
// 返回值:
//   error - 错误信息
func (s *DbUtils) UpdateClient(t *Client) error {
	// 更新内存中的客户端信息
	s.JsonDb.Clients.Store(t.Id, t)
	// 如果速率限制为0，设置默认速率限制
	if t.RateLimit == 0 {
		t.Rate = rate.NewRate(int64(2 << 23))
		t.Rate.Start()
	}
	return nil
}

// IsPubClient 检查客户端是否为公共客户端（不显示）
// 参数:
//   id - 客户端ID
// 返回值:
//   bool - 是否为公共客户端
func (s *DbUtils) IsPubClient(id int) bool {
	client, err := s.GetClient(id)
	if err == nil {
		return client.NoDisplay
	}
	return false
}

// GetClient 根据ID获取客户端
// 参数:
//   id - 客户端ID
// 返回值:
//   c - 客户端对象
//   err - 错误信息
func (s *DbUtils) GetClient(id int) (c *Client, err error) {
	if v, ok := s.JsonDb.Clients.Load(id); ok {
		c = v.(*Client)
		return
	}
	err = errors.New("未找到客户端")
	return
}

// GetClientIdByVkey 根据MD5加密的验证密钥获取客户端ID
// 参数:
//   vkey - MD5加密后的验证密钥
// 返回值:
//   id - 客户端ID
//   err - 错误信息
func (s *DbUtils) GetClientIdByVkey(vkey string) (id int, err error) {
	var exist bool
	// 遍历所有客户端查找匹配的MD5验证密钥
	s.JsonDb.Clients.Range(func(key, value interface{}) bool {
		v := value.(*Client)
		if crypt.Md5(v.VerifyKey) == vkey {
			exist = true
			id = v.Id
			return false // 找到后停止遍历
		}
		return true
	})
	if exist {
		return
	}
	err = errors.New("未找到客户端")
	return
}

// GetHostById 根据ID获取主机配置
// 参数:
//   id - 主机ID
// 返回值:
//   h - 主机对象
//   err - 错误信息
func (s *DbUtils) GetHostById(id int) (h *Host, err error) {
	if v, ok := s.JsonDb.Hosts.Load(id); ok {
		h = v.(*Host)
		return
	}
	err = errors.New("The host could not be parsed")
	return
}

// GetInfoByHost 根据主机名和HTTP请求获取匹配的主机配置
// 支持通配符匹配和路径匹配，返回最精确匹配的主机配置
// 参数:
//   host - 主机名
//   r - HTTP请求对象
// 返回值:
//   h - 匹配的主机配置
//   err - 错误信息
func (s *DbUtils) GetInfoByHost(host string, r *http.Request) (h *Host, err error) {
	var hosts []*Host
	// 处理带端口的访问，提取IP地址
	host = common.GetIpByAddr(host)
	
	// 遍历所有主机配置，查找匹配的主机
	s.JsonDb.Hosts.Range(func(key, value interface{}) bool {
		v := value.(*Host)
		// 跳过已关闭的主机
		if v.IsClose {
			return true
		}
		// 检查协议匹配：如果主机配置不是"all"且与请求协议不匹配，跳过
		if v.Scheme != "all" && v.Scheme != r.URL.Scheme {
			return true
		}
		
		tmpHost := v.Host
		// 处理通配符主机名匹配（如 *.example.com）
		if strings.Contains(tmpHost, "*") {
			// 移除通配符进行部分匹配
			tmpHost = strings.Replace(tmpHost, "*", "", -1)
			if strings.Contains(host, tmpHost) {
				hosts = append(hosts, v)
			}
		} else if v.Host == host {
			// 精确主机名匹配
			hosts = append(hosts, v)
		}
		return true
	})

	// 在匹配的主机中查找最精确的路径匹配
	for _, v := range hosts {
		// 如果未设置路径，默认匹配所有路径
		if v.Location == "" {
			v.Location = "/"
		}
		// 检查请求URI是否以配置的路径开头
		if strings.Index(r.RequestURI, v.Location) == 0 {
			// 选择路径最长（最精确）的匹配
			if h == nil || (len(v.Location) > len(h.Location)) {
				h = v
			}
		}
	}
	
	if h != nil {
		return
	}
	err = errors.New("The host could not be parsed")
	return
}
