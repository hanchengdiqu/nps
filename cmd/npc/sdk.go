/*
C# 调用示例（P/Invoke，Windows）

using System;
using System.Runtime.InteropServices;

internal static class NpcNative
{
    const string Dll = "npc.dll"; // 将 npc.dll 与您的可执行文件放在同一路径或 PATH 中

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
    public static extern int StartClientByVerifyKey(string serverAddr, string verifyKey, string connType, string proxyUrl);

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl)]
    public static extern int GetClientStatus();

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl)]
    public static extern IntPtr Version();

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl)]
    public static extern IntPtr Logs();

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl)]
    public static extern void CloseClient();

    [DllImport(Dll, CallingConvention = CallingConvention.Cdecl)]
    public static extern void FreeCString(IntPtr p);
}

class Demo
{
    static void Main()
    {
        // 1) 启动客户端（示例参数）
        int ok = NpcNative.StartClientByVerifyKey("example.com:8024", "YOUR_VERIFY_KEY", "tcp", "");
        Console.WriteLine($"Start ret={ok}");

        // 2) 读取版本（返回的是 C 分配的 char*，需要 FreeCString 释放）
        IntPtr vPtr = NpcNative.Version();
        string ver = Marshal.PtrToStringAnsi(vPtr);
        NpcNative.FreeCString(vPtr);
        Console.WriteLine($"Version={ver}");

        // 3) 简单轮询状态
        for (int i = 0; i < 10; i++)
        {
            int st = NpcNative.GetClientStatus();
            Console.WriteLine($"Status={st}");
            System.Threading.Thread.Sleep(1000);
        }

        // 4) 拉取内存日志（同样需要释放返回内存）
        IntPtr lPtr = NpcNative.Logs();
        string logs = Marshal.PtrToStringAnsi(lPtr);
        NpcNative.FreeCString(lPtr);
        Console.WriteLine(logs);

        // 5) 关闭客户端
        NpcNative.CloseClient();
    }
}

构建与使用说明（Windows）
- 先安装 mingw-w64，确保 gcc 可用；并启用 CGO。
  PowerShell 示例：
    $env:CGO_ENABLED = "1"
    $env:CC = "gcc"
- 在项目根目录构建 C 共享库（会生成 npc.dll 与 npc.h）：
    go build -buildmode=c-shared -o npc.dll ./cmd/npc
- 在 C# 项目中确保 npc.dll（以及必要的依赖）可被加载：
  将 npc.dll 复制到程序运行目录，或添加到 PATH。
注意事项
- API 调用约定为 Cdecl；字符串参数按 ANSI 传递（CharSet.Ansi）。
- 对于返回的 char*（Version/Logs），必须调用 FreeCString 释放内存。
- 这些接口未加并发保护，多线程调用请在外层自行加锁。
*/

// Package main 提供了将 NPC 客户端以 CGO 方式导出为 C 语言可调用接口的封装。
//
// 本文件中的函数通过 "//export" 指令导出，便于其它语言（如 C/C++/Objective‑C、
// Swift 通过桥接、以及 Java/Android 通过 cgo 生成的 so）直接调用，以便将 nps 客户端
// 嵌入到桌面应用或移动端应用中。
//
// 注意事项：
// 1）被 "//export" 导出的函数，"//export" 必须紧挨着对应的 func，二者之间不能插入空行或其他代码。
// 2）导出的字符串均以 C 的 char* 返回（需要调用方在合适的时机释放内存）。
// 3）本文件中维护一个进程内唯一的客户端实例 cl。重复启动会先关闭旧实例再新建。
// 4）这些接口并未做并发同步保护，如果您的宿主程序可能并发调用，请自行在更高层加锁。
// 5）日志默认写入内存（store logger），可通过 Logs() 拉取最近的日志文本。
package main

/*
#include <stdlib.h>
*/
import (
	"C"
	"unsafe"

	"ehang.io/nps/client"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/version"
	"github.com/astaxie/beego/logs"
)

// cl 保存当前进程中的唯一 TRPClient 实例。
// StartClientByVerifyKey 会根据传入参数创建/替换它，CloseClient 会关闭它。
var cl *client.TRPClient

// StartClientByVerifyKey 通过验证密钥启动 NPC 客户端，并保持与服务端的长连接。
//
// 参数（均为 C 字符串指针，内部将使用 C.GoString 转为 Go 字符串）：
// - serverAddr：服务端地址，形如 "example.com:8024" 或 "http://example.com:8024"。
// - verifyKey：客户端在服务端的验证密钥（对应 nps 后台为客户端生成的密钥）。
// - connType：连接类型，支持 "tcp"、"kcp"、"websocket" 等，取决于项目支持情况。
// - proxyUrl：可选的代理地址，形如 "http://127.0.0.1:1080"，不需要可传空字符串。
//
// 行为：
// - 若已有运行中的客户端实例，将先调用 Close() 关闭旧实例。
// - 使用给定参数创建新的客户端实例，并异步启动（内部自行维护重连等）。
// - 返回值固定为 1，仅表示调用成功发起（不代表与服务端已经建立连接）。
//
// 日志：
// - 函数内部将日志输出器设置为 "store"，日志会被写入内存，便于通过 Logs() 拉取查看。
//
//export StartClientByVerifyKey
func StartClientByVerifyKey(serverAddr, verifyKey, connType, proxyUrl *C.char) C.int {
	// 将 beego logs 输出设置为内存存储，便于宿主应用通过 Logs() 主动获取。
	_ = logs.SetLogger("store")

	// 如果已存在客户端实例，先进行优雅关闭，避免资源泄露或重复连接。
	if cl != nil {
		cl.Close()
	}

	// 创建并启动新的客户端实例。
	cl = client.NewRPClient(
		C.GoString(serverAddr),
		C.GoString(verifyKey),
		C.GoString(connType),
		C.GoString(proxyUrl),
		nil, // 额外配置留空，保持与原逻辑一致。
		60,  // 心跳或内部使用的时间参数，保持与原逻辑一致。
	)
	cl.Start()
	return C.int(1)
}

// GetClientStatus 返回当前客户端状态码（由 client 包维护）。
// 典型状态可参考 client.NowStatus 的定义与含义，通常用于宿主程序轮询状态。
//
//export GetClientStatus
func GetClientStatus() C.int {
	return C.int(client.NowStatus)
}

// CloseClient 关闭并清理当前客户端实例。
// 如果客户端尚未创建或已关闭，则该函数等价于空操作。
//
//export CloseClient
func CloseClient() {
	if cl != nil {
		cl.Close()
	}
}

// Version 返回 NPC 的版本号（以 C 字符串形式）。
// 调用方如为 C/C++，应在合适时机负责释放返回的 C 字符串内存。
//
//export Version
func Version() *C.char {
	return C.CString(version.VERSION)
}

// Logs 返回当前缓存的日志文本（以 C 字符串形式）。
// 该日志来源于 SetLogger("store") 的内存日志存储，主要用于嵌入宿主应用时的可视化输出。
//
//export Logs
func Logs() *C.char {
	return C.CString(common.GetLogMsg())
}

// FreeCString 释放由本模块返回的 C 字符串内存（例如 Version/Logs 返回值）。
// 在 C# / PInvoke 等场景中，拿到 IntPtr 后应调用该函数释放。
//
//export FreeCString
func FreeCString(p *C.char) {
	if p != nil {
		C.free(unsafe.Pointer(p))
	}
}

// main 是为了让 CGO 能够将该包编译为 C 共享库所必须存在的入口。
// 实际上不会被调用。
func main() {
	// Need a main function to make CGO compile package as C shared library
}
