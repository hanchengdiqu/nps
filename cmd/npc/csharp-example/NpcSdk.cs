using System;
using System.Runtime.InteropServices;

namespace NpcSdkExample
{
    /// <summary>
    /// NPC SDK C# 包装类
    /// 用于调用 Go 编译的 npc-sdk.dll
    /// </summary>
    public static class NpcSdk
    {
        private const string DllName = "npc-sdk.dll";

        /// <summary>
        /// 通过验证密钥启动客户端
        /// </summary>
        /// <param name="serverAddr">服务器地址，例如: "127.0.0.1:8024"</param>
        /// <param name="verifyKey">验证密钥</param>
        /// <param name="connType">连接类型，例如: "tcp"</param>
        /// <param name="proxyUrl">代理URL，可以为空字符串</param>
        /// <returns>成功返回1，失败返回其他值</returns>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        public static extern int StartClientByVerifyKey(
            [MarshalAs(UnmanagedType.LPStr)] string serverAddr,
            [MarshalAs(UnmanagedType.LPStr)] string verifyKey,
            [MarshalAs(UnmanagedType.LPStr)] string connType,
            [MarshalAs(UnmanagedType.LPStr)] string proxyUrl
        );

        /// <summary>
        /// 获取客户端状态
        /// </summary>
        /// <returns>客户端状态码</returns>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern int GetClientStatus();

        /// <summary>
        /// 关闭客户端
        /// </summary>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void CloseClient();

        /// <summary>
        /// 获取版本信息
        /// </summary>
        /// <returns>版本字符串指针</returns>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        private static extern IntPtr Version();

        /// <summary>
        /// 获取日志信息
        /// </summary>
        /// <returns>日志字符串指针</returns>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        private static extern IntPtr Logs();

        /// <summary>
        /// 获取版本信息（C# 友好的方法）
        /// </summary>
        /// <returns>版本字符串</returns>
        public static string GetVersion()
        {
            IntPtr ptr = Version();
            if (ptr == IntPtr.Zero)
                return string.Empty;
            
            string result = Marshal.PtrToStringAnsi(ptr) ?? string.Empty;
            // 注意：Go 分配的内存需要由 Go 的垃圾回收器处理，这里不需要手动释放
            return result;
        }

        /// <summary>
        /// 获取日志信息（C# 友好的方法）
        /// </summary>
        /// <returns>日志字符串</returns>
        public static string GetLogs()
        {
            IntPtr ptr = Logs();
            if (ptr == IntPtr.Zero)
                return string.Empty;
            
            string result = Marshal.PtrToStringAnsi(ptr) ?? string.Empty;
            // 注意：Go 分配的内存需要由 Go 的垃圾回收器处理，这里不需要手动释放
            return result;
        }

        /// <summary>
        /// 客户端状态枚举
        /// </summary>
        public enum ClientStatus
        {
            Disconnected = 0,
            Connected = 1,
            Connecting = 2,
            Error = -1
        }

        /// <summary>
        /// 获取客户端状态（枚举形式）
        /// </summary>
        /// <returns>客户端状态枚举</returns>
        public static ClientStatus GetClientStatusEnum()
        {
            int status = GetClientStatus();
            return status switch
            {
                0 => ClientStatus.Disconnected,
                1 => ClientStatus.Connected,
                2 => ClientStatus.Connecting,
                _ => ClientStatus.Error
            };
        }
    }
}