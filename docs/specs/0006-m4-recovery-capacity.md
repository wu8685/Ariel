# 0006：M4 断线恢复与有界容量

- 状态：实施中。遵循用户授权的连续开发范围；不改变无离线投递、原 owner 为权威、未知结果不自动重发的产品约束。
- 依据：[MVP 验收矩阵](baseline/2026-10-03-codex-remote-delivery.md)、[M1 链路](0003-m1-relay-mock-web.md)、[M2 Agent](0004-m2-real-desktop-agent.md)。
- 后续规格优先：[0013 大会话有界同步](0013-paged-history-and-large-owner.md)已将本规格中“App Server 子进程退出即取消 Relay 会话”改为**只重启只读子进程**，并将“大历史超 8 MiB 即整会话失败”改为**本机最多 64 MiB、Web 最近 10 turn 与旧历史按需分页**；Relay→Web 单帧 8 MiB 与原 owner 失效时 fail closed 的边界仍保持。

## 行为

- Web 的 socket 临时断开后，在同一页面内保留 token、设备、当前 thread 选择与未确认草稿；自动重连 Relay，读取新 epoch/device 状态，再对原 thread 建新 subscription，从 owner 全量快照恢复。旧 stream/seq 不得混入新视图。
- 已选 Desktop Agent 单独出现短暂离线时，Web 保留最后确认的会话视图至多 8 秒，并立即把发送、停止、审批和队列变更切为不可用；Agent 在宽限期内恢复后，Web 放弃旧 subscription、重新读取设备状态并建立新 subscription，不显示持久离线错误。只有离线持续超过 8 秒才清空旧视图并显示明确离线提示。宽限期不得把旧视图当作在线状态，也不得缓存或重放写请求。
- Relay 或 Agent 重启使转发中的变更结果变为 `unknown`，Web 不重发；旧 agentEpoch 的 routes/subscriptions 清理。仍待处理的交互只从新 owner snapshot 重建，已处理的不复活。
- Relay 对 Web 和 Agent 连接做有界 WebSocket ping；Agent 对 Relay 也做 ping。连接半开且不响应时主动关闭，让离线状态和既有重连逻辑接管，不能长期显示假在线。
- Desktop 无法访问时不能宣称 `codexReady`。不自动拉起 Desktop。用户可见失败或加载中状态。
- 单个原 owner 的 IPC follower 失效时，Agent 使该 thread 的旧 stream 明确失效（`RESYNC_REQUIRED`），释放 follower；Web 自动用新 subscription 重新取得 owner 全量快照，不继续用旧 seq。若快照结构不可归一化（包括原生历史出现无 turn ID 的条目），须以 `NATIVE_STATE_UNCERTAIN` 显式终止旧 stream、清空手机端旧视图、禁用该会话远程操作；首次订阅即发现异常也返回同一错误。Web 不自动重试这种确定性的结构异常，用户可稍后手动重新选择会话。不得静默跳过异常条目或编造 turn ID。
- Agent 建立 Relay 会话前须对当前 Desktop IPC 做有界 initialize 探测；运行中连续两次探测失败则主动断开 Relay，令设备变为离线。Desktop 恢复后按原连接策略重试，不投递离线请求。仅测试探针与状态机，不为测试退出真实业务 Desktop。
- Agent 所启动的 Codex App Server 子进程若退出，即使 Desktop IPC 仍可访问，也须立即取消当前 Relay 会话并由外层重连流程重建 App Server；不能保持“设备在线”但所有 thread 请求失败的假在线状态。
- Web 从后台回到前台时若可能错过事件，应放弃旧 subscription 并从 owner 重新获取全量快照；若 WebSocket 已半开则先重建连接。未完成变更统一标为结果未知，不自动重发。
- Relay 每 Web 未完成请求最多 32；IPC 有界事件缓冲。慢 Web 写超时即关闭其连接，让客户端重取完整快照，不能让一个慢连接无限阻塞其它会话。
- Agent 至多缓存 64 个 thread controller，每个 thread 至多接受 16 个订阅。到达上限时只能回明确的 `OVERLOADED`；有空闲、无订阅的 controller 时可先释放其 follower 并淘汰最久未用者，不能淘汰正在请求或仍被订阅的 thread。即使 controller 被淘汰，最近 4096 个已受理／结果未知的 `threadId + clientMessageId` 仍在本进程内有界去重，不能因导航历史会话而允许同一消息 ID 重放。
- Relay 至多保留每个 Web 的 32 个活跃订阅；超限 `thread.subscribe` 须在转发到 Agent 之前拒绝，以免 Relay 和 Agent 之间留下无主订阅。Web 切换会话应主动退订旧 subscription。
- Web 断开时 Relay 必须通知仍在线的 Agent 释放该 Web 的每个 subscription；已转发的订阅若在 Web 断开或超时后才返回 accepted，也须在确认其不属于现有活跃路由后释放，不能让页面刷新逐次占满 Agent 的订阅上限。清理请求不代表业务操作重试，也不向其他 Web 广播正文。
- 单帧限 8 MiB。历史/快照超过安全发送容量时，在“订阅已受理”前返回明确的 `HISTORY_TOO_LARGE`；绝不静默截断。交互上下文超限时不能提供可点击审批。
- `thread.list` 仅返回列表所需的会话身份、标题、cwd、时间与状态，不把每条历史塞进列表；历史只由选中后的读取／订阅获取。
- 已订阅会话若后续增长越过容量，Agent 发送有界的 `thread.error`（`HISTORY_TOO_LARGE`）并停止该 stream；Web 清除过期快照、显示明确错误。Relay 只向绑定的订阅者转发，不生成截断的 `thread.update`。
- 响应式 Web 在窄屏、软键盘、切后台回来后可选会话、可看状态、可输入；无后台通知承诺。
- 窄屏会话列表打开后可不切换会话直接关闭；长标题和工作目录在会话头部收束显示，不把输入框挤出可视宽度。

## TDD / 验收

1. 先测断开后保留选择、重连重新订阅、旧序列不再应用、新 snapshot 接管。
   - Agent 短暂离线不足 8 秒时保留最后确认内容且所有写操作禁用；恢复后自动重订阅且不遗留离线提示。
   - Agent 离线达到 8 秒时才清空旧视图并显示离线提示。
2. 先测 Relay/Agent 断开路由清理、变更 unknown、慢 Web 写失败的隔离。
3. 先测超大历史在 Agent 本地得到清楚错误，不先答订阅成功。
4. 手工在本地 Relay/Agent 重启、真实 fixture 待处理卡片与手机尺寸 Web 中验证；物理手机使用需目标局域网设备配合，不能以仅调窄桌面 viewport 冒称完成。
