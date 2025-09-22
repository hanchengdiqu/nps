// Package file 提供JSON文件数据库的实现
// 负责客户端、任务和主机配置的持久化存储和加载
package file

import (
	"encoding/json"
	"errors"
	"github.com/astaxie/beego/logs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/rate"
)

// NewJsonDb 创建新的JSON数据库实例
// 参数:
//   runPath - 运行路径，用于确定配置文件位置
// 返回值:
//   *JsonDb - JSON数据库实例
func NewJsonDb(runPath string) *JsonDb {
	return &JsonDb{
		RunPath:        runPath,
		TaskFilePath:   filepath.Join(runPath, "conf", "tasks.json"),
		HostFilePath:   filepath.Join(runPath, "conf", "hosts.json"),
		ClientFilePath: filepath.Join(runPath, "conf", "clients.json"),
	}
}

// JsonDb JSON文件数据库结构体
// 使用sync.Map提供线程安全的内存存储，并支持持久化到JSON文件
type JsonDb struct {
	Tasks            sync.Map // 隧道任务存储，key为任务ID，value为*Tunnel
	Hosts            sync.Map // 主机配置存储，key为主机ID，value为*Host
	HostsTmp         sync.Map // 临时主机配置存储
	Clients          sync.Map // 客户端存储，key为客户端ID，value为*Client
	RunPath          string   // 程序运行路径
	ClientIncreaseId int32    // 客户端自增ID计数器
	TaskIncreaseId   int32    // 任务自增ID计数器
	HostIncreaseId   int32    // 主机自增ID计数器
	TaskFilePath     string   // 任务配置文件路径
	HostFilePath     string   // 主机配置文件路径
	ClientFilePath   string   // 客户端配置文件路径
}

// LoadTaskFromJsonFile 从JSON文件加载隧道任务配置
// 读取任务配置文件，解析JSON数据并加载到内存中
func (s *JsonDb) LoadTaskFromJsonFile() {
	loadSyncMapFromFile(s.TaskFilePath, func(v string) {
		var err error
		post := new(Tunnel)
		// 解析JSON数据到Tunnel结构体
		if json.Unmarshal([]byte(v), &post) != nil {
			return
		}
		// 根据客户端ID获取客户端对象，建立关联关系
		if post.Client, err = s.GetClient(post.Client.Id); err != nil {
			return
		}
		// 存储任务到内存
		s.Tasks.Store(post.Id, post)
		// 更新任务ID计数器，确保新任务ID不重复
		if post.Id > int(s.TaskIncreaseId) {
			s.TaskIncreaseId = int32(post.Id)
		}
	})
}

// LoadClientFromJsonFile 从JSON文件加载客户端配置
// 读取客户端配置文件，解析JSON数据并初始化客户端对象
func (s *JsonDb) LoadClientFromJsonFile() {
	loadSyncMapFromFile(s.ClientFilePath, func(v string) {
		post := new(Client)
		// 解析JSON数据到Client结构体
		if json.Unmarshal([]byte(v), &post) != nil {
			return
		}
		// 初始化速率限制器
		if post.RateLimit > 0 {
			// 根据配置的速率限制创建限制器（KB转换为字节）
			post.Rate = rate.NewRate(int64(post.RateLimit * 1024))
		} else {
			// 默认速率限制：16MB/s
			post.Rate = rate.NewRate(int64(2 << 23))
		}
		// 启动速率限制器
		post.Rate.Start()
		// 重置当前连接数
		post.NowConn = 0
		// 存储客户端到内存
		s.Clients.Store(post.Id, post)
		// 更新客户端ID计数器，确保新客户端ID不重复
		if post.Id > int(s.ClientIncreaseId) {
			s.ClientIncreaseId = int32(post.Id)
		}
	})
}

// LoadHostFromJsonFile 从JSON文件加载主机配置
// 读取主机配置文件，解析JSON数据并加载到内存中
func (s *JsonDb) LoadHostFromJsonFile() {
	loadSyncMapFromFile(s.HostFilePath, func(v string) {
		var err error
		post := new(Host)
		// 解析JSON数据到Host结构体
		if json.Unmarshal([]byte(v), &post) != nil {
			return
		}
		// 根据客户端ID获取客户端对象，建立关联关系
		if post.Client, err = s.GetClient(post.Client.Id); err != nil {
			return
		}
		// 存储主机配置到内存
		s.Hosts.Store(post.Id, post)
		// 更新主机ID计数器，确保新主机ID不重复
		if post.Id > int(s.HostIncreaseId) {
			s.HostIncreaseId = int32(post.Id)
		}
	})
}

// GetClient 根据ID获取客户端对象
// 参数:
//   id - 客户端ID
// 返回值:
//   c - 客户端对象指针
//   err - 错误信息，如果客户端不存在则返回错误
func (s *JsonDb) GetClient(id int) (c *Client, err error) {
	// 从内存中查找客户端
	if v, ok := s.Clients.Load(id); ok {
		c = v.(*Client)
		return
	}
	err = errors.New("未找到客户端")
	return
}

