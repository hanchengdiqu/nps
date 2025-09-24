# NPC SDK C# 示例项目

这个项目演示了如何在 C# 中调用 Go 编译的 NPC SDK DLL。

## 前置要求

1. **TDM-GCC**: 用于编译 Go 代码为 DLL
   - 下载地址: https://jmeubank.github.io/tdm-gcc/
   - 安装后确保 `gcc` 命令在 PATH 中

2. **.NET 6.0 或更高版本**: 用于运行 C# 示例
   - 下载地址: https://dotnet.microsoft.com/download

3. **Go 1.16 或更高版本**: 用于编译 SDK
   - 下载地址: https://golang.org/dl/

## 编译步骤

### 1. 编译 Go SDK 为 DLL

在 `cmd/npc` 目录下运行编译批处理文件：

```cmd
cd c:\git\nps\cmd\npc
build-dll-tdm.bat
```

编译成功后会生成：
- `npc-sdk.dll` - 64位 DLL 文件
- `npc-sdk.h` - C 头文件

### 2. 复制 DLL 到 C# 项目

将生成的 `npc-sdk.dll` 复制到 `csharp-example` 目录：

```cmd
copy npc-sdk.dll csharp-example\
```

### 3. 编译并运行 C# 示例

```cmd
cd csharp-example
dotnet build
dotnet run
```

## 项目结构

```
csharp-example/
├── NpcSdkExample.csproj  # C# 项目文件
├── NpcSdk.cs             # NPC SDK C# 包装类
├── Program.cs            # 示例主程序
├── npc-sdk.dll          # Go 编译的 DLL (需要复制)
└── README.md            # 本说明文件
```

## API 说明

### NpcSdk 类方法

- `StartClientByVerifyKey(serverAddr, verifyKey, connType, proxyUrl)` - 启动客户端
- `GetClientStatus()` - 获取客户端状态
- `GetClientStatusEnum()` - 获取客户端状态（枚举形式）
- `CloseClient()` - 关闭客户端
- `GetVersion()` - 获取版本信息
- `GetLogs()` - 获取日志信息

### 客户端状态

- `0` (Disconnected) - 断开连接
- `1` (Connected) - 已连接
- `2` (Connecting) - 连接中
- `-1` (Error) - 错误

## 使用示例

```csharp
// 启动客户端
int result = NpcSdk.StartClientByVerifyKey(
    "127.0.0.1:8024",    // 服务器地址
    "your_verify_key",   // 验证密钥
    "tcp",               // 连接类型
    ""                   // 代理URL（可选）
);

// 检查状态
var status = NpcSdk.GetClientStatusEnum();
Console.WriteLine($"客户端状态: {status}");

// 获取版本
string version = NpcSdk.GetVersion();
Console.WriteLine($"版本: {version}");

// 关闭客户端
NpcSdk.CloseClient();
```

## 注意事项

1. **平台要求**: 此 DLL 为 64 位版本，需要在 64 位环境下运行
2. **内存管理**: Go 分配的字符串内存由 Go 的垃圾回收器管理，C# 端不需要手动释放
3. **线程安全**: 请确保在同一时间只有一个线程调用 SDK 方法
4. **错误处理**: 建议在调用 SDK 方法时添加适当的异常处理

## 故障排除

### 找不到 DLL 文件
- 确保 `npc-sdk.dll` 在程序目录中
- 检查 DLL 是否为 64 位版本

### 编译失败
- 确保 TDM-GCC 已正确安装并在 PATH 中
- 检查 Go 版本是否支持 CGO
- 确保所有依赖包都已下载

### 运行时错误
- 检查服务器地址和验证密钥是否正确
- 确保 NPS 服务器正在运行
- 查看日志信息获取详细错误