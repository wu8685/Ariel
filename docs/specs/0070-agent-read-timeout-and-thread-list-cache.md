# 0070：Agent 只读请求自愈与设备会话列表缓存

- 状态：Implemented（2026-10-11；Go／Web／race／production build／真实 Chromium 截图回归通过）。
- 来源：切换到在线设备时会话列表偶发归零，并显示“执行结果不确定”；重启本机 Agent 后可暂时恢复。
- 跟踪：[GitHub Issue #1](https://github.com/wu8685/Ariel/issues/1)。
- 范围：在 0064 的跨层只读预算之上，补充 Desktop Agent 的 Codex App Server 自愈、Relay／Web 的最终只读超时语义，以及浏览器内按设备隔离的会话列表缓存；不自动重放任何写操作，不改变 Codex 会话 SSOT。

## 已确认原因

1. Relay 与 Web 都有请求超时，但现有 `RestartingRPC` 只有在 App Server 子进程退出时才替换 endpoint。
2. 当 App Server 进程仍存活、`thread/list` RPC 却不再返回时，Agent 会一直占用处理槽；Relay 先超时并把读取误报为 `OUTCOME_UNKNOWN`。
3. `OUTCOME_UNKNOWN` 是写操作必须保守处理的语义；会话列表读取不会产生远端副作用，超时后可以安全重试，也应向用户呈现可重试的 `TIMEOUT`。
4. Web 切换设备时会先清空会话列表，再重新请求；即使浏览器刚读取过该设备，也无法立即复用已确认的列表。

## 行为与不变量

1. `thread/list` 每次调用 App Server 必须有短于 Relay 普通请求超时的单次 deadline。
2. 首次 deadline 到期时，即使 App Server 进程没有退出，也必须关闭并废弃当前 endpoint，启动新 endpoint，并安全重试一次。
3. 第二次仍失败时返回确定性的 `TIMEOUT`；不得继续无界重试，不得让挂起 goroutine 长期占用 Agent 请求槽。
4. 只有明确列出的只读 RPC 可以自动重试。队列修改、发送、停止、审批、新建会话等写操作仍不得自动重放，结果不明时继续使用 `OUTCOME_UNKNOWN`。
5. Relay 和 Web 本地计时器必须把只读请求超时表达为 `rejected/TIMEOUT`；写请求和可能已提交的 mutation 继续使用 `unknown/OUTCOME_UNKNOWN`。
6. Web 在内存中缓存会话列表，identity 至少包含 `deviceId`、`agentEpoch` 与规范化搜索词；不同设备、不同 Agent epoch、不同搜索词不得串用。
7. 切回设备时先显示最后确认的缓存列表：缓存仍新鲜时不重复请求；缓存过期时保留旧列表并后台刷新，失败只显示错误，不把已确认列表清空。
8. 手动刷新必须绕过新鲜度判断并发起真实请求；成功响应以 thread identity 合并／替换缓存，分页 cursor 与列表一起保存。
9. 缓存必须有界并采用 LRU 淘汰。设备消失、Agent epoch 改变或浏览器断开时清除不再有效的条目。

## 超时预算

- 0064 已将 Relay 只读 route 设为 35 秒、Web 只读等待设为 40 秒。
- Agent 的 `thread/list` 单次 App Server deadline 默认 3 秒，最多尝试 2 次；即使 `Service.List` 继续读取置顶分区，也远小于 Relay 的 35 秒上限。
- Web 的 transport timeout 继续晚于 Relay；它只作为最后一道保护，不代替 Agent 自愈。
- 搜索和历史读取沿用各自已有的较长预算，本规格不把大查询强制压缩到 4 秒。

## TDD 与验收

1. Red：构造 endpoint 进程存活但 `thread/list` 等到 context deadline；断言第一次 endpoint 被关闭、第二个 endpoint 成功，且只启动两次。
2. Red：第二个 endpoint 也超时；断言有限时间内返回 `context.DeadlineExceeded`，不出现第三次启动。
3. Red：mutation 在 endpoint 异常时仍不重放，继续返回未知结果语义。
4. Red：Relay 的只读超时返回 `rejected/TIMEOUT`，mutation 超时仍返回 `unknown/OUTCOME_UNKNOWN`。
5. Red：Web transport 对 `thread.list` 的本地超时返回 `TIMEOUT`；写请求仍保持 `OUTCOME_UNKNOWN`。
6. Red：设备 A → B → A 时，A 的同 epoch 列表立即从缓存恢复；新 epoch 不复用旧列表；手动刷新绕过缓存；后台刷新失败保留缓存内容。
7. 通过 Go 全包测试、race、Web 全量测试与 production build；使用真实浏览器验证设备切换不会先闪成空列表。
8. 真实公网链路复验：本机 Agent 重启后在线；模拟／等待卡住读取时可自愈，手机不再看到“执行结果不确定”。

## 非目标

- 不在 Relay 存储完整会话历史或新增数据库。
- 不跨浏览器刷新持久化会话列表。
- 不把旧 epoch 的会话列表当作当前 Agent 的权威状态。
- 不自动重放任何可能改变 Codex 状态的操作。

## 实施结果

- Desktop Agent 为 `thread/list` 的每次 App Server 调用设置 3 秒 deadline。进程仍存活但 RPC 卡死时，旧 endpoint 会被关闭并强制重建，只读请求只重试一次。
- `context.DeadlineExceeded` 在 Agent、Relay 和 Web transport 中统一为 `rejected/TIMEOUT`；mutation 计时器仍返回 `unknown/OUTCOME_UNKNOWN`，没有扩大自动重放范围。
- Web 新增 4 MiB／16 条目的内存 LRU 会话列表缓存，以 `deviceId + agentEpoch + normalized query` 隔离；60 秒内切回设备直接复用，过期条目先显示再后台刷新，最长保留 5 分钟。
- 手动刷新显式绕过缓存；分页成功后缓存合并后的列表与 cursor。设备消失、epoch 改变或 WebSocket 断开时清除对应缓存。
- 本机 Agent 已重启以临时解除旧 endpoint 卡死；正式二进制与公网 Web／Relay 的部署结果记录在测试文档。
- 验收证据见 [`docs/testing/2026-10-11-0070-agent-read-timeout-and-thread-list-cache.md`](../testing/2026-10-11-0070-agent-read-timeout-and-thread-list-cache.md)。
