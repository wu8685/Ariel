# 0103：轻量历史首屏与 Owner 后台同步

- 状态：Implemented（2026-10-25；Go 全量／race、Web 单元／build 与移动 Chromium cache fixture 通过）
- 日期：2026-10-25
- 依据：[0100 Agent 持续跟随与 Relay 双 Turn Hot Cache](0100-agent-fed-relay-two-turn-hot-cache.md)、[0102 低延迟发送](0102-low-latency-turn-start.md)、[轻量 Owner 状态兼容性探测](../compatibility/2026-10-25-lightweight-owner-state.md)。
- 取代范围：取代 0100 §7.2.3 中“支持 0100 的组合不得在 cache miss 时使用 `thread.read`”的限制；fresh Relay cache 命中仍不得穿透 Agent。

## 1. 已验证事实

1. 当前 Codex Desktop `26.930.31730`／bundled Codex `0.160.0` 中，只发送 owner follow、不调用 `thread-follower-load-complete-history`，owner 仍主动发送约 39 MiB 的完整 baseline。
2. 公共 App Server 的 `thread/read(includeTurns:false)` 可独立读取约百字节 metadata；`thread/turns/list(limit:2, itemsView:full)` 可有界读取最近两个完整 turn。
3. Desktop Agent 已在发送 Relay 前规范化并裁剪 owner state；大帧只存在于 Codex App → Desktop Agent 本地 IPC，但仍造成冷启动等待与内存开销。

## 2. 目标与非目标

### 2.1 目标

1. Relay cache miss／stale 时，Web 不再以空白页面等待 owner baseline。
2. Web 经 Relay 向 Desktop Agent 发起一次只读 `thread.read`；Desktop Agent 使用 App Server metadata + 最近 2 turn，规范化后返回。
3. owner follower 继续在后台建立。fresh cache 到达前禁止发送、停止、审批与回答；草稿仍可编辑。
4. fresh cache 原子接管轻量历史，不能重复 turn、清空正文或改变用户阅读锚点。
5. owner promotion 明确失败时停止无限 spinner，保留已加载历史并进入只读状态。

### 2.2 非目标

1. 本规格不声称消除 Codex App → Desktop Agent 的约 39 MiB owner baseline；当前版本没有经过验证的 metadata-only owner IPC。
2. 不改变 Codex 作为会话与执行状态 SSOT 的边界，不允许根据 App Server 历史开放 mutation。
3. 不增加 Relay 正文数据库、离线 mutation 队列或 mutation 自动重试。

## 3. 打开流程

1. fresh `thread.cache.watch` 命中时沿用 0100：立即展示，零 `thread.read`、零 Web-owned owner subscription。
2. cache missing 时：
   - 保留 watcher；
   - 至多发送一次 `thread.cache.promote`；
   - 并行发送一次 `thread.read`；
   - `thread.read` 成功后立即展示最近 2 turn，但保持 `syncing`，发送按钮显示同尺寸 spinner并禁用。
3. stale cache 时立即展示 stale cache；若本地没有可展示正文，也并行执行一次 `thread.read`。已有 stale 正文时不得用较旧的 read 结果覆盖它。
4. `thread.cache.update(fresh)` 到达后，以 cache stream identity／seq 原子接管，解除同步状态并开放符合既有权限门槛的操作。
5. fresh cache 已接管后到达的迟发 `thread.read` 必须丢弃。
6. 用户切换会话后，旧会话的迟发 read／promotion 结果不得写入当前页面。

## 4. 错误处理

1. `thread.read` 失败但 promotion 仍在进行时可以继续等待 owner；不得清除已有 IndexedDB／stale cache 内容。
2. Agent 通过 `thread.cache.status=error` 报告 `TIMEOUT`、`HISTORY_TOO_LARGE`、`AGENT_CAPACITY`、`NATIVE_STATE_UNCERTAIN`、`RESYNC_REQUIRED` 等终态错误时，Relay 即使尚无正文也必须向 watcher 保留 `staleReason`。
3. Web 收到上述终态错误后停止 spinner：有轻量历史则保持只读，无历史则展示明确错误；不得无限显示“正在同步会话”。
4. lower-case 的 `agent_epoch_changed` 等 stale 原因仍表示后台恢复中，不作为终态失败。

## 5. 数据与容量边界

1. `thread.read` 继续由 Desktop Agent 的 `recentTurnLimit=2` 约束；每个 turn 保留完整可展示 item，超限沿用既有 item 子分页和 `HISTORY_TOO_LARGE` 语义。
2. Desktop Agent → Relay／Web 不发送原始 owner state、reasoning、完整旧历史或未经规范化的 tool output。
3. Relay cache 与 Web IndexedDB 的现有字节上限保持不变。

## 6. TDD 与验收

1. Red：cache miss 被接受后同时出现一次 `thread.read` 和一次 promotion；read 先返回时正文可见、spinner存在、发送禁用。
2. Red：fresh cache update 接管后 spinner消失、发送恢复；迟发 read 不覆盖 fresh cache。
3. Red：cache status error 在空 cache entry 上仍广播 `staleReason`；Web 停止无限同步并保留轻量历史。
4. Red：fresh cache 命中仍为零 `thread.read`。
5. Go 全量／race、Web 全量／build 和移动真实浏览器 fixture 通过；真实大会话验收记录首屏 read 与 owner fresh 各自耗时，不能用轻量历史出现冒充 owner 已就绪。
