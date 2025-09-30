# npc sdk文档

## 基础函数

```
命令行模式启动客户端（阻塞模式）
从v0.26.10开始，此函数会阻塞，直到客户端退出返回，请自行管理是否重连
p0->连接地址
p1->vkey
p2->连接类型（tcp or udp）
p3->连接代理

extern GoInt StartClientByVerifyKey(char* p0, char* p1, char* p2, char* p3);

查看当前启动的客户端状态，在线为1，离线为0
extern GoInt GetClientStatus();

关闭客户端
extern void CloseClient();

获取当前客户端版本
extern char* Version();

获取日志，实时更新
extern char* Logs();
```

## 异步启动和自动重连功能（新增）

```
异步启动客户端（非阻塞模式）
此函数立即返回，客户端在后台运行，支持自动重连
参数与StartClientByVerifyKey相同
返回值：成功返回1，失败返回0

extern GoInt StartClientByVerifyKeyAsync(char* serverAddr, char* verifyKey, char* connType, char* proxyUrl);

停止自动重连
停止当前的自动重连机制并关闭异步客户端

extern void StopAutoReconnect();

设置重连间隔
设置自动重连的时间间隔（秒）
参数：seconds - 重连间隔秒数（必须>=1）
返回值：成功返回1，失败返回0

extern GoInt SetReconnectInterval(GoInt seconds);

检查自动重连状态
返回值：启用返回1，禁用返回0

extern GoInt IsAutoReconnectEnabled();

获取当前重连间隔
返回值：当前设置的重连间隔秒数

extern GoInt GetReconnectInterval();
```

## 使用示例

### 传统阻塞模式
```c
// 阻塞式启动，需要在单独线程中运行
StartClientByVerifyKey("server:port", "vkey", "tcp", "");
```

### 异步模式（推荐）
```c
// 设置重连间隔为10秒
SetReconnectInterval(10);

// 异步启动，立即返回
if (StartClientByVerifyKeyAsync("server:port", "vkey", "tcp", "")) {
    printf("客户端启动成功\n");
    
    // 监控连接状态
    while (1) {
        if (GetClientStatus() == 1) {
            printf("已连接\n");
        } else {
            printf("连接断开，自动重连中...\n");
        }
        Sleep(5000);
    }
    
    // 停止自动重连
    StopAutoReconnect();
}
```

## 注意事项

1. **向后兼容性**：原有的 `StartClientByVerifyKey` 函数保持不变，现有代码无需修改
2. **异步安全**：`StartClientByVerifyKeyAsync` 使用独立的客户端实例，不会影响同步版本
3. **自动重连**：异步模式下，连接断开后会自动尝试重连，重连间隔可配置
4. **状态监控**：可通过 `GetClientStatus()` 实时监控连接状态
5. **资源管理**：使用 `StopAutoReconnect()` 正确停止自动重连并释放资源
