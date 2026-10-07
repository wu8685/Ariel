<p align="center">
  <img src="web/public/brand/ariel-logo-wind-messenger-color.png" width="360" alt="Ariel" />
</p>

<h1 align="center">Ariel</h1>

<p align="center">在手机浏览器中接续 Mac 上的 Codex Desktop 会话。</p>

## Ariel 是什么

Ariel 是一个运行在可信局域网内的 Codex Desktop 联络中继器。离开电脑后，你仍可以在手机上查看原会话的历史与实时输出、继续发送消息、停止任务、处理审批与补充问题，也可以管理排队输入或在指定项目中新建会话。

Ariel 不复制会话，不接管工作目录，也不建立另一套聊天记录。Codex Desktop 始终是会话和执行状态的唯一来源。

## 架构

```mermaid
flowchart LR
  Browser["手机 / 浏览器"] <-->|HTTP + WebSocket| Relay["Ariel Relay"]
  Relay <-->|WebSocket| Agent["Desktop Agent"]
  Agent <-->|本机 IPC| Codex["Codex Desktop"]
```

- **Web** 提供适配手机和桌面的会话界面。
- **Relay** 负责浏览器认证、连接管理和消息转发。
- **Desktop Agent** 运行在 Mac 上，把 Relay 请求转换为 Codex Desktop 的本机操作。

所有运行状态都来自 Codex Desktop；Relay 不持久化第二份会话正文，也不会自动重放结果不确定的操作。

## 快速开始

准备一台正在运行 Codex Desktop 的 Mac，并安装 Go 1.26+、Node.js 和 npm。手机与 Mac 需要连接同一个可信局域网。

```sh
git clone https://github.com/wu8685/Ariel.git
cd Ariel
./scripts/ariel.sh up local \
  --listen '<Mac-LAN-IP>:8080' \
  --device-id 'my-mac' \
  --device-name '我的 Mac'
```

脚本会构建 Web、Relay 和 Desktop Agent，并在成功后输出手机可访问的地址。查看运行状态和六位连接码：

```sh
./scripts/ariel.sh status
./scripts/ariel.sh show-pin
```

## 基本使用

1. 在手机浏览器中打开启动脚本输出的地址。
2. 输入六位连接码，选择已有会话或创建新会话。
3. 浏览器已连接时，可以使用“手机扫码登录”为另一台手机生成一次性登录二维码。
4. 切换 Wi-Fi 或手机热点后，运行 `./scripts/ariel.sh restart-local` 自动发现新的局域网地址并重启。

常用命令：

```sh
./scripts/ariel.sh up             # 使用已保存的配置启动
./scripts/ariel.sh status         # 查看 Relay、Agent 和 Codex 状态
./scripts/ariel.sh restart-local  # 换网后发现新地址并重启
./scripts/ariel.sh stop           # 停止 Ariel 管理的本地进程
```

## 安全边界

Ariel 当前面向单用户、可信局域网使用。默认 HTTP/WebSocket 连接未加密，不要直接暴露到公网或不可信网络。

远程操作使用 Codex Desktop 当前权限；页面出现 Full Access 提示时，请先确认你接受该权限范围。Ariel 依赖 Desktop 的本机 IPC，版本变化可能造成兼容性问题；不兼容操作会显式失败，不会被当作成功或自动重试。

## 深入阅读

- [安装、配置与故障排查](docs/operations/agent-install.md)
- [系统架构设计](docs/specs/baseline/2026-10-03-codex-remote-architecture.md)
- [Desktop 兼容性与实测边界](docs/compatibility/2026-10-03-m0.md)
- [功能规格索引](docs/specs/README.md)
- [MVP 验收记录](docs/testing/2026-10-04-mvp-acceptance-audit.md)
- [项目待办](docs/TODO.md)
