# 0006：M4 断线恢复与有界容量

- 状态：实施中。遵循用户授权的连续开发范围；不改变无离线投递、原 owner 为权威、未知结果不自动重发的产品约束。
- 依据：[MVP 验收矩阵](baseline/2026-10-03-codex-remote-delivery.md)、[M1 链路](0003-m1-relay-mock-web.md)、[M2 Agent](0004-m2-real-desktop-agent.md)。

## 行为

- Web 的 socket 临时断开后，在同一页面内保留 token、设备、当前 thread 选择与未确认草稿；自动重连 Relay，读取新 epoch/device 状态，再对原 thread 建新 subscription，从 owner 全量快照恢复。旧 stream/seq 不得混入新视图。
- Relay 或 Agent 重启使转发中的变更结果变为 `unknown`，Web 不重发；旧 agentEpoch 的 routes/subscriptions 清理。仍待处理的交互只从新 owner snapshot 重建，已处理的不复活。
- Relay 对 Web 和 Agent 连接做有界 WebSocket ping；Agent 对 Relay 也做 ping。连接半开且不响应时主动关闭，让离线状态和既有重连逻辑接管，不能长期显示假在线。
- Desktop 无法访问时不能宣称 `codexReady`。不自动拉起 Desktop。用户可见失败或加载中状态。
- 单个原 owner 的 IPC follower 失效或快照不可归一化时，Agent 使该 thread 的旧 stream 明确失效（`RESYNC_REQUIRED`），释放 follower；Web 自动用新 subscription 重新取得 owner 全量快照，不继续用旧 seq。
- Agent 建立 Relay 会话前须对当前 Desktop IPC 做有界 initialize 探测；运行中连续两次探测失败则主动断开 Relay，令设备变为离线。Desktop 恢复后按原连接策略重试，不投递离线请求。仅测试探针与状态机，不为测试退出真实业务 Desktop。
- Web 从后台回到前台时若可能错过事件，应放弃旧 subscription 并从 owner 重新获取全量快照；若 WebSocket 已半开则先重建连接。未完成变更统一标为结果未知，不自动重发。
- Relay 每 Web 未完成请求最多 32；IPC 有界事件缓冲。慢 Web 写超时即关闭其连接，让客户端重取完整快照，不能让一个慢连接无限阻塞其它会话。
- 单帧限 8 MiB。历史/快照超过安全发送容量时，在“订阅已受理”前返回明确的 `HISTORY_TOO_LARGE`；绝不静默截断。交互上下文超限时不能提供可点击审批。
- `thread.list` 仅返回列表所需的会话身份、标题、cwd、时间与状态，不把每条历史塞进列表；历史只由选中后的读取／订阅获取。
- 已订阅会话若后续增长越过容量，Agent 发送有界的 `thread.error`（`HISTORY_TOO_LARGE`）并停止该 stream；Web 清除过期快照、显示明确错误。Relay 只向绑定的订阅者转发，不生成截断的 `thread.update`。
- 响应式 Web 在窄屏、软键盘、切后台回来后可选会话、可看状态、可输入；无后台通知承诺。

## TDD / 验收

1. 先测断开后保留选择、重连重新订阅、旧序列不再应用、新 snapshot 接管。
2. 先测 Relay/Agent 断开路由清理、变更 unknown、慢 Web 写失败的隔离。
3. 先测超大历史在 Agent 本地得到清楚错误，不先答订阅成功。
4. 手工在本地 Relay/Agent 重启、真实 fixture 待处理卡片与手机尺寸 Web 中验证；物理手机使用需目标局域网设备配合，不能以仅调窄桌面 viewport 冒称完成。
