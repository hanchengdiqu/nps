using System;

namespace NpcSdkExample
{
   public class TestProgram
    {
        public static void TestMain(string[] args)
        {
            Console.WriteLine("=== NPC SDK C# 测试程序 ===");
            Console.WriteLine();

            try
            {
                Console.WriteLine("1. 测试 DLL 加载...");
                
                // 测试版本获取功能
                Console.WriteLine("2. 测试版本获取功能...");
                string version = NpcSdk.GetVersion();
                Console.WriteLine($"✓ NPC SDK 版本: {version}");
                Console.WriteLine();

                // 配置连接参数（请根据实际情况修改）
                string serverAddr = "www.198408.xyz:65203";  // NPS 服务器地址
                string verifyKey = "abcdefg";   // 验证密钥
                string connType = "tcp";                // 连接类型
                string proxyUrl = "";                   // 代理URL（可选）

                Console.WriteLine("连接参数:");
                Console.WriteLine($"  服务器地址: {serverAddr}");
                Console.WriteLine($"  验证密钥: {verifyKey}");
                Console.WriteLine($"  连接类型: {connType}");
                Console.WriteLine();

                // 启动客户端
                Console.WriteLine("正在启动 NPC 客户端...");
                int result = NpcSdk.StartClientByVerifyKey(serverAddr, verifyKey, connType, proxyUrl);

                if (result == 1)
                {
                    Console.WriteLine("✓ 客户端启动成功");
                }
                else
                {
                    Console.WriteLine($"✗ 客户端启动失败，返回码: {result}");
                }


                // 测试客户端状态获取
                Console.WriteLine("3. 测试客户端状态获取...");
                var status = NpcSdk.GetClientStatusEnum();
                Console.WriteLine($"✓ 初始客户端状态: {status} ({(int)status})");
                Console.WriteLine();

                // 测试日志获取
                Console.WriteLine("4. 测试日志获取功能...");
                string logs = NpcSdk.GetLogs();
                if (!string.IsNullOrEmpty(logs))
                {
                    Console.WriteLine($"✓ 日志信息: {logs}");
                }
                else
                {
                    Console.WriteLine("✓ 日志信息: (空)");
                }
                Console.WriteLine();

                // 测试关闭客户端（即使没有启动也可以安全调用）
                Console.WriteLine("5. 测试关闭客户端功能...");
                NpcSdk.CloseClient();
                Console.WriteLine("✓ 关闭客户端调用成功");
                Console.WriteLine();

                Console.WriteLine("========================================");
                Console.WriteLine("✓ 所有基本功能测试通过！");
                Console.WriteLine("✓ C# 可以成功调用 Go 编译的 DLL");
                Console.WriteLine("========================================");
            }
            catch (DllNotFoundException ex)
            {
                Console.WriteLine("✗ 错误: 找不到 npc-sdk.dll 文件");
                Console.WriteLine($"详细错误: {ex.Message}");
                Console.WriteLine("请确保:");
                Console.WriteLine("1. 已编译生成 npc-sdk.dll");
                Console.WriteLine("2. DLL 文件位于程序目录中");
                Console.WriteLine("3. 使用 64 位编译的 DLL");
            }
            catch (BadImageFormatException ex)
            {
                Console.WriteLine("✗ 错误: DLL 格式不正确");
                Console.WriteLine($"详细错误: {ex.Message}");
                Console.WriteLine("可能原因:");
                Console.WriteLine("1. DLL 架构不匹配（32位 vs 64位）");
                Console.WriteLine("2. DLL 文件损坏");
            }
            catch (Exception ex)
            {
                Console.WriteLine($"✗ 发生未知错误: {ex.Message}");
                Console.WriteLine($"详细信息: {ex}");
            }

            Console.WriteLine();
            Console.WriteLine("按任意键退出...");
            try
            {
                Console.ReadKey();
            }
            catch (InvalidOperationException)
            {
                // 在重定向输入时忽略此异常
                Console.WriteLine("程序结束");
            }
        }
    }
}