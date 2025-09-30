#include <stdio.h>
#include <stdlib.h>
#include <windows.h>
#include "npc-sdk.h"

int main() {
    printf("=== NPC SDK 异步功能测试 ===\n\n");
    
    // 测试版本信息
    char* version = Version();
    printf("SDK版本: %s\n", version);
    free(version);
    
    // 测试初始重连状态
    printf("初始自动重连状态: %s\n", IsAutoReconnectEnabled() ? "启用" : "禁用");
    printf("初始重连间隔: %d 秒\n", GetReconnectInterval());
    
    // 设置重连间隔
    printf("\n设置重连间隔为10秒...\n");
    if (SetReconnectInterval(10)) {
        printf("重连间隔设置成功: %d 秒\n", GetReconnectInterval());
    } else {
        printf("重连间隔设置失败\n");
    }
    
    // 测试异步启动（使用测试服务器地址）
    printf("\n启动异步客户端...\n");
    int result = StartClientByVerifyKeyAsync("127.0.0.1:8080", "test_key", "tcp", "");
    if (result) {
        printf("异步客户端启动成功\n");
        printf("自动重连状态: %s\n", IsAutoReconnectEnabled() ? "启用" : "禁用");
    } else {
        printf("异步客户端启动失败\n");
    }
    
    // 监控状态5秒
    printf("\n监控连接状态5秒...\n");
    for (int i = 0; i < 5; i++) {
        int status = GetClientStatus();
        printf("第%d秒 - 连接状态: %s\n", i+1, status ? "已连接" : "未连接");
        Sleep(1000);
    }
    
    // 停止自动重连
    printf("\n停止自动重连...\n");
    StopAutoReconnect();
    printf("自动重连状态: %s\n", IsAutoReconnectEnabled() ? "启用" : "禁用");
    
    // 关闭客户端
    printf("\n关闭客户端...\n");
    CloseClient();
    
    printf("\n测试完成！\n");
    return 0;
}