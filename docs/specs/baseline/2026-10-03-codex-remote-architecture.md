---
title: "Codex 远程交互系统架构与状态归属"
created: 2026-10-03
tags: [codex, remote-control, architecture, ssot]
type: experiment
source: "[[2026-10-03-codex-remote-requirements]]"
project: "Codex 远程交互系统 待命名"
---

# Codex 远程交互系统架构与状态归属

## 连接与职责

Web 主动连接 Relay，Desktop Agent 也主动连接 Relay；两条连接建立后均可双向传输。Agent 通过电脑本地接口访问 Codex。Relay 不直连电脑端监听端口，Web 不直接访问 Codex socket。

| 组件 | 负责 | 不负责 |
| --- | --- | --- |
| Web | 会话选择、历史展示、渐进回复、发送和停止、审批与回答、连接和错误提示 | 执行模型、保存权威会话、直接调用 Desktop IPC |
| Relay | 连接鉴别、设备注册、请求路由、订阅事件转发、在线状态 | 理解 Codex 私有协议、保存聊天、重放任务、离线排队 |
| Desktop Agent | Codex 适配、历史读取、owner 发现、快照归一化、请求关联、状态同步 | 第二套会话库、模型代理、绕过权限、自动启动 Desktop |
| Codex Desktop | 持有实时执行 owner、既有执行环境、原有审批规则和会话落盘 | 替本系统保存投递记录或 Relay 状态 |

建议 Relay 同时提供 Web 静态资源与 `/ws`，手机访问局域网 Relay 地址；桌面浏览器访问同一页面。局域网 IP、端口和防火墙需在现场验证；手机的 `localhost` 不是电脑。

## 两个 Codex 数据来源

建议把 Adapter 内部分成两个角色：

- `HistoryReader`：通过匹配版本的标准 App Server 读取本地会话列表和持久化历史，不执行任务。
- `DesktopController`：连接正在运行的 Desktop IPC，发现 owner，订阅实时状态、发送、停止、审批和回答。

这是当前实测支持的接入策略，不是官方稳定扩展协议。应优先探测目标版本是否存在可复用的公开控制入口；不能仅因公开协议存在，就断言它连接着现有 Desktop owner。

禁止为了“恢复旧会话”而未经设计就在独立 App Server 中调用 resume/start-turn，制造与 Desktop 并行的执行者。读取进程的 `notLoaded` 不表示 Desktop 空闲，更不能用作允许发送的依据。

## SSOT 与允许的内存状态

会话历史的 SSOT 是 Codex 的持久化记录；尚在执行的 turn、审批和补充问题以 Desktop 当前 owner 为准。实时状态不一定已写入历史。Agent 的快照只是可重建的视图，不是新的事实源。

| 位置 | 允许保存 | 生命周期 |
| --- | --- | --- |
| Codex | 原生会话、工作目录与原有设置 | 由 Codex 管理 |
| Relay 配置 | 监听地址、连接 token、Origin 白名单等 | 启动配置，不是业务数据 |
| Relay 内存 | 在线连接、路由、请求关联、订阅、有界发送缓冲 | 连接或进程结束失效 |
| Agent 配置 | Relay 地址、稳定 deviceId、Codex binary/socket 路径 | 启动配置，不复制 Codex 凭据 |
| Agent 内存 | owner、当前快照、请求映射、有限去重、订阅引用计数 | 可从 Codex 重建；重启不恢复投递账本 |
| Web 内存 | 当前页面、历史视图、草稿、发送待确认状态 | 页面存活期间；默认不离线保存历史 |

沿用 Codex 原始 `threadId`，路由键为 `(deviceId, threadId)`。`deviceId` 在配置中稳定保存；Agent 每次进程启动另生成 `agentEpoch`，不可混用。无需另建 conversationId 映射数据库。

“不持久化”不等于没有状态，也不等于承诺无限内存。所有队列、快照和请求表必须有上限及清理机制；日志默认只记请求 ID、状态、时延和错误类型，不落聊天正文、回答、命令明文或 token。

## 同一会话的写入顺序

Agent 按 thread 串行处理变更请求，并在执行前检查 owner 的最新状态。Web 的按钮状态只改善体验，不是并发控制。电脑 Desktop、手机 Web、电脑 Web 都可能提交；最终以 Codex owner 的受理结果为准。

未实测前不能声称已阻止所有“Desktop 与 Web 同时发送”的竞态。验收必须覆盖；若 Desktop 自身会排队而 Adapter 无法阻止，需要说明差异并回到产品决策，不能悄悄违反“不自动排队”。

停止绑定精确 turnId。审批与回答绑定精确待处理请求及 owner 会话；用户在电脑处理后，Web 按钮失效。切换会话或断开 Web 只取消订阅，不停止 turn，也不自动拒绝审批。

## 断线和重启

| 情况 | 系统行为 |
| --- | --- |
| Web 与 Relay 断开 | 显示状态可能过期，禁止写入；重连鉴别后重新读取列表并订阅当前会话 |
| Agent 与 Relay 断开 | Codex 已受理任务可能继续；Relay 告知离线，不积压待执行请求 |
| Relay 重启 | 路由和订阅丢失，双方重连重建；不得重发先前变更请求 |
| Agent 重启 | 新 agentEpoch；重新发现 owner、加载历史和当前请求，不依赖以前内存 |
| Desktop 退出或崩溃 | Agent 可保持 Relay 在线，但 codexReady 为 false；不自动重启，不承诺任务继续 |
| 丢失提交或审批响应 | 显示“结果未知”，刷新权威状态；无法确认时由用户决定，不自动重发 |
| 长时间缓冲溢出或事件缺口 | 使当前 stream 失效，重新取快照；不能丢事件后继续伪装一致 |

重连恢复的是“当前可观察状态”，不是全部离线事件。不能假设临时审批可以从聊天日志完整还原；要从当前 owner 查询。查询失败时显示“交互状态未知”，不得把缺数据解释成“没有待处理请求”。

## 首版最小安全边界

以下是轻量工程提案，不扩展成账号平台：单个配置 token、明确 Web/Agent 角色、Web Origin 白名单、消息大小和请求并发限制。token 用首次鉴别帧传输，不放 URL，也不记录日志；鉴别完成前不转发业务请求。

LAN 可先使用 HTTP/WS，但只限可信网络，并明确 token 和内容没有传输加密。Relay 默认绑定本机；需要手机访问时显式配置局域网监听。不得直接将这套配置暴露到公网。

Codex 凭据、本地 socket 和执行权限留在电脑。Relay 只路由白名单业务方法，不提供任意 IPC/RPC 转发或远程 shell。审批内容仍可能含敏感代码，因此只对已鉴别 Web 连接发送；Markdown 不执行原生 HTML，不自动加载不可信远程资源。

上线公网时再安排 TLS、独立设备凭据与撤销、用户隔离、访问控制、审计及攻击面检查。本阶段不实施公网部署。

## 相关规格

[[2026-10-03-codex-remote-protocol]]、[[2026-10-03-codex-remote-adapter]]、[[2026-10-03-codex-remote-delivery]]。
