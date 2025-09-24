using System;
using System.Runtime.InteropServices;

namespace NpcSdkExample
{
    public class BasicTest
    {
        public static void Main(string[] args)
        {
            Console.WriteLine("=== C# DLL 基本功能测试 ===");
            
            try
            {
                // 测试版本获取
                Console.WriteLine("1. 测试版本获取...");
                string version = NpcSdk.GetVersion();
                Console.WriteLine($"✓ SDK 版本: {version}");
                
                // 测试状态获取
                Console.WriteLine("\n2. 测试状态获取...");
                int status = NpcSdk.GetClientStatus();
                Console.WriteLine($"✓ 客户端状态: {status}");
                
                // 测试日志获取
                Console.WriteLine("\n3. 测试日志获取...");
                string logs = NpcSdk.GetLogs();
                Console.WriteLine($"✓ 日志长度: {logs.Length} 字符");
                
                // 测试关闭客户端
                Console.WriteLine("\n4. 测试关闭客户端...");
                NpcSdk.CloseClient();
                Console.WriteLine("✓ 关闭客户端成功");
                
                Console.WriteLine("\n========================================");
                Console.WriteLine("✓ 所有基本功能测试通过！");
                Console.WriteLine("✓ C# 可以成功调用 Go 编译的 DLL");
                Console.WriteLine("========================================");
            }
            catch (DllNotFoundException ex)
            {
                Console.WriteLine($"❌ DLL 未找到: {ex.Message}");
            }
            catch (BadImageFormatException ex)
            {
                Console.WriteLine($"❌ DLL 格式错误: {ex.Message}");
            }
            catch (Exception ex)
            {
                Console.WriteLine($"❌ 其他错误: {ex.Message}");
            }
        }
    }
}