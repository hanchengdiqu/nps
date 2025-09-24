# NPC SDK DLL 构建说明

本文档说明如何将 Go 语言的 NPC SDK 编译为 C# 可调用的 64 位 DLL。

## 文件结构

```
cmd/npc/
├── sdk.go                    # Go SDK 源代码
├── build-dll-tdm.bat        # TDM-GCC 编译批处理文件
├── test-build.bat           # 简化测试编译脚本
├── npc-sdk.dll              # 生成的 64 位 DLL (编译后)
├── npc-sdk.h                # 生成的 C 头文件 (编译后)
├── csharp-example/          # C# 示例项目
│   ├── NpcSdkExample.csproj # C# 项目文件
│   ├── NpcSdk.cs            # C# SDK 包装类
│   ├── Program.cs           # 示例主程序
│   ├── npc-sdk.dll          # DLL 副本 (需要复制)
│   └── README.md            # C# 项目说明
└── BUILD_INSTRUCTIONS.md    # 本文档
```

## 前置要求

1. **Go 1.16+**: 支持 CGO 的 Go 版本
   - 确保 Go 已正确安装并在 PATH 中
   - 验证: `go version`

2. **TDM-GCC**: 用于编译 DLL
   - 下载: https://jmeubank.github.io/tdm-gcc/
   - 推荐安装到: `C:\TDM-GCC-64`
   - **重要**: 必须将 `C:\TDM-GCC-64\bin` 添加到系统 PATH 环境变量
   - 验证: `gcc --version`

3. **.NET 6.0+**: 用于运行 C# 示例
   - 下载: https://dotnet.microsoft.com/download
   - 验证: `dotnet --version`

### 环境变量配置

编译前需要确保以下环境变量正确设置：

#### 临时设置 (当前会话)
```powershell
# 添加 TDM-GCC 到 PATH
$env:PATH = "C:\TDM-GCC-64\bin;" + $env:PATH

# 启用 CGO
$env:CGO_ENABLED = "1"

# 验证设置
gcc --version
go env CGO_ENABLED
```

#### 永久设置 (推荐)
1. **系统 PATH**: 将 `C:\TDM-GCC-64\bin` 添加到系统环境变量 PATH
2. **CGO 设置**: 添加系统环境变量 `CGO_ENABLED=1`

**设置步骤**:
1. 右键 "此电脑" → "属性" → "高级系统设置"
2. 点击 "环境变量"
3. 在 "系统变量" 中:
   - 编辑 `Path`，添加 `C:\TDM-GCC-64\bin`
   - 新建 `CGO_ENABLED`，值为 `1`
4. 重启命令行窗口

## 编译步骤

### 0. 环境检查 (推荐)

在编译前，建议先检查环境是否正确配置：

```powershell
# 检查 Go 版本
go version

# 检查 GCC 是否可用
gcc --version

# 检查 CGO 是否启用
go env CGO_ENABLED

# 检查 .NET 版本 (可选)
dotnet --version
```

**预期输出**:
- Go: `go version go1.x.x windows/amd64`
- GCC: `gcc.exe (tdm64-1) 10.3.0` 或更高版本
- CGO: `1` (表示已启用)
- .NET: `6.0.x` 或更高版本

### 1. 编译 DLL

在 `cmd/npc` 目录下运行：

#### 推荐方式 (完整脚本)
```cmd
build-dll-tdm.bat
```
- 包含完整的错误检查和用户友好输出
- 显示详细的编译信息和文件大小
- 适合生产环境和分发使用

#### 快速测试方式
```cmd
test-build.bat
```
- 简化的编译脚本，适合快速测试
- 输出较少，适合自动化脚本

#### 手动编译 (高级用户)
```cmd
# 清理旧文件
del npc-sdk.dll npc-sdk.h 2>nul

# 编译 DLL
go build -buildmode=c-shared -ldflags="-s -w" -o npc-sdk.dll sdk.go
```

### 2. 验证编译结果

编译成功后应该生成：
- `npc-sdk.dll` (约 11MB) - 64位动态链接库
- `npc-sdk.h` (约 2KB) - C语言头文件

**验证命令**:
```cmd
dir npc-sdk.*
```

**成功标志**:
- 批处理脚本显示 "Build successful!" 或 "Build completed successfully!"
- 两个文件都存在且大小合理
- 没有编译错误信息

### 3. 测试 C# 集成

```cmd
# 复制 DLL 到 C# 项目
copy npc-sdk.dll csharp-example\

# 编译 C# 项目
cd csharp-example
dotnet build

# 运行示例 (可选)
dotnet run
```

## 编译脚本对比

项目提供了两个编译脚本，根据不同需求选择：

| 特性 | build-dll-tdm.bat | test-build.bat |
|------|-------------------|----------------|
| **推荐用途** | 生产环境、分发 | 快速测试、自动化 |
| **错误检查** | ✅ 完整 | ⚠️ 基础 |
| **输出信息** | ✅ 详细 | ⚠️ 简洁 |
| **GCC 版本显示** | ✅ 是 | ❌ 否 |
| **文件大小显示** | ✅ 是 | ❌ 否 |
| **成功/失败消息** | ✅ 清晰 | ⚠️ 简单 |
| **用户友好性** | ✅ 高 | ⚠️ 中等 |
| **适合新手** | ✅ 是 | ❌ 否 |

