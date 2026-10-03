---
title: "Codex Desktop 会话接入实测"
created: 2026-10-03
tags: [codex, remote-control, evidence]
type: experiment
source: "[[2026-10-03-codex-remote-adapter]]"
project: "Codex 远程交互系统 待命名"
---

# Codex Desktop 会话接入实测

测试日期：2026-10-03（Asia/Shanghai）。本次仅验证本机当前版本，不代表公开接口兼容承诺。

## 结论

现有 Desktop 会话的历史可由独立 App Server 读取。连接现有 Desktop 会话的实时状态并发送、停止任务，本次通过 Desktop 自身的本地 IPC 成功，未通过公开 App Server 控制 socket 完成。

## 环境与入口

- Desktop 内置 Codex：`0.159.0-alpha.12.1`。
- PATH 中的 Codex CLI：`0.144.6`。
- Desktop 的 App Server 进程：PID `71830`，父进程是 Desktop，启动使用默认 stdio。
- 默认公开控制 socket `<user-home>/.codex/app-server-control/app-server-control.sock` 不存在；`codex app-server daemon version` 连接失败。
- Desktop 本地 IPC：`<user-home>/.codex/ipc/ipc.sock`，由 Desktop 主进程提供。
- IPC 编码：4 字节 little-endian 长度前缀 + UTF-8 JSON；不是公开 App Server WebSocket/JSON-RPC 接口。
- 协议来自已安装应用包的只读检查；未修改应用包、配置、凭据或启动参数。

## 实测结果

| 验证项 | 结果与证据 |
| --- | --- |
| 独立 App Server 读取当前聊天 | 成功。`thread/read(includeTurns:true)` 返回当前聊天历史；`thread/list` 能找到该会话。 |
| 独立进程是否共享实时状态 | 不共享本次运行状态。当前聊天正在执行，但新进程返回 `notLoaded`，`thread/loaded/list` 为空。 |
| IPC 发现当前聊天 owner | 成功，`thread-owner-discovery` 返回 Desktop owner。 |
| IPC 订阅当前聊天 | 成功，收到 snapshot，随后收到 token usage 和当前 turn item 的 patches。 |
| 从独立程序发送消息到 Desktop 测试聊天 | 成功，收到 `IPC_REPLY_OK`，Desktop 读回的 turn 为 `completed`。 |
| 从独立程序停止 Desktop 测试任务 | 成功，按 `expectedTurnId` 停止，Desktop 读回为 `interrupted`，执行时长 310 ms。 |
| IPC 客户端断开后重连 | 成功，新连接重新发现 owner，收到包含已完成回答的 snapshot。 |
| Desktop 写入后，由另一 App Server 读取 | 成功，返回三个 turn，状态依次为 completed、completed、interrupted，并读到两条测试回答。 |

本次没有向当前业务聊天注入测试消息或停止当前任务。当前聊天只做历史读取、owner 查询和实时订阅。

## 测试聊天与 turn

- 当前聊天：`<business-thread-id>`。
- 临时测试聊天：`<fixture-thread-id>`，标题 `[临时测试] Desktop 连接验证`。
- 普通 App Server 初始化测试 turn：`<seed-turn-id>`，回答 `CONNECTIVITY_SEED_OK`。
- IPC 发送测试 turn：`<reply-turn-id>`，回答 `IPC_REPLY_OK`，状态 completed。
- IPC 停止测试 turn：`<stop-turn-id>`，状态 interrupted。
- IPC 控制请求与当前聊天的 owner 都由同一 Desktop client 处理：`fdb67af3-f5ab-4d93-91dc-60804e54c5ce`。

临时测试聊天先由独立 App Server 创建并完成初始化回复；退出该进程后，在 Desktop 打开该聊天，Desktop 随即成为其 owner。后续发送、停止由独立 IPC 客户端请求 Desktop 执行。原始创建来源没有成为控制障碍。

## 实际使用的 IPC 操作

- `initialize`，版本 0。
- `thread-owner-discovery`，版本 1。
- `thread-stream-following-changed`，版本 1，用于开始/结束订阅。
- `thread-follower-load-complete-history`，版本 1。
- `thread-stream-state-changed`，版本 11，包含 snapshot 和 patches。
- `thread-follower-start-turn`，版本 2。
- `thread-follower-interrupt-turn`，版本 4，并传入精确 `expectedTurnId`。

会话数据不一定放在顶层 `turns` 数组。当前 Desktop 快照中该数组为空，实际历史位于 `turnHistory.history`。不能把空 `turns` 误判为无历史。

## 尚未验证

- 全新、未在 Desktop 加载的历史聊天如何自动成为 Desktop owner。本次测试中，打开该聊天前 owner 查询返回 `no-client-found`，打开后成功。
- 执行中追加指令、远程审批、文件附件、多电脑路由。
- Desktop 完全退出、重启、升级之后的兼容性。
- 不同历史存储格式，以及云端 ChatGPT 聊天类型。
- 不保证每个瞬时事件都已持久化，也不保证网络中断时自动重发的幂等性。

## 收尾

临时聊天已归档（可恢复），界面已切回原聊天。所有探针连接均结束；只保留本目录中的测试脚本、版本匹配的 Schema、测试标识和此报告。

## 文件

- `probe.mjs`：标准 App Server 的 stdio 探针；默认只读。`seed` 模式会创建并运行一个临时测试会话。
- `ipc-probe.mjs`：Desktop IPC 探针；默认只读。`exercise` 模式只允许对 `test-thread.json` 中指定的临时会话执行发送/停止。
- `inspect-asar.mjs`：只读检查已安装应用包的相关代码；不会解包或修改应用。
- `schema/`：由 Desktop 内置 CLI 生成的协议 Schema。

上述脚本是一次性兼容性探针，不是生产客户端。IPC 中的状态恢复、异常清理和版本兼容仍需在正式 Adapter 中实现。
