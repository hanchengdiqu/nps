using System;
using System.Threading;
using System.Threading.Tasks;

namespace NpcSdkExample
{
    class Program
    {
        static async Task Main(string[] args)
        {
            Console.WriteLine("=== NPC SDK C# 示例程序 ===");
            Console.WriteLine();

            try
            {
                // 显示版本信息
                string version = NpcSdk.GetVersion();
                Console.WriteLine($"NPC SDK 版本: {version}");
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
                    return;
                }

                // 监控客户端状态
                Console.WriteLine();
                Console.WriteLine("监控客户端状态 (按 'q' 退出):");
                Console.WriteLine("状态说明: 0=断开连接, 1=已连接, 2=连接中, -1=错误");
                Console.WriteLine();

                var cancellationTokenSource = new CancellationTokenSource();
                
                // 启动状态监控任务
                var monitorTask = Task.Run(async () =>
                {
                    while (!cancellationTokenSource.Token.IsCancellationRequested)
                    {
                        var status = NpcSdk.GetClientStatusEnum();
                        var statusText = status switch
                        {
                            NpcSdk.ClientStatus.Disconnected => "断开连接",
                            NpcSdk.ClientStatus.Connected => "已连接",
                            NpcSdk.ClientStatus.Connecting => "连接中",
                            NpcSdk.ClientStatus.Error => "错误",
                            _ => "未知状态"
                        };

                        Console.WriteLine($"[{DateTime.Now:HH:mm:ss}] 客户端状态: {statusText} ({(int)status})");

                        // 显示日志信息
                        string logs = NpcSdk.GetLogs();
                        if (!string.IsNullOrEmpty(logs))
                        {
                            Console.WriteLine($"[{DateTime.Now:HH:mm:ss}] 日志: {logs}");
                        }

                        await Task.Delay(3000, cancellationTokenSource.Token);
                    }
                }, cancellationTokenSource.Token);

                // 等待用户输入
                while (true)
                {
                    var key = Console.ReadKey(true);
                    if (key.KeyChar == 'q' || key.KeyChar == 'Q')
                    {
                        break;
                    }
                }

                // 停止监控
                cancellationTokenSource.Cancel();
                
                try
                {
                    await monitorTask;
                }
                catch (OperationCanceledException)
                {
                    // 正常取消，忽略异常
                }

                // 关闭客户端
                //Console.WriteLine();
                //Console.WriteLine("正在关闭客户端...");
                //NpcSdk.CloseClient();
                //Console.WriteLine("✓ 客户端已关闭");
            }
            catch (DllNotFoundException)
            {
                Console.WriteLine("✗ 错误: 找不到 npc-sdk.dll 文件");
                Console.WriteLine("请确保:");
                Console.WriteLine("1. 已编译生成 npc-sdk.dll");
                Console.WriteLine("2. DLL 文件位于程序目录中");
                Console.WriteLine("3. 使用 64 位编译的 DLL");
            }
            catch (Exception ex)
            {
                Console.WriteLine($"✗ 发生错误: {ex.Message}");
                Console.WriteLine($"详细信息: {ex}");
            }

            Console.WriteLine();
            Console.WriteLine("按任意键退出...");
            Console.ReadKey();
        }
    }
}