**建议**:
- **首次使用**: 使用 `build-dll-tdm.bat`
- **日常开发**: 使用 `build-dll-tdm.bat`
- **CI/CD 自动化**: 可考虑 `test-build.bat`

## 导出的函数

SDK 导出以下 C 函数供 C# 调用：

```c
// 启动客户端
GoInt StartClientByVerifyKey(char* serverAddr, char* verifyKey, char* connType, char* proxyUrl);

// 获取客户端状态
GoInt GetClientStatus(void);

// 关闭客户端
void CloseClient(void);

// 获取版本信息
char* Version(void);

// 获取日志信息
char* Logs(void);
```

## C# 调用示例

```csharp
// 启动客户端
int result = NpcSdk.StartClientByVerifyKey(
    "127.0.0.1:8024",    // 服务器地址
    "your_verify_key",   // 验证密钥
    "tcp",               // 连接类型
    ""                   // 代理URL
);

// 检查状态
var status = NpcSdk.GetClientStatusEnum();
Console.WriteLine($"状态: {status}");

// 获取版本
string version = NpcSdk.GetVersion();
Console.WriteLine($"版本: {version}");

// 关闭客户端
NpcSdk.CloseClient();
```

## 故障排除

### 环境配置问题

#### 1. 找不到 gcc 命令
**错误现象**: `'gcc' is not recognized as an internal or external command`

**解决方案**:
```powershell
# 检查 TDM-GCC 是否安装
dir "C:\TDM-GCC-64\bin\gcc.exe"

# 临时添加到 PATH
$env:PATH = "C:\TDM-GCC-64\bin;" + $env:PATH

# 验证
gcc --version
```

**永久解决**: 将 `C:\TDM-GCC-64\bin` 添加到系统环境变量 PATH

#### 2. CGO 被禁用
**错误现象**: 编译时出现 CGO 相关错误，或 `go env CGO_ENABLED` 返回 `0`

**解决方案**:
```powershell
# 临时启用
$env:CGO_ENABLED = "1"

# 验证
go env CGO_ENABLED  # 应该返回 1
```

**永久解决**: 添加系统环境变量 `CGO_ENABLED=1`

#### 3. 批处理文件报告 "Build partially failed!"
**错误现象**: DLL 和头文件都存在，但批处理文件仍报告失败

**原因**: 文件系统时序问题，批处理文件检查过早

**解决方案**: 
- 使用最新版本的 `build-dll-tdm.bat` (已修复此问题)
- 或手动验证文件是否存在: `dir npc-sdk.*`

### 编译错误

#### 1. Go 模块依赖错误
**错误现象**: `go: module not found` 或依赖版本冲突

**解决方案**:
```cmd
# 更新依赖
go mod tidy

# 清理模块缓存 (如果需要)
go clean -modcache
```

#### 2. 编译器版本不兼容
**错误现象**: 编译过程中出现 C 编译错误

**解决方案**:
- 确保使用 TDM-GCC 10.3.0 或更高版本
- 避免使用 MinGW 的其他发行版，可能存在兼容性问题

#### 3. 内存不足
**错误现象**: 编译过程中崩溃或内存错误

**解决方案**:
- 关闭其他大型程序
- 确保至少有 2GB 可用内存

### 运行时错误

#### 1. 找不到 DLL
**错误现象**: `System.DllNotFoundException: Unable to load DLL 'npc-sdk.dll'`

**解决方案**:
```cmd
# 确保 DLL 在正确位置
copy npc-sdk.dll csharp-example\bin\Debug\net6.0\
```

#### 2. 架构不匹配
**错误现象**: `BadImageFormatException` 或架构相关错误

**解决方案**:
- 确保使用 64 位 .NET 运行时
- 检查项目配置: `<PlatformTarget>x64</PlatformTarget>`

#### 3. 函数调用失败
**错误现象**: `AccessViolationException` 或函数返回异常值

**解决方案**:
- 检查函数签名是否正确
- 确保字符串参数使用 UTF-8 编码
- 验证服务器地址和端口是否正确

### 调试技巧

#### 1. 详细错误信息
```powershell
# 启用详细的 Go 编译输出
go build -v -x -buildmode=c-shared -o npc-sdk.dll sdk.go
```

#### 2. 检查 DLL 导出函数
```cmd
# 使用 dumpbin 查看导出函数 (需要 Visual Studio)
dumpbin /exports npc-sdk.dll

# 或使用 objdump (TDM-GCC 自带)
objdump -p npc-sdk.dll | findstr "Export"
```

#### 3. C# 调试
```csharp
// 添加详细的错误处理
try {
    var result = NpcSdk.StartClientByVerifyKey(...);
    Console.WriteLine($"Result: {result}");
} catch (Exception ex) {
    Console.WriteLine($"Error: {ex.Message}");
    Console.WriteLine($"Stack: {ex.StackTrace}");
}
```

## 注意事项

1. **平台**: 此 DLL 仅适用于 Windows x64
2. **内存管理**: Go 分配的字符串由 Go GC 管理
3. **线程安全**: 避免多线程同时调用 SDK 函数
4. **错误处理**: 建议添加适当的异常处理

## 自动化构建

可以将编译过程集成到 CI/CD 流水线中：

```yaml
# GitHub Actions 示例
- name: Build NPC SDK DLL
  run: |
    cd cmd/npc
    build-dll-tdm.bat
    
- name: Test C# Integration
  run: |
    cd cmd/npc/csharp-example
    dotnet build
    dotnet test
```