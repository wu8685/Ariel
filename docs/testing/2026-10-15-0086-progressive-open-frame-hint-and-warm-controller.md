# 0086 大会话渐进打开、follower 大帧 hint 与 live controller 暖保留验收

- 日期：2026-10-15
- 规格：[`0086-progressive-open-frame-hint-and-warm-controller.md`](../specs/0086-progressive-open-frame-hint-and-warm-controller.md)
- 来源：GitHub Issue #5
- 结论：实现、Go 全量测试、Web 全量测试（210）、真实移动 Chromium（31，含新增渐进打开双场景）、production build 均通过；物理 iPhone／Safari 与真实 100+ turn 大会话的 2 秒级首屏待用户复验。

## 实现行为

- Web 选中无缓存视图的会话后并行发出 `thread.read`（快路径）与 `thread.subscribe`（慢路径）。快路径先返回时立即建立 `runtime: "unknown"` 的只读渐进视图，沿用“正在同步最新状态 · 当前内容可继续阅读和编辑草稿，操作暂时禁用。”横幅；发送、停止、审批、interaction、queue 均因 `ownerReady === false` 保持禁用。
- fresh `thread.snapshot` 到达后按既有 thread／device／subscription／stream fencing 覆盖渐进视图，阅读位置锚点在接管时保留（且绝不覆盖 resync 已捕获的锚点）；重叠 turn 由 owner 窗口替换。
- 订阅失败时：渐进视图已建立则原地降级为“历史只读”并附可重试提示；`HISTORY_TOO_LARGE`／`OVERLOADED` 复用同一次快路径 `thread.read`，不再重复请求；快路径也失败时维持既有“会话状态无法确认”。
- Agent 侧记录有界（128 条 LRU）的 per-thread owner 帧大小 hint：已知超限的会话跳过注定失败的 8 MiB 普通 follower，直接申请 64 MiB large follower（仍受全进程单名额限制）；普通 follower 成功会清除 hint。
- 最后一个 Web subscription 消失后 live follower 进入 90 秒暖保留；暖保留内重新订阅或发起变更直接复用，不再重新走 owner discovery／deep link／complete-history。容量 LRU 淘汰、`invalidateLive`（native state uncertain）与 Service `Close` 仍立即关闭；暖保留不创建任何 subscription identity，重新进入仍生成新的 subscription/stream ID。
- 订阅链路以 `log/slog` JSON 输出结构化阶段日志：`owner-discovery`、`deep-link-open`、`follower-initialize`（含 owner frame bytes）、`small-frame-overflow`、`large-follower-retry`、`owner-attach`、`normalize`、`queue-attach`、`first-snapshot`／`first-snapshot-send`（含 snapshot bytes 与总耗时）。threadId 一律以 SHA-256 前 8 字节 hex 摘要出现，不记录 token、正文或原始 snapshot。
- 首个 `thread.snapshot` 的既有有界语义（最多 4 个最近 turn、7 MiB 预算、巨型 turn 走 item 分页）补充了显式测试。

## TDD 证据

Red 阶段（实现前运行）：

- 新增 Go 测试编译失败：`openFollowerWithFallback` 尚无 threadID 参数，`resetFrameHints`／`maxFrameHintEntries`／`frameHintPrefersLarge` 等不存在。
- 新增 Web 测试 `shows persisted history while the owner sync is still in flight` 失败：`thread.read` 从未发出，等待超时。
- 新增 e2e `mobile-progressive-open.spec.ts` 两个场景在旧 `App.tsx` 下均失败（git stash 验证）。

Green 阶段覆盖：

- Agent：暖保留内复用同一 live follower 且不重复 attach（`TestServiceKeepsLiveFollowerWarmAfterLastUnsubscribe`）；到期关闭并重新 attach（`TestServiceWarmGraceExpiresAndNextSubscribeReattaches`）；退订后 native state uncertain 仍立即关闭（`TestServiceInvalidationClosesWarmFollowerImmediately`）；`Close` 立即关闭暖 follower；暖保留内变更复用 follower；到期时已有新订阅则跳过关闭（`TestServiceWarmRetireSkipsCloseWhileResubscribed`）；容量淘汰仍立即关闭（更新后的 `TestServiceBoundsControllersAndRetiresIdleFollower`）。
- Agent：大帧 hint 跳过第二次注定失败的小 follower（`TestFrameHintSkipsSmallFollowerForKnownLargeThread`）、普通成功不置 hint、hint LRU 有界。
- Agent：首个 snapshot 最多 4 turn 且 `historyComplete: false`（`TestServiceInitialSnapshotBoundsRecentWindow`）。
- Agent：阶段日志覆盖 `owner-attach`／`normalize`／`first-snapshot`，且不泄露原始 threadId（`TestSubscribeLogsStructuredPhasesWithoutRawThreadID`、`TestThreadDigestIsIrreversibleAndStable`）。
- Web：快路径先渲染、操作禁用、snapshot 无闪空接管并恢复发送（`shows persisted history while the owner sync is still in flight`）；订阅超时保留只读历史并提示重试（`keeps the progressively loaded history read-only when the owner sync fails`）；迟到的快路径不覆盖 fresh snapshot（`ignores a late read fast path once the fresh owner snapshot arrived`）。
- 真实浏览器：`mobile-progressive-open.spec.ts` 在 390×844 Chromium 上验证渐进渲染、同步中禁用、snapshot 接管与超时降级。

## 未覆盖与待复验

- 真实 100+ turn／1 MiB+ owner 帧的 2 秒级首屏、物理 iPhone／Safari：待用户复验。
- `openFollower` 的真实 socket 路径（含 `follower-initialize` 的 frame bytes 记录）仍由既有 Desktop fixture 集成测试覆盖，本次未改动其网络行为。
- 公网 `www.kan.chat` 部署未执行：本机 SSH 未获授权（见 AGENTS.md 公网部署边界）。
