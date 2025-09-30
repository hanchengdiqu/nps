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
        /// 使用默认配置初始化客户端
        /// 服务器地址: www.198408.xyz:65203
        /// 验证密钥: abcdefg
        /// 连接类型: tcp
        /// </summary>
        /// <returns>成功返回1，失败返回其他值</returns>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern int InitDef();

        /// <summary>
        /// 使用自定义验证密钥初始化客户端
        /// 服务器地址: www.198408.xyz:65203 (默认)
        /// 连接类型: tcp (默认)
        /// </summary>
        /// <param name="verifyKey">自定义验证密钥</param>
        /// <returns>成功返回1，失败返回其他值</returns>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        public static extern int InitDefWithKey([MarshalAs(UnmanagedType.LPStr)] string verifyKey);

        /// <summary>
        /// 异步启动客户端（新增功能）
        /// 立即返回，不阻塞主线程，自动启用重连功能
        /// </summary>
        /// <param name="serverAddr">服务器地址，例如: "127.0.0.1:8024"</param>
        /// <param name="verifyKey">验证密钥</param>
        /// <param name="connType">连接类型，例如: "tcp"</param>
        /// <param name="proxyUrl">代理URL，可以为空字符串</param>
        /// <returns>成功返回1，失败返回其他值</returns>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Ansi)]
        public static extern int StartClientByVerifyKeyAsync(
            [MarshalAs(UnmanagedType.LPStr)] string serverAddr,
            [MarshalAs(UnmanagedType.LPStr)] string verifyKey,
            [MarshalAs(UnmanagedType.LPStr)] string connType,
            [MarshalAs(UnmanagedType.LPStr)] string proxyUrl
        );

        /// <summary>
        /// 停止自动重连
        /// </summary>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void StopAutoReconnect();

        /// <summary>
        /// 设置重连间隔（秒）
        /// </summary>
        /// <param name="seconds">重连间隔秒数</param>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void SetReconnectInterval(int seconds);

        /// <summary>
        /// 检查是否启用了自动重连
        /// </summary>
        /// <returns>启用返回1，未启用返回0</returns>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern int IsAutoReconnectEnabled();

        /// <summary>
        /// 获取当前重连间隔（秒）
        /// </summary>
        /// <returns>重连间隔秒数</returns>
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern int GetReconnectInterval();

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

        public static ClientStatus GetClientStatusEnum()
        {
            int status = GetClientStatus();
            switch (status)
            {
                case 0:
                    return ClientStatus.Disconnected;
                case 1:
                    return ClientStatus.Connected;
                case 2:
                    return ClientStatus.Connecting;
                default:
                    return ClientStatus.Error;
            }
        }
    }
}