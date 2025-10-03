# NPC 客户端（npc）启动与运行业务逻辑说明

> 适用文件：`cmd/npc/npc.go`
>
> 目标：帮助开发者快速理解 npc 客户端的启动入口、服务管理、命令行子命令以及核心运行流程，便于排障与二次开发。

---

## 概览

npc 是 nps 项目的客户端进程，支持两种主要运行模式：
- 直连模式：通过命令行参数 `-server` 与 `-vkey` 直接连接服务端。
- 配置文件模式：通过 `-config` 指定配置文件，从文件启动所有转发/隧道任务。

同时支持“密钥直连/本地模式”（`-password` 等），可不依赖服务端在本地快速开启 p2p/端口转发服务；并可注册为系统服务（Windows Service、systemd、SysV）。

---

## 启动入口：`main()`

`main()` 的职责：
- 解析命令行参数，初始化日志与可选的性能分析（pprof）。
- 构造跨平台系统服务配置，并将非服务控制动词的参数透传给服务进程。
- 处理一次性子命令（如 `install`/`start`/`stop`/`uninstall`/`nat`/`status`/`update`）。
- 在服务框架可用时以服务方式运行；不可用时退化为前台运行并阻塞。

关键行为：
- 默认日志路径通过 `common.GetNpcLogPath()` 获取；Windows 平台会将路径中的 `\` 转义为 `\\` 以适配日志后端。
- `-debug=true`（默认）时日志输出到控制台；服务模式下会强制附加参数 `-debug=false`，将日志输出到文件。
- 非 Windows 平台注入 systemd 与 SysV 启动脚本模板，并声明网络依赖。

---

## 命令行参数一览（核心）

- `-server`：服务端地址，`ip:port`。
- `-config`：客户端配置文件路径。
- `-vkey`：验证密钥，和服务端匹配。
- `-type`：连接类型，`tcp`（默认）或 `kcp`。
- `-proxy`：连接服务端使用的 socks5 代理，例如 `socks5://user:pass@127.0.0.1:1080`。
- `-log`：日志输出方式（`stdout|file`），真实生效受 `-debug` 控制。
- `-log_level`：日志级别 `0~7`，默认 `7`。
- `-pprof`：开启 pprof，形如 `ip:port`。
- `-disconnect_timeout`：心跳超时次数阈值，达到后断开客户端，默认 `60`。
- P2P/本地模式相关：
  - `-password`：开启“密钥直连/本地模式”的开关。
  - `-local_type`：本地服务类型，默认 `p2p`。
  - `-target`：本地模式目标，例如 `127.0.0.1:22`。
  - `-local_port`：本地监听端口，默认 `2000`。
- 注册/NAT：
  - `-time`：向服务端注册本机 IP 的持续时间（小时），默认 `2`。
  - `-stun_addr`：STUN 服务器地址，默认 `stun.stunprotocol.org:3478`。
- 其他：
  - `-log_path`：日志文件路径（`-debug=false` 时使用）。
  - `-debug`：是否调试模式（控制台输出），默认 `true`。
  - `-version`：打印版本后退出。

环境变量缺省：
- 未显式指定时，`-server` 与 `-vkey` 会尝试读取环境变量：
  - `NPC_SERVER_ADDR`
  - `NPC_SERVER_VKEY`

---

## 系统服务管理

服务名：`NpxClient`（Windows 显示为“npx客户端”）。

- 在服务模式下，`main()` 将除控制动词（`install/start/stop/uninstall/restart`）外的参数原样透传到服务进程，并强制追加 `-debug=false`。
- 非 Windows 平台：
  - `Dependencies`：`Requires=network.target`，`After=network-online.target syslog.target`。
  - 注入 `SystemdScript`、`SysvScript` 模板。
- 子命令：
  - `install`：先 `stop`/`uninstall`，再安装并启动；在 SysV 平台创建 `/etc/rc.d/S90NpxClient` 与 `/etc/rc.d/K02NpxClient` 软链接。
  - `start|stop|restart`：SysV 通过 `/etc/init.d/NpxClient` 执行；其他平台用通用 service 控制。
  - `uninstall`：卸载服务并清理 SysV 软链接。

---

## 一次性子命令

- `status -config=/path/to/conf`：查询任务状态（调用 `client.GetTaskStatus`）。
- `register`：注册本机 IP 到服务端（`client.RegisterLocalIp`）。
- `update`：在线更新 npc 可执行文件（`install.UpdateNpc`）。
- `nat`：通过 STUN 探测 NAT 类型与公网地址。

执行完毕后，进程退出（除服务控制以外）。

---

## 服务生命周期（`npc` 结构体）

- `Start()`：启动后台 goroutine，执行 `p.run()`，快速返回。
- `Stop()`：关闭退出通道；交互模式（前台运行）下直接 `os.Exit(0)`。
- `run()`：
  - `defer` 保护：捕获 `panic`，打印调用栈并避免进程异常退出。
  - 调用顶层 `run()` 启动主逻辑。
  - 阻塞等待退出通道，收到后打印 `stop...` 并返回。

---

## 核心运行流程：`run()`

