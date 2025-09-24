# NPC SDK DLL 编译安装指南

本指南将帮助您将 NPC (NPS 客户端) 编译为 C# 可调用的 DLL 文件。

## 前置要求

### 1. Go 语言环境

确保已安装 Go 1.16 或更高版本：

```bash
go version
```

如果未安装，请从 [Go 官网](https://golang.org/dl/) 下载并安装。

### 2. C 编译器

CGO 需要 C 编译器支持。请选择以下任一选项：

#### Windows 选项 1: MinGW-w64 (推荐)

1. 访问 [MinGW-w64 下载页面](https://www.mingw-w64.org/downloads/)
2. 选择 "MingW-W64-builds" 或使用包管理器：
   ```bash
   # 使用 Chocolatey
   choco install mingw
   
   # 使用 Scoop
   scoop install gcc
   ```
3. 确保 `gcc` 在 PATH 环境变量中

#### Windows 选项 2: TDM-GCC

1. 访问 [TDM-GCC 官网](https://jmeubank.github.io/tdm-gcc/)
2. 下载并安装最新版本
3. 安装时选择添加到 PATH

#### Windows 选项 3: Visual Studio Build Tools

1. 访问 [Visual Studio 下载页面](https://visualstudio.microsoft.com/downloads/)
2. 下载 "Build Tools for Visual Studio"
3. 安装时选择 "C++ build tools"

#### Linux

```bash
# Ubuntu/Debian
sudo apt-get install build-essential

# CentOS/RHEL/Fedora
sudo yum install gcc gcc-c++ make
# 或 (较新版本)
sudo dnf install gcc gcc-c++ make
```

#### macOS

```bash
# 安装 Xcode Command Line Tools
xcode-select --install

# 或使用 Homebrew
brew install gcc
```

## 编译步骤

### 方法 1: 使用构建脚本 (推荐)

#### Windows
```bash
.\build-dll.bat
```

#### Linux/macOS
```bash
chmod +x build-dll.sh
./build-dll.sh
```

### 方法 2: 手动编译

#### Windows
```bash
set CGO_ENABLED=1
set GOOS=windows
mkdir dll-output
go build -buildmode=c-shared -o dll-output\npc-sdk.dll cmd\npc\sdk.go
```

#### Linux
```bash
export CGO_ENABLED=1
mkdir -p dll-output
go build -buildmode=c-shared -o dll-output/npc-sdk.so cmd/npc/sdk.go
```

#### macOS
```bash
export CGO_ENABLED=1
mkdir -p dll-output
go build -buildmode=c-shared -o dll-output/npc-sdk.dylib cmd/npc/sdk.go
```

## 编译输出

编译成功后，`dll-output` 目录将包含：

- **Windows**: `npc-sdk.dll` 和 `npc-sdk.h`
- **Linux**: `npc-sdk.so` 和 `npc-sdk.h`
- **macOS**: `npc-sdk.dylib` 和 `npc-sdk.h`

## 使用 C# 项目

### 1. 复制文件

将生成的共享库文件复制到您的 C# 项目输出目录。

### 2. 运行示例

```bash
cd csharp-example
dotnet build
dotnet run
```

### 3. 集成到您的项目

1. 将 `NpcSdk.cs` 复制到您的项目中
2. 确保共享库文件在程序运行目录
3. 参考 `Program.cs` 中的使用示例

## 故障排除

### 常见错误

#### 1. "gcc not found"
```
cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%
```

**解决方案**: 安装 C 编译器并确保在 PATH 中。

#### 2. "go: cannot find main module"
```
go: cannot find main module, but found .git/config in C:\git\nps
```

**解决方案**: 确保在项目根目录执行命令，且 `go.mod` 文件存在。

#### 3. "DllNotFoundException" (C# 运行时)
```
System.DllNotFoundException: Unable to load DLL 'npc-sdk.dll'
```

**解决方案**: 
- 确保 DLL 文件在程序目录
- 检查平台匹配 (x64/x86)
- 安装 Visual C++ Redistributable

#### 4. 编译时内存不足

**解决方案**: 
```bash
# 增加 Go 编译器内存限制
set GOGC=off
go build -buildmode=c-shared -o dll-output\npc-sdk.dll cmd\npc\sdk.go
```

### 验证安装

#### 检查 Go 环境
```bash
go env CGO_ENABLED
# 应该输出: 1
```

#### 检查 C 编译器
```bash
gcc --version
# 或
cl
# 或
clang --version
```

#### 测试 CGO
```bash
# 创建测试文件
echo 'package main; import "C"; func main() {}' > test_cgo.go
go build test_cgo.go
# 如果成功编译，说明 CGO 环境正常
rm test_cgo.go test_cgo.exe
```

## 性能优化

### 编译优化

```bash
# 启用优化编译
go build -buildmode=c-shared -ldflags="-s -w" -o dll-output\npc-sdk.dll cmd\npc\sdk.go
```

### 减小文件大小

```bash
# 使用 UPX 压缩 (可选)
upx --best dll-output\npc-sdk.dll
```

## 交叉编译

### 为其他平台编译

```bash
# 为 Linux 编译 (在 Windows 上)
set GOOS=linux
set GOARCH=amd64
go build -buildmode=c-shared -o dll-output/npc-sdk.so cmd/npc/sdk.go

# 为 macOS 编译 (在 Windows 上)
set GOOS=darwin
set GOARCH=amd64
go build -buildmode=c-shared -o dll-output/npc-sdk.dylib cmd/npc/sdk.go
```

## 支持

如果遇到问题，请：

1. 检查本指南的故障排除部分
2. 确认所有前置要求已满足
3. 查看编译输出的详细错误信息
4. 在项目 Issues 中搜索相关问题

## 许可证

本项目遵循与 NPS 项目相同的许可证。