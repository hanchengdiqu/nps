# NPS 服务端（nps）启动与运行业务逻辑说明

> 适用文件：`cmd/nps/nps.go`（入口），相关模块：`server/*`、`web/routers`、`lib/*`
>
> 目标：梳理服务端启动、服务管理、配置加载、核心运行与组件协作，便于排障与扩展。

---

## 概览

nps 服务端负责：
- Web 管理端与接口路由初始化（`web/routers`）。
- 桥接端口服务启动（客户端与服务端的数据通道）。
- TLS 初始化、端口策略、系统信息采集、备份服务。
- 系统服务管理（安装/启动/停止/重启/卸载/更新/守护）。

---

## 启动入口：`main()`

职责与流程：
- 解析命令行参数（目前仅 `-version`）。
- 加载配置文件 `conf/nps.conf`，失败时使用内置默认配置（`DefaultConfig.ApplyToAppConfig()`）。
- 初始化 pprof（按配置文件）、日志（控制台或文件）。
- 构造跨平台系统服务配置，注入 systemd/SysV 脚本（非 Windows），并追加运行参数 `service`。
- 创建服务实例并根据子命令执行控制；缺省以服务方式运行。

关键点：
- 配置查找优先级：安装路径 `common.GetRunPath()/conf/nps.conf` → 若加载失败，应用内置默认配置。
- 日志输出策略：
  - 带参数 `service` 时使用文件日志（路径缺省为 `common.GetLogPath()`）。
  - 非 `service` 参数时输出到控制台。
- 非 Windows 平台声明依赖：`Requires=network.target` 与 `After=network-online.target syslog.target`。

---

## 系统服务子命令

- `reload`：初始化守护（`daemon.InitDaemon("nps", ...)`）。
- `install [--force]`：
  - 先 `stop` → `uninstall`。
  - `--force` 时使用 `install.ReInstallNps()`，否则 `install.InstallNps()`，并将返回的二进制路径设置到 `svcConfig.Executable`。
  - 重新创建服务并执行安装；SysV 平台创建 `/etc/rc.d/S90<name>` 与 `/etc/rc.d/K02<name>` 软链接。
- `start|restart|stop`：SysV 用 `/etc/init.d/<name>` 调用；其他平台使用通用服务控制。
- `uninstall`：卸载服务，并在 SysV 清理 rc.d 软链接。
- `update`：在线更新服务端二进制（`install.UpdateNps()`）。
- 其他未知命令会提示不支持。

服务名与显示名：由 `programBaseName()` 解析当前可执行文件名（去 `.exe`），作为 `Name` 与 `DisplayName` 前缀。

---

## 服务生命周期（`nps` 结构体）

- `Start()`：查询状态并异步启动 `p.run()`。
- `Stop()`：查询状态，关闭退出通道；交互模式下直接进程退出。
- `run()`：
  - `defer` 捕获 `panic`，记录调用栈，避免进程崩溃。
  - 调用顶层 `run()` 启动核心逻辑。
  - 阻塞等待退出信号并优雅停止。

---

## 核心运行：`run()`（顶层）

1) 初始化 Web 路由：`routers.Init()`。
2) 构造一个 `file.Tunnel{ Mode: "webServer" }` 作为入口任务描述。
3) 读取桥接端口：`bridge_port`（必需，失败则退出）。
4) 打印服务端版本与允许客户端核心版本范围：`version.VERSION`、`version.GetVersion()`。
5) 初始化连接服务：`connection.InitConnectionService()`（心跳、注册、转发等依赖）。
6) 初始化 TLS：`crypt.InitTls()`（证书路径按配置或默认）。
7) 初始化端口策略：`tool.InitAllowPort()`（白名单/限制策略）。
8) 启动系统信息采集：`tool.StartSystemInfo()`（用于后台展示与诊断）。
9) 读取 `disconnect_timeout`（客户端断连清理阈值，默认 60）。
10) 启动备份服务：`server.StartBackupService()`（数据库/文件备份与邮件报告）。
11) 启动桥接服务：
    ```go
    go server.StartNewServer(bridgePort, task, beego.AppConfig.String("bridge_type"), timeout)
    ```
    - `bridge_type`：桥接传输类型（通常 `tcp`）。
    - `timeout`：断连清理阈值。

