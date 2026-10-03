# 0003：M1 最小链路（Relay、Mock Agent、Web）

- 状态：Approved，用户于 2026-10-04 确认继续实施。
- 创建日期：2026-10-03
- 设计依据：[初始架构](baseline/2026-10-03-codex-remote-architecture.md)、[初始协议](baseline/2026-10-03-codex-remote-protocol.md)、[实施与验收](baseline/2026-10-03-codex-remote-delivery.md)、[审批语义](0002-approval-denial-semantics.md)。
- 边界：M1 使用 Mock Agent，不接真实 Codex owner；当前版本的真实能力只由 M0 报告声明。

## 1. 用户可见结果

在同一响应式 Web 页面中，手机和电脑都可连接局域网 Relay，看到 Mock 会话列表、详情和实时变化，切换会话、发送一条文字并停止指定的 Mock turn。Web 显示离线、连接中、busy、未知结果等不同状态。真实 Codex 续聊属于 M2，审批和补充回答的生产 UI 属于 M3。

M1 的“已发送”只代表 Mock Agent 已受理，不暗示当前 Desktop 具备相同效果。页面应清楚标识 Mock 演示模式；不得把模拟会话混入真实 Codex 历史。

## 2. 组件与状态归属

| 组件 | 输入 | 输出 | 生命周期内可保存 |
| --- | --- | --- | --- |
| Web | 用户输入、Relay 事件 | 请求、草稿、当前视图 | 页面内存中的 token、草稿、当前视图和请求状态 |
| Relay | Web / Agent 主动 WebSocket 连接 | 鉴别结果、路由响应、设备与订阅事件 | 有界连接、路由、订阅和缓冲；进程重启即消失 |
| Mock Agent | Relay 请求、预置合成会话 | 规范化列表、历史、快照、增量和控制回执 | 可重建的合成 fixture 状态，不写业务数据库 |

Relay 只监听 Web 可访问的 HTTP/WS 地址，不主动连接 Desktop 或 Codex。M1 不启动真实 Codex Adapter，不创建第二套真实会话库、离线任务队列或内容日志。Mock ID 只用于测试，与真实 threadId 命名空间分开。

## 3. 启动配置

- 同一个 Go module 构建 `cmd/relay`、`cmd/mock-agent`；Web 为 React + TypeScript + Vite，不使用 SSR。
- Relay 默认监听 `127.0.0.1`；手机访问需显式配置局域网监听地址。Relay 同时服务 Web 静态资源和 `/ws`。
- Relay 启动时必须配置一枚非空随机 token 和允许的 Web Origin 集合；不提供内置固定凭据。浏览器页面只在内存中保留用户输入的 token，连接鉴别帧携带 token，不放 URL。
- Agent 配置 Relay URL、同一 token、稳定 deviceId 和显示名；每次进程启动生成新的 agentEpoch。Relay 每次启动生成 relayEpoch。
- 日志默认仅含连接与请求的合成 ID、状态、耗时和错误类型；不记录 token、消息正文、命令、回答或会话快照。

## 4. 唯一协议定义

`protocol/` 中的 JSON Schema 是 v1 唯一机器可读协议定义。Go 与 TypeScript 通过同一 Schema 生成类型或在边界校验；协议 fixtures 同时通过两端验证，不能维护不一致的手写平行类型。协议为自有 JSON over WebSocket，不暴露 Codex IPC method/version。

### 4.1 握手与角色

客户端连接后 5 秒内的第一帧必须是 `hello`（v1、role、token）；Agent 还带 deviceId、deviceName、agentEpoch、adapterVersion 和 capabilities。鉴别与版本检查通过后，Relay 返回含 connectionId、relayEpoch 的 `hello.ok`。鉴别完成前不处理业务帧；错 token、错版本、缺必填、重复 hello、未允许的 Web Origin 或超限帧均拒绝。Agent 的 Origin 可缺省，但 role 不可由后续业务帧改变。

同一 deviceId 同时最多一个活跃 Agent；第二个注册明确失败，不替换第一个。连接断开或心跳判失联后，旧 Agent 路由失效。设备可见状态区分 agentOnline、codexReady 与 capability，前者不能推断后者。

### 4.2 请求关联与错误

Web 请求带 requestId UUID、deviceId、白名单 method 和规范化 params。Relay 按 `(connectionId, requestId)` 建立有界关联，给 Agent 使用新的 relayRequestId；Agent 不能指定 Web 连接身份。Agent 响应只回原 Web。Web 断线、Agent 断线或 Relay 重启后不续投、不重放。

