package main

import (
	"fmt"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/common"
)

func main() {
	fmt.Println("=== NPS 数据调试工具 ===")
	
	// 获取数据库实例
	db := file.GetDb()
	
	// 检查客户端数据
	clientCount := common.GeSynctMapLen(db.JsonDb.Clients)
	fmt.Printf("内存中客户端数量: %d\n", clientCount)
	
	// 检查任务数据
	taskCount := common.GeSynctMapLen(db.JsonDb.Tasks)
	fmt.Printf("内存中任务数量: %d\n", taskCount)
	
	// 检查主机数据
	hostCount := common.GeSynctMapLen(db.JsonDb.Hosts)
	fmt.Printf("内存中主机数量: %d\n", hostCount)
	
	// 打印一些详细信息
	fmt.Println("\n=== 客户端详情 ===")
	db.JsonDb.Clients.Range(func(key, value interface{}) bool {
		client := value.(*file.Client)
		fmt.Printf("客户端ID: %d, 验证密钥: %s, 备注: %s\n", client.Id, client.VerifyKey, client.Remark)
		return true
	})
	
	fmt.Println("\n=== 任务详情 ===")
	db.JsonDb.Tasks.Range(func(key, value interface{}) bool {
		task := value.(*file.Tunnel)
		fmt.Printf("任务ID: %d, 模式: %s, 端口: %d\n", task.Id, task.Mode, task.Port)
		return true
	})
	
	fmt.Println("\n=== 主机详情 ===")
	db.JsonDb.Hosts.Range(func(key, value interface{}) bool {
		host := value.(*file.Host)
		fmt.Printf("主机ID: %d, 主机名: %s, 位置: %s\n", host.Id, host.Host, host.Location)
		return true
	})
	
	// 尝试手动存储数据
	fmt.Println("\n=== 手动存储数据到文件 ===")
	db.JsonDb.StoreClientsToJsonFile()
	db.JsonDb.StoreTasksToJsonFile()
	db.JsonDb.StoreHostToJsonFile()
	fmt.Println("数据存储完成")
}