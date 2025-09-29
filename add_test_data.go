package main

import (
	"fmt"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/common"
)

func main() {
	fmt.Println("=== 添加测试数据 ===")
	
	// 获取数据库实例
	db := file.GetDb()
	
	// 创建测试客户端
	client1 := file.NewClient("test_key_001", false, false)
	client1.Id = int(db.JsonDb.GetClientId())
	client1.Remark = "测试客户端1"
	client1.Status = true
	
	client2 := file.NewClient("test_key_002", false, false)
	client2.Id = int(db.JsonDb.GetClientId())
	client2.Remark = "测试客户端2"
	client2.Status = true
	
	// 添加客户端到数据库
	err := db.NewClient(client1)
	if err != nil {
		fmt.Printf("添加客户端1失败: %v\n", err)
	} else {
		fmt.Printf("成功添加客户端1: ID=%d, Key=%s\n", client1.Id, client1.VerifyKey)
	}
	
	err = db.NewClient(client2)
	if err != nil {
		fmt.Printf("添加客户端2失败: %v\n", err)
	} else {
		fmt.Printf("成功添加客户端2: ID=%d, Key=%s\n", client2.Id, client2.VerifyKey)
	}
	
	// 创建测试任务
	task1 := &file.Tunnel{
		Id:       int(db.JsonDb.GetTaskId()),
		Port:     8080,
		Mode:     "tcp",
		Target:   &file.Target{TargetStr: "127.0.0.1:80"},
		Remark:   "测试TCP隧道",
		Status:   true,
		Client:   client1,
		Flow:     &file.Flow{},
	}
	
	task2 := &file.Tunnel{
		Id:       int(db.JsonDb.GetTaskId()),
		Port:     8081,
		Mode:     "http",
		Target:   &file.Target{TargetStr: "127.0.0.1:8080"},
		Remark:   "测试HTTP隧道",
		Status:   true,
		Client:   client2,
		Flow:     &file.Flow{},
	}
	
	// 添加任务到数据库
	err = db.NewTask(task1)
	if err != nil {
		fmt.Printf("添加任务1失败: %v\n", err)
	} else {
		fmt.Printf("成功添加任务1: ID=%d, 端口=%d\n", task1.Id, task1.Port)
	}
	
	err = db.NewTask(task2)
	if err != nil {
		fmt.Printf("添加任务2失败: %v\n", err)
	} else {
		fmt.Printf("成功添加任务2: ID=%d, 端口=%d\n", task2.Id, task2.Port)
	}
	
	// 创建测试主机
	host1 := &file.Host{
		Id:       int(db.JsonDb.GetHostId()),
		Host:     "test1.example.com",
		Location: "/",
		Scheme:   "http",
		Remark:   "测试主机1",
		Client:   client1,
		Target:   &file.Target{TargetStr: "127.0.0.1:8080"},
		Flow:     &file.Flow{},
	}
	
	host2 := &file.Host{
		Id:       int(db.JsonDb.GetHostId()),
		Host:     "test2.example.com",
		Location: "/api",
		Scheme:   "https",
		Remark:   "测试主机2",
		Client:   client2,
		Target:   &file.Target{TargetStr: "127.0.0.1:8081"},
		Flow:     &file.Flow{},
	}
	
	// 添加主机到数据库
	err = db.NewHost(host1)
	if err != nil {
		fmt.Printf("添加主机1失败: %v\n", err)
	} else {
		fmt.Printf("成功添加主机1: ID=%d, 主机=%s\n", host1.Id, host1.Host)
	}
	
	err = db.NewHost(host2)
	if err != nil {
		fmt.Printf("添加主机2失败: %v\n", err)
	} else {
		fmt.Printf("成功添加主机2: ID=%d, 主机=%s\n", host2.Id, host2.Host)
	}
	
	// 显示最终统计
	fmt.Println("\n=== 最终数据统计 ===")
	clientCount := common.GeSynctMapLen(db.JsonDb.Clients)
	taskCount := common.GeSynctMapLen(db.JsonDb.Tasks)
	hostCount := common.GeSynctMapLen(db.JsonDb.Hosts)
	
	fmt.Printf("客户端数量: %d\n", clientCount)
	fmt.Printf("任务数量: %d\n", taskCount)
	fmt.Printf("主机数量: %d\n", hostCount)
	
	fmt.Println("\n测试数据添加完成！")
}