首版白名单：`device.list`、`thread.list`、`thread.read`、`thread.subscribe`、`thread.unsubscribe`、`turn.start`、`turn.interrupt`、`interaction.respond`。M1 Mock 可按相同形状提供交互卡片；未知 interaction 一律可见 unsupported。`interaction.respond` 的公共拒绝决定须区分 `deny` 和 `deny_and_stop`，只展示该卡片实际支持的决定；M1 不作真实审批。

变更响应区分 accepted、rejected 和 unknown；确定未转发为 `not_submitted`，已发出而回执丢失为 `unknown`。Relay 收到请求不等于执行者已受理。请求过期后在 Agent 提交前拒绝，提交后超时不自动重试。错误 code 至少涵盖 UNAUTHORIZED、PROTOCOL_UNSUPPORTED、DEVICE_OFFLINE、TURN_BUSY、STALE_TURN、INTERACTION_UNSUPPORTED、OUTCOME_UNKNOWN、RESYNC_REQUIRED、INVALID_ARGUMENT、OVERLOADED。

### 4.3 订阅顺序

`thread.subscribe` 成功响应先于此订阅的首个 `thread.snapshot` 事件；新 streamId 的 seq 从 1 开始。后续 `thread.update` 的 baseSeq 必须等于上一个 seq。Web 发现缺口、未知 streamId 或 owner/epoch 变化时停止写入并重新订阅；Relay 不提供持久化事件回放。多个 Web 对同一 thread 独立持有 subscriptionId；一个 Web 取消订阅不影响另一个，也不停止 Mock turn。

Web 按 item ID 替换正文，不把重复全量文本接成重复段落。终态 completed、failed、interrupted 分别显示。未加载历史的可用性保留 autoLoad capability；手机点选后自动加载原会话是产品必做 TODO，Mock 不冒充此功能已在 Desktop 实现。

## 5. Mock 行为与并发

- 预置两个同名但不同 cwd/更新时间的会话，列表支持跨页读取；过滤范围明确写成 Mock 数据范围。
- `turn.start` 必须按 thread 串行检查最新 runtime、pending request 和 clientMessageId；busy 时返回 TURN_BUSY 且不入队。Web 输入保留草稿，turn 结束后不自动发送。
- Mock 受理返回新的 turnId，逐步替换同一个 assistant item，随后发 completed；可配置阻塞或失败用例。`turn.interrupt` 只对 expectedTurnId 生效，旧 ID 不得停止新 turn。
- 两个 Web 同时 start 同一 idle thread 时，至多一个被受理。Agent 入口对同一 thread 串行，锁内再次读取状态。M0 已发现当前原生 Desktop 在 busy 时仍可能返回 accepted 且复用原 turnId；M2 必须保留同一入口约束，并独立核对原生消息是否真正受理。
- 一端通过 Mock 处理 interaction，另一端的旧卡片变为 resolved；未知与过期请求不予受理。

## 6. 容量、错误与故障

初始上限：单帧 8 MiB、Web 输入 64 KiB、单连接待发 16 MiB、单 Web 未完成请求 32 个；超限返回可见错误或关闭异常连接，不丢事件继续声称一致。hello 超时 5 秒，ping/pong 默认 15/45 秒，重连退避 1、2、4、8 秒并在 15 秒封顶且加抖动；均作为可配置初值。

Relay 只在可信局域网以 HTTP/WS 运行。启用局域网监听时 README 明确传输未加密及防火墙要求，不把该配置作为公网部署方案。断线后恢复当前 Mock 状态，不补发先前的 turn.start / interrupt / interaction.respond。

## 7. TDD 与验收

按下列顺序，每组先写并运行失败测试，再写最小实现、运行通过并重构：

1. JSON Schema 正反例、Go/TS 共用 fixtures、版本和字段边界。
2. Relay 握手：token、Origin、角色、重复 Agent、连接 epoch、超时。
3. Relay 路由：请求关联、断线清理、超时 unknown、容量、无离线队列。
4. 订阅：响应先于快照、多 Web 独立、seq/stream 缺口和重同步。
5. Mock Agent：列表分页、history、逐步 item 更新、busy 不排队、exact interrupt、旧 interaction。
6. Web：鉴别、会话切换、草稿和状态、两个浏览器实例同步；最后用真实手机浏览器在 LAN 验证 Mock 链路。

M1 退出条件：手机经 Relay 看见 Mock 会话并可完成上述交互；错误 token / Origin、Agent 离线、重复请求、慢消费者、两 Web 同时发送均有测试。所有页面仍须标明 Mock；真实 Codex 续聊、审批与自动加载不能据此标记完成。
