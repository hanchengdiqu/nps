package main

import (
	"fmt"
	"os"
	"path/filepath"
	"ehang.io/nps/lib/file"
)

func main() {
	fmt.Println("=== NPS 存储调试工具 ===")
	
	// 获取当前工作目录
	pwd, _ := os.Getwd()
	fmt.Printf("当前工作目录: %s\n", pwd)
	
	// 初始化数据库
	db := file.GetDb()
	jsonDb := db.JsonDb
	fmt.Printf("数据库运行路径: %s\n", jsonDb.RunPath)
	fmt.Printf("客户端文件路径: %s\n", jsonDb.ClientFilePath)
	fmt.Printf("任务文件路径: %s\n", jsonDb.TaskFilePath)
	fmt.Printf("主机文件路径: %s\n", jsonDb.HostFilePath)
	
	// 检查文件路径是否存在
	checkPath := func(name, path string) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Printf("%s文件不存在: %s\n", name, path)
		} else {
			info, _ := os.Stat(path)
			fmt.Printf("%s文件存在: %s (大小: %d 字节)\n", name, path, info.Size())
		}
	}
	
	checkPath("客户端", jsonDb.ClientFilePath)
	checkPath("任务", jsonDb.TaskFilePath)
	checkPath("主机", jsonDb.HostFilePath)
	
	// 检查目录权限
	confDir := filepath.Dir(jsonDb.ClientFilePath)
	fmt.Printf("配置目录: %s\n", confDir)
	
	if info, err := os.Stat(confDir); err != nil {
		fmt.Printf("配置目录不存在或无法访问: %v\n", err)
	} else {
		fmt.Printf("配置目录权限: %v\n", info.Mode())
	}
	
	// 尝试创建测试文件
	testFile := filepath.Join(confDir, "test.txt")
	if f, err := os.Create(testFile); err != nil {
		fmt.Printf("无法在配置目录创建文件: %v\n", err)
	} else {
		f.WriteString("test")
		f.Close()
		os.Remove(testFile)
		fmt.Println("配置目录写入权限正常")
	}
	
	// 检查内存中的数据
	clientCount := 0
	jsonDb.Clients.Range(func(key, value interface{}) bool {
		clientCount++
		client := value.(*file.Client)
		fmt.Printf("客户端 %v: NoStore=%v\n", key, client.NoStore)
		return true
	})
	
	taskCount := 0
	jsonDb.Tasks.Range(func(key, value interface{}) bool {
		taskCount++
		task := value.(*file.Tunnel)
		fmt.Printf("任务 %v: NoStore=%v\n", key, task.NoStore)
		return true
	})
	
	hostCount := 0
	jsonDb.Hosts.Range(func(key, value interface{}) bool {
		hostCount++
		host := value.(*file.Host)
		fmt.Printf("主机 %v: NoStore=%v\n", key, host.NoStore)
		return true
	})
	
	fmt.Printf("内存中数据统计: 客户端=%d, 任务=%d, 主机=%d\n", clientCount, taskCount, hostCount)
	
	// 手动调用存储方法
	fmt.Println("\n=== 开始存储数据 ===")
	
	fmt.Println("存储客户端数据...")
	jsonDb.StoreClientsToJsonFile()
	
	fmt.Println("存储任务数据...")
	jsonDb.StoreTasksToJsonFile()
	
	fmt.Println("存储主机数据...")
	jsonDb.StoreHostToJsonFile()
	
	fmt.Println("存储完成")
	
	// 再次检查文件大小
	fmt.Println("\n=== 存储后文件状态 ===")
	checkPath("客户端", jsonDb.ClientFilePath)
	checkPath("任务", jsonDb.TaskFilePath)
	checkPath("主机", jsonDb.HostFilePath)
}