// hostLock 主机配置文件写入锁，防止并发写入冲突
var hostLock sync.Mutex

// StoreHostToJsonFile 将主机配置持久化到JSON文件
// 使用互斥锁确保文件写入的原子性
func (s *JsonDb) StoreHostToJsonFile() {
	hostLock.Lock()
	storeSyncMapToFile(s.Hosts, s.HostFilePath)
	hostLock.Unlock()
}

// taskLock 任务配置文件写入锁，防止并发写入冲突
var taskLock sync.Mutex

// StoreTasksToJsonFile 将任务配置持久化到JSON文件
// 使用互斥锁确保文件写入的原子性
func (s *JsonDb) StoreTasksToJsonFile() {
	taskLock.Lock()
	storeSyncMapToFile(s.Tasks, s.TaskFilePath)
	taskLock.Unlock()
}

// clientLock 客户端配置文件写入锁，防止并发写入冲突
var clientLock sync.Mutex

// StoreClientsToJsonFile 将客户端配置持久化到JSON文件
// 使用互斥锁确保文件写入的原子性
func (s *JsonDb) StoreClientsToJsonFile() {
	clientLock.Lock()
	storeSyncMapToFile(s.Clients, s.ClientFilePath)
	clientLock.Unlock()
}

// GetClientId 获取下一个可用的客户端ID
// 使用原子操作确保ID的唯一性和线程安全
// 返回值:
//   int32 - 新的客户端ID
func (s *JsonDb) GetClientId() int32 {
	return atomic.AddInt32(&s.ClientIncreaseId, 1)
}

// GetTaskId 获取下一个可用的任务ID
// 使用原子操作确保ID的唯一性和线程安全
// 返回值:
//   int32 - 新的任务ID
func (s *JsonDb) GetTaskId() int32 {
	return atomic.AddInt32(&s.TaskIncreaseId, 1)
}

// GetHostId 获取下一个可用的主机ID
// 使用原子操作确保ID的唯一性和线程安全
// 返回值:
//   int32 - 新的主机ID
func (s *JsonDb) GetHostId() int32 {
	return atomic.AddInt32(&s.HostIncreaseId, 1)
}

// loadSyncMapFromFile 从文件加载数据到sync.Map
// 读取指定文件内容，按分隔符分割后逐行处理
// 参数:
//   filePath - 文件路径
//   f - 处理每行数据的回调函数
func loadSyncMapFromFile(filePath string, f func(value string)) {
	// 读取整个文件内容
	b, err := common.ReadAllFromFile(filePath)
	if err != nil {
		panic(err)
	}
	// 按分隔符分割文件内容，逐行处理
	for _, v := range strings.Split(string(b), "\n"+common.CONN_DATA_SEQ) {
		f(v)
	}
}

// storeSyncMapToFile 将sync.Map数据存储到文件
// 先写入临时文件，成功后替换原文件，确保数据完整性
// 参数:
//   m - 要存储的sync.Map
//   filePath - 目标文件路径
func storeSyncMapToFile(m sync.Map, filePath string) {
	// 创建临时文件，避免写入过程中文件损坏
	file, err := os.Create(filePath + ".tmp")
	if err != nil {
		panic(err)
	}
	// 遍历sync.Map中的所有数据
	m.Range(func(key, value interface{}) bool {
		var b []byte
		var err error
		// 根据数据类型进行JSON序列化
		switch value.(type) {
		case *Tunnel:
			obj := value.(*Tunnel)
			// 跳过标记为不存储的隧道
			if obj.NoStore {
				return true
			}
			b, err = json.Marshal(obj)
		case *Host:
			obj := value.(*Host)
			// 跳过标记为不存储的主机
			if obj.NoStore {
				return true
			}
			b, err = json.Marshal(obj)
		case *Client:
			obj := value.(*Client)
			// 跳过标记为不存储的客户端
			if obj.NoStore {
				return true
			}
			b, err = json.Marshal(obj)
		default:
			// 未知类型，跳过
			return true
		}
		// JSON序列化失败，跳过该条记录
		if err != nil {
			return true
		}
		// 写入JSON数据
		_, err = file.Write(b)
		if err != nil {
			panic(err)
		}
		// 写入分隔符
		_, err = file.Write([]byte("\n" + common.CONN_DATA_SEQ))
		if err != nil {
			panic(err)
		}
		return true
	})
	// 强制将缓冲区数据写入磁盘
	_ = file.Sync()
	// 关闭临时文件
	_ = file.Close()
	// 将临时文件重命名为目标文件，实现原子替换
	// 必须先关闭文件再重命名，否则在某些系统上会失败
	err = os.Rename(filePath+".tmp", filePath)
	if err != nil {
		logs.Error(err, "store to file err, data will lost")
	}
	// 文件替换操作，在大多数文件系统上提供原子性保证
}
