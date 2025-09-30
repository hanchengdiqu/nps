using System;
using System.Threading;
using System.Threading.Tasks;

namespace NpcSdkExample
{
    /// <summary>
    /// 异步日志输出测试程序
    /// 专门测试异步启动模式下的实时日志输出功能
    /// </summary>
    class AsyncLogTest
    {
        static async Task Main(string[] args)
        {
            Console.WriteLine("=== NPC SDK 异步日志输出测试 ===");
            Console.WriteLine();

            try
            {
                // 显示版本信息
                string version = NpcSdk.GetVersion();
                Console.WriteLine($"NPC SDK 版本: {version}");
                Console.WriteLine();

                // 测试参数
                string serverAddr = "www.198408.xyz:65203";
                string verifyKey = "abcdefg";
                string connType = "tcp";
                string proxyUrl = "";

                Console.WriteLine("测试参数:");
                Console.WriteLine($"  服务器地址: {serverAddr}");
                Console.WriteLine($"  验证密钥: {verifyKey}");
                Console.WriteLine($"  连接类型: {connType}");
                Console.WriteLine();

                // 设置重连间隔
                Console.WriteLine("设置重连间隔为5秒...");
                NpcSdk.SetReconnectInterval(5);
                Console.WriteLine($"当前重连间隔: {NpcSdk.GetReconnectInterval()}秒");
                Console.WriteLine();

                // 异步启动客户端
                Console.WriteLine("正在异步启动 NPC 客户端...");
                int result = NpcSdk.StartClientByVerifyKeyAsync(serverAddr, verifyKey, connType, proxyUrl);
                
                if (result == 1)
                {
                    Console.WriteLine("✓ 异步客户端启动成功");
                    Console.WriteLine($"自动重连状态: {(NpcSdk.IsAutoReconnectEnabled() == 1 ? "已启用" : "未启用")}");
                    Console.WriteLine();
                }
                else
                {
                    Console.WriteLine($"✗ 异步客户端启动失败，返回码: {result}");
                    return;
                }

                // 实时监控状态和日志
                Console.WriteLine("开始实时监控客户端状态和日志...");
                Console.WriteLine("状态说明: 0=断开连接, 1=已连接, 2=连接中, -1=错误");
                Console.WriteLine("按 'q' 退出监控");
                Console.WriteLine(new string('=', 60));

                var cancellationTokenSource = new CancellationTokenSource();
                
                // 启动实时监控任务
                var monitorTask = Task.Run(async () =>
                {
                    string lastLogs = "";
                    int logCounter = 0;
                    
                    while (!cancellationTokenSource.Token.IsCancellationRequested)
                    {
                        try
                        {
                            // 获取客户端状态
                            var status = NpcSdk.GetClientStatusEnum();
                            var statusText = status switch
                            {
                                NpcSdk.ClientStatus.Disconnected => "断开连接",
                                NpcSdk.ClientStatus.Connected => "已连接",
                                NpcSdk.ClientStatus.Connecting => "连接中",
                                NpcSdk.ClientStatus.Error => "错误",
                                _ => "未知状态"
                            };

                            // 获取日志信息
                            string currentLogs = NpcSdk.GetLogs();
                            
                            // 显示状态（每次都显示）
                            Console.WriteLine($"[{DateTime.Now:HH:mm:ss}] 状态: {statusText} ({(int)status}) | 自动重连: {(NpcSdk.IsAutoReconnectEnabled() == 1 ? "启用" : "禁用")}");
                            
                            // 显示日志（只有当日志内容变化时才显示）
                            if (!string.IsNullOrEmpty(currentLogs) && currentLogs != lastLogs)
                            {
                                logCounter++;
                                Console.WriteLine($"[{DateTime.Now:HH:mm:ss}] 日志 #{logCounter}: {currentLogs}");
                                lastLogs = currentLogs;
                            }
                            else if (!string.IsNullOrEmpty(currentLogs))
                            {
                                // 如果日志内容相同，显示一个简短的指示
                                Console.WriteLine($"[{DateTime.Now:HH:mm:ss}] 日志: (无新内容)");
                            }
                            else
                            {
                                Console.WriteLine($"[{DateTime.Now:HH:mm:ss}] 日志: (暂无日志)");
                            }

                            Console.WriteLine(); // 空行分隔
                            
                            // 等待2秒再次检查（更频繁的检查以获得更实时的日志）
                            await Task.Delay(2000, cancellationTokenSource.Token);
                        }
                        catch (OperationCanceledException)
                        {
                            break;
                        }
                        catch (Exception ex)
                        {
                            Console.WriteLine($"[{DateTime.Now:HH:mm:ss}] 监控异常: {ex.Message}");
                        }
                    }
                }, cancellationTokenSource.Token);

                // 等待用户输入
                while (true)
                {
                    var key = Console.ReadKey(true);
                    if (key.KeyChar == 'q' || key.KeyChar == 'Q')
                    {
                        Console.WriteLine("用户请求退出...");
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
                Console.WriteLine();
                Console.WriteLine("正在关闭客户端...");
                
                // 如果启用了自动重连，先停止它
                if (NpcSdk.IsAutoReconnectEnabled() == 1)
                {
                    Console.WriteLine("停止自动重连...");
                    NpcSdk.StopAutoReconnect();
                }
                
                NpcSdk.CloseClient();
                Console.WriteLine("✓ 客户端已关闭");
            }
            catch (Exception ex)
            {
                Console.WriteLine($"✗ 发生错误: {ex.Message}");
                Console.WriteLine($"详细信息: {ex}");
            }

            Console.WriteLine();
            Console.WriteLine("测试完成，按任意键退出...");
            Console.ReadKey();
        }
    }
}