---

## 重要组件与协作

- Web 路由（`web/routers`）：注册管理后台与接口路由，支撑 Web 控制台。
- 连接服务（`server/connection`）：维护客户端连接、心跳、注册与转发的核心通道。
- TLS（`lib/crypt`）：加载/初始化服务端证书与密钥，支持 HTTPS/加密传输。
- 端口策略（`server/tool`）：端口白名单、限制与系统信息采集（CPU/内存/网络）。
- 备份服务（`server` 包内部）：周期性备份数据库/文件，支持邮件报告（参考配置项）。
- 代理模块（`server/proxy/*`）：HTTP、HTTPS、TCP、UDP、SOCKS5、P2P 等具体代理实现与转发逻辑。

---

## 配置与默认值（`DefaultConfig`）

当 `conf/nps.conf` 无法加载时，使用内置默认配置写入 `beego.AppConfig`，关键项包括：
- 运行信息：`appname`、`runmode`。
- HTTP/HTTPS 代理：`http_proxy_ip`、`http_proxy_port`、`https_proxy_port`、`https_just_proxy`、证书文件。
- 桥接：`bridge_type`（默认 `tcp`）、`bridge_port`、`bridge_ip`。
- Web 管理端：`web_host`、`web_username`、`web_password`、`web_port`、`web_ip`、`web_open_ssl`、证书文件。
- 日志：`log_level`、`log_path`（缺省为 `common.GetLogPath()`）。
- 鉴权/能力开关：`auth_crypt_key`、`allow_*` 系列（用户、速率、连接数、隧道数、本地代理、多 IP 等）。
- HTTP 缓存：`http_cache` 与长度、是否增加 `Origin` 头。
- 连接与超时：`disconnect_timeout`（默认 60）。
- 邮件备份：`email_*` 配置（开关、周期、SMTP 主机/端口/用户名/密码/TLS、发件人、收件人、主题、保留天数）。

---

## 时序（文字版）

- 进程启动 → 加载配置（文件或默认） → 初始化 pprof/日志 → 构造服务配置 → 根据子命令执行控制或进入服务。
- 服务启动（`Start`）→ 异步执行顶层 `run()`：
  - `routers.Init()` → 读取 `bridge_port` → 打印版本 → `connection.InitConnectionService()` → `crypt.InitTls()` → `tool.InitAllowPort()` → `tool.StartSystemInfo()` → 读取 `disconnect_timeout` → `server.StartBackupService()` → `StartNewServer(...)`。
- 服务停止（`Stop`）→ 关闭退出通道，后台优雅退出。

---

## 常见排障建议

- 配置加载失败：确认 `conf/nps.conf` 路径与语法；若使用默认配置，注意 Web/桥接端口可能与系统已有端口冲突。
- 桥接端口错误：`bridge_port` 读取失败会直接退出；检查配置文件或默认值范围。
- TLS 初始化异常：检查证书/密钥文件路径与权限，或关闭 `web_open_ssl` 并验证 HTTP 服务是否正常。
- 客户端兼容性：查看日志中的服务端版本与允许的客户端核心版本（`version.GetVersion()`），以排除协议不匹配。
- 服务模式日志：带 `service` 参数时日志写入文件；路径在 Windows 下会自动将 `\` 转义为 `\\`。
- 备份/邮件失败：核对 SMTP 主机/端口/TLS、账号与 `email_*` 配置；查看备份任务日志输出。

---

## 使用示例

- 安装与启动服务：
  ```powershell
  nps.exe install
  nps.exe start
  ```
- 强制重新安装：
  ```powershell
  nps.exe install --force
  ```
- 控制服务：
  ```powershell
  nps.exe restart
  nps.exe stop
  nps.exe uninstall
  ```
- 在线更新：
  ```powershell
  nps.exe update
  ```
- 启动守护：
  ```powershell
  nps.exe reload
  ```

---

## 结论

服务端将“进程与服务管理”与“核心业务运行”清晰分层：入口负责配置与服务控制；核心 `run()` 统一初始化路由、连接、TLS、策略、系统信息与备份，再启动桥接服务。理解该时序与组件协作关系，可高效定位问题并进行扩展开发。