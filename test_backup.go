package main

import (
	"fmt"
	"os"
	"path/filepath"
	"ehang.io/nps/lib/backup"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/common"
)

func main() {
	fmt.Println("=== NPS 备份功能测试 ===")
	
	// 初始化数据库
	db := file.GetDb()
	jsonDb := db.JsonDb
	
	fmt.Printf("数据库运行路径: %s\n", jsonDb.RunPath)
	fmt.Printf("客户端文件路径: %s\n", jsonDb.ClientFilePath)
	fmt.Printf("任务文件路径: %s\n", jsonDb.TaskFilePath)
	fmt.Printf("主机文件路径: %s\n", jsonDb.HostFilePath)
	
	// 检查数据文件是否存在
	checkFile := func(name, path string) {
		if info, err := os.Stat(path); err != nil {
			fmt.Printf("%s文件不存在: %s\n", name, path)
		} else {
			fmt.Printf("%s文件存在: %s (大小: %d 字节)\n", name, path, info.Size())
		}
	}
	
	fmt.Println("\n=== 检查数据文件 ===")
	checkFile("客户端", jsonDb.ClientFilePath)
	checkFile("任务", jsonDb.TaskFilePath)
	checkFile("主机", jsonDb.HostFilePath)
	checkFile("配置", filepath.Join(common.GetRunPath(), "conf", "nps.conf"))
	
	// 检查内存中的数据
	clientCount := 0
	jsonDb.Clients.Range(func(key, value interface{}) bool {
		clientCount++
		return true
	})
	
	taskCount := 0
	jsonDb.Tasks.Range(func(key, value interface{}) bool {
		taskCount++
		return true
	})
	
	hostCount := 0
	jsonDb.Hosts.Range(func(key, value interface{}) bool {
		hostCount++
		return true
	})
	
	fmt.Printf("\n内存中数据统计: 客户端=%d, 任务=%d, 主机=%d\n", clientCount, taskCount, hostCount)
	
	// 创建备份
	fmt.Println("\n=== 创建备份 ===")
	backupService := backup.NewBackupService()
	backupPath, err := backupService.CreateBackup()
	if err != nil {
		fmt.Printf("备份创建失败: %v\n", err)
		return
	}
	
	fmt.Printf("备份创建成功: %s\n", backupPath)
	
	// 检查备份文件大小
	if info, err := os.Stat(backupPath); err != nil {
		fmt.Printf("无法获取备份文件信息: %v\n", err)
	} else {
		fmt.Printf("备份文件大小: %d 字节\n", info.Size())
	}
	
	fmt.Println("\n备份测试完成！")
}