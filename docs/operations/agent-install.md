# Ariel 安装与本地配置：供执行 Agent 使用

本指南用于让自动化 Agent 在一台 Mac 上安装 Ariel 并完成配置。先向用户确认模式和必要地址；不要索取 Codex 登录信息，也不要让用户把 Relay token 或六位连接码贴进聊天。所有写测试只允许使用隔离会话；安装验收只做只读状态检查。

## 共同前提与行为

1. 在目标 Mac 上取得可信的 Ariel 仓库（例如 `git clone https://github.com/wu8685/Ariel.git`），进入仓库根目录。不要使用 `curl | sh`，也不要自动 `git pull` 覆盖用户工作区。
2. 确认 Go 1.26+ 与兼容的 Codex Desktop 已安装且 Desktop 正在运行。本机 Relay 模式还需要 Node.js/npm；入口会执行 `npm ci`。脚本不安装系统软件、不调用 `sudo`、不修改防火墙或 Codex 配置。缺少前提时先报告，再让用户决定安装方法。
3. 选定稳定且唯一的 `device-id`（字母、数字、点、下划线或连字符；最多 64 字符）和用户看得懂的 `device-name`。后续重启复用同一 ID，避免浏览器中的设备选择失效。
4. 只通过本机私有文件传入 Agent 长口令，绝不放在命令参数、聊天文本、提交或日志中。脚本将凭据复制到 Git 忽略的 `.local/runtime/`（目录 `0700`，文件 `0600`）。现有 Go 服务仍通过进程环境变量读取口令，同一系统用户可能读取自己的进程环境；请保护 Mac 用户会话。

脚本入口为 `./scripts/ariel.sh`；可从任意目录用其绝对路径调用。首次 `up` 完成配置、构建和后台启动；之后只需 `./scripts/ariel.sh up`。`status` 不显示凭据；`stop` 只停止脚本自己登记且核对过身份的进程。现有手工服务或其他端口被占用时，入口会报错，不会接管或杀掉对方。

## 模式 A：本机 Agent 注册到指定 Relay

先从 Relay 管理者取得准确的 Agent WebSocket 地址和该 Relay 的**长口令文件**，并确认 Desktop Mac 可连接该地址。URL 以 `/ws` 结尾；公网或不可信网络必须使用有效 TLS 的 `wss://`。此模式不需要 Web 六位连接码，也不会在本机启动 Relay 或构建 Web。

```sh
./scripts/ariel.sh up agent \
  --relay-url 'wss://relay.example.com/ws' \
  --device-id 'my-mac' \
  --device-name '我的 Mac' \
  --token-file '/path/to/private/relay-agent-token'
./scripts/ariel.sh status
```

确实处于可信局域网、Relay 只提供 `ws://` 时，可以在完整命令后显式增加 `--allow-insecure-ws`。不要用这个开关把明文 WebSocket 暴露到公网。成功时会看到“Agent 已与指定 Relay 握手”；再让用户在该 Relay 的 Web 页面确认设备在线和会话列表。脚本无法代替持有 Web 连接码的用户检查远端页面，因此不把本地进程存活误报为完整端到端验收。Relay 的安装、TLS、Origin 白名单和 token 发放不由本模式处理。

## 模式 B：本机启动 Relay，再注册本机 Agent

先确认手机与 Mac 在同一可信、可互访的局域网。让用户指定 Mac 当前网卡上的 IP 与端口；不要默认监听 `0.0.0.0` 或公网地址。首次运行会生成并私存独立的 Agent 长口令和固定六位 Web PIN；若已有手工部署，先备好旧 token/PIN 文件并用 `--token-file`、`--pin-file` 导入，避免换码。

```sh
./scripts/ariel.sh up local \
  --listen '192.168.1.20:8080' \
  --device-id 'my-mac' \
  --device-name '我的 Mac'
./scripts/ariel.sh status
```

只有用户明确需要读码时，才在其可信本机终端执行 `./scripts/ariel.sh show-pin`；不要把输出转发到聊天或日志。手机打开脚本报告的 `http://<Mac-LAN-IP>:<port>/`，输入原码或新生成的六位码。刷新同一标签页会尽量复用 Relay session；Relay 重启、新标签页或 session 过期可能要求重输。HTTP/WS 为明文，六位码不能抵御同网段窃听；不要在访客 Wi‑Fi、不可信网络或公网直接使用。

## 后续操作与核验

```sh
./scripts/ariel.sh status   # Relay HTTP 可达性、Agent 握手／Codex 就绪
./scripts/ariel.sh up       # 沿用原配置和口令；已运行时不重复启动
./scripts/ariel.sh stop     # 先 Agent 后 Relay；不杀外部进程
./scripts/ariel.sh help
```

更新仓库代码后，先 `stop`，再 `up` 以重建二进制。若要更换 Relay、监听地址或设备身份，先 `stop`，再用完整的 `up agent`／`up local` 参数加 `--replace-config`；切换到另一外部 Relay 时必须提供它的口令文件。从外部模式切到本机模式且未导入 token 时，会生成新的本机长口令，不复用外部 Relay 的凭据。错误或构建失败会保留已保存配置；可修复后重试。日志位于 `.local/runtime/relay.log` 与 `.local/runtime/desktop-agent.log`，不要把未经检查的日志直接公开。

安装 Agent 的交付报告应只包含：模式、脚本版本或 commit、手机／Relay URL（不含口令）、`status` 结果、是否做过物理手机验收，以及未验证事项。不要发送真实会话正文、绝对私人路径、token、PIN 或 Codex 凭据。

### 常见故障

| 现象 | 只读检查与处理 |
| --- | --- |
| `port ... occupied` | 原服务不由脚本管理；确认占用者与迁移窗口，切勿按端口杀进程。 |
| Desktop/IPC 不可用或版本未验证 | 打开 Codex Desktop，核对项目支持的 Desktop／内置 Codex 版本；不要用未知版本强行启动。 |
| Relay 在线、Agent 尚未握手 | 检查 `status`、Agent 日志、URL 与 Relay 长口令；本机 LAN 地址应绕过 HTTP 代理。不要反复提交手机短码碰锁定上限。 |
| 手机无法打开页面 | 检查脚本报告的监听 IP、同一可信局域网、防火墙和 AP 隔离；不要为了排障关闭整机防火墙。 |
| 页面打开但没有设备 | 先看 `status` 是否显示 Agent 握手；若已握手，刷新页面并检查是否选择了正确的稳定设备 ID。 |

本工具不是 launchd 服务；Mac 重启后需再次执行 `up`。它也不自动部署公网 TLS 或外部 Relay。