1) 初始化 pprof（若设置 `-pprof`）。
2) 判断是否为“密钥直连/本地模式”（`-password` 非空）：
   - 构造最小化的 `CommonConfig` 与 `LocalServer`：
     - `CommonConfig`：包含服务端地址（`Server`）、验证密钥（`VKey`）、连接类型（`Tp`）。
     - `LocalServer`：包含本地类型（`Type`）、密码（`Password`）、目标（`Target`）、端口（`Port`）。
   - 为满足 `StartLocalServer` 的依赖，最小填充 `Client.Cnf` 结构体。
   - 启动本地服务：`client.StartLocalServer(localServer, commonConfig)`；随后返回（不再进入直连/配置模式）。
3) 常规模式：
   - 若命令行未提供 `-server` 或 `-vkey`，尝试从环境变量填充。
   - 打印客户端版本与核心版本：`version.VERSION` 与 `version.GetVersion()`。
   - 分支逻辑：
     - 直连模式：当 `-server` 与 `-vkey` 提供且未指定 `-config`：
       - 启动一个重连循环 goroutine：
         - `client.NewRPClient(server, vkey, type, proxy, nil, disconnectTime).Start()` 阻塞运行。
         - 连接关闭后等待 5 秒，打印 “Client closed! It will be reconnected in five seconds”，然后重连。
     - 配置文件模式：否则（含未提供 `-server`/`-vkey` 或指定了 `-config`）
       - 若未指定 `-config`，使用默认路径：`common.GetConfigPath()`。
       - 启动：`client.StartFromFile(configPath)`。

---

## 关键调用关系（概念层）

- `client.NewRPClient(...).Start()`：与服务端建立长连接，处理心跳、注册、转发与断线重连（断开后由外层循环重试）。
- `client.StartLocalServer(localServer, commonConfig)`：在本地开启 p2p/端口转发等小型服务，依据 `LocalServer.Type` 与 `Password/Target/Port` 的组合运行。
- `client.StartFromFile(configPath)`：读取客户端配置文件，批量启动隧道/任务。
- `common.InitPProfFromArg(pprofAddr)`：按地址开启 pprof HTTP 采样。
- `install.*` 与 `service.Control(...)`：负责安装/更新/控制系统服务。

---

## 时序（文字版）

- 解析参数 → 初始化日志 → 构造服务配置 → 处理子命令（如安装/控制）→ 进入服务框架。
- 服务启动时（`Start`）异步执行 `run()`：
  - pprof（可选）→ 判断是否本地模式：
    - 是：构造最小配置并启动本地服务，返回。
    - 否：尝试从环境变量补齐 server/vkey → 打印版本 →
      - 直连：启动重连循环并进入 `RPClient.Start()` 阻塞。
      - 配置：确定 `configPath` 并 `StartFromFile()`。
- 收到服务停止事件时，关闭退出通道并优雅退出。

---

## 平台差异与日志行为

- Windows：日志路径中的 `\` 需转义为 `\\`；服务模式自动追加 `-debug=false`，日志写入文件。
- Linux（systemd/sysv）：声明网络依赖，注入脚本模板；SysV 平台通过 `/etc/init.d/NpxClient` 控制并创建 rc.d 软链接。

---

## 常见排障建议

- 直连失败：确认 `-server`/`-vkey` 与服务端版本兼容，必要时检查 `version.GetVersion()` 的客户端核心版本范围；查看日志是否有心跳/鉴权错误。
- 配置模式失败：检查 `-config` 路径或默认配置路径 `common.GetConfigPath()` 是否存在且格式正确。
- 日志未输出：检查是否在服务模式下运行（会强制 `-debug=false`），确认日志文件路径与权限。
- 代理连接：`-proxy` 格式需符合 `socks5://user:pass@host:port`；若有认证问题请在服务端放行或使用直连测试。
- NAT 探测失败：检查 `-stun_addr` 的连通性或更换 STUN 服务地址。

---

## 使用示例

- 直连模式（控制台）：
  ```bash
  npc.exe -server=1.2.3.4:8024 -vkey=abc123 -type=tcp -debug=true
  ```
- 配置文件模式（服务）：
  ```bash
  npc.exe install
  npc.exe start
  # 服务进程将自动追加 -debug=false，并从默认或指定的 -config 启动。
  ```
- 本地模式（快速 p2p）：
  ```bash
  npc.exe -server=1.2.3.4:8024 -vkey=abc123 -password=secret \
          -local_type=p2p -target=127.0.0.1:22 -local_port=2000
  ```
- 注册本机 IP：
  ```bash
  npc.exe register -server=1.2.3.4:8024 -vkey=abc123 -time=2
  ```
- NAT 探测：
  ```bash
  npc.exe nat -stun_addr=stun.l.google.com:19302
  ```

---

## 结论

npc 客户端将“服务管理”与“连接/任务执行”分层：前者通过跨平台服务框架控制进程生命周期，后者在 `run()` 中依据“直连/配置/本地”三种场景初始化连接或本地服务，并提供日志与重连保护。理解上述入口与分支即可快速定位运行时问题并进行功能扩展。