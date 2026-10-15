# 0086：大会话渐进打开、follower 大帧 hint 与 live controller 暖保留

- 状态：Implemented（2026-10-15；Go 全量、Web 全量 210、真实移动 Chromium 31 项含新增渐进打开双场景、build 已验；物理 iPhone／Safari 与真实大会话 2 秒级首屏待用户复验）。
- 来源：手机 Web 打开长会话时长时间停留在“正在同步会话／等待电脑端加载原始历史”。只读探针实测：持久化历史读取约 1.2 秒、112 turn／约 7000 item、owner 首个完整状态帧约 1.73 MiB 时默认 follower 先报 frame 超限，再由 64 MiB large follower 重试；首屏被绑定在完整 owner 冷启动链路上。
- 关联：[0013 大会话最近窗口与 64 MiB 有界同步](0013-paged-history-and-large-owner.md)、[0072 四 turn 首屏窗口](0072-four-turn-initial-history-window.md)、[0073 增量同步与无白屏缓存恢复](0073-incremental-thread-sync-and-stale-view-cache.md)、[0083 前后台与会话切换的非破坏性恢复](0083-nondestructive-foreground-resume.md)、[0085 后台同步发送按钮加载态](0085-background-sync-send-button-loading.md)。

## 目标与边界

把“先读到历史内容”与“取得可操作的 fresh owner 状态”拆成两个阶段：

- 历史阅读尽快可用；发送、停止、审批、interaction 和 queue 变更仍必须等 fresh owner，继续走既有 fencing。
- 不用缓存或持久化历史冒充实时 owner 状态；迟到的 owner snapshot 不得覆盖用户已经切换到的会话。
- 与 Issue #2（Web history cache 生命周期）和 #4（Relay 注册与重连）互不重叠：本规格只处理网络与 Agent 都健康时的冷订阅阻塞。

## 用户行为

1. Web 选中没有任何可用缓存视图的会话后，并行发出只读快路径 `thread.read` 与慢路径 `thread.subscribe`；两者互不等待。
2. 快路径先返回且订阅尚未被接受时，立即用持久化历史建立视图：`runtime: "unknown"`、`pendingInteractions: []`、`permissions` 为 `unknown`，`syncingView` 保持 true。正文可读、可翻页、可写草稿；既有横幅显示“正在同步最新状态 · 当前内容可继续阅读和编辑草稿，操作暂时禁用。”，发送、停止、审批、interaction 和 queue 操作全部禁用。
3. fresh `thread.snapshot` 到达后按既有 identity fencing 校验并覆盖视图：runtime、permissions、pending interactions、queue 与当前 active turn 以 owner 为准，重叠历史直接由 owner 窗口替换；不清空正文、不把已上滚的用户拽回底部（沿用阅读位置锚点）。
4. 订阅失败而快路径视图已建立时，保留该历史视图并降级为既有“历史只读”状态，附明确错误提示，用户可重新选择会话重试；不回退成空白“正在同步会话”。
5. 快路径自身失败或慢于订阅回执时不改变既有行为；已有 warm／缓存视图的会话不触发快路径。

## Agent 行为

### 初始 snapshot 保持有界

- `thread.subscribe` 首个 snapshot 沿用既有上限：最多 `recentTurnLimit`（4）个最近 turn、序列化后 7 MiB 页预算，巨型 turn 走 `itemsComplete: false` 的 item 分页（每页最多 100 item）；更早内容继续由 `thread.history`／`thread.history.items` 分页补读。本规格为这条既有行为补齐显式测试，不放宽任何上限。

### owner 帧大小 hint

- Agent 以 threadId 为键记录有界 LRU hint（容量 128），标记该会话最近一次是否需要 large follower。
- 无 hint 时仍先试普通 follower；普通 follower 报 `ErrFrameTooLarge` 时记录 hint 并走既有 64 MiB large follower 重试（仍受全进程单名额限制）。
- 有 hint 时跳过注定失败的普通 follower，直接申请 large follower；普通 follower 成功时清除 hint。
- hint 只是连接策略，不改变任何校验、容量或安全语义；large follower 名额占用时仍按既有 `OVERLOADED` 处理。

### live controller 暖保留（warm grace）

- 最后一个 subscription 消失、且无未完成 lease／bootstrap hold／migration 时，不再立即关闭 live follower，而是进入 90 秒暖保留（`idleGrace`，测试可注入更短值）。
- 暖保留期间重新订阅或发起变更，直接复用现有 live follower，不重新走 owner discovery／deep link／complete-history。
- 暖保留到期、controller 被容量 LRU 淘汰、`invalidateLive`（native state uncertain／owner 变化）、或 Service `Close` 时，live follower 仍立即关闭；暖保留绝不代表 Web 已订阅，重新进入仍建立新的 subscription／stream identity。
- agentEpoch 与 deviceId 天然由 Agent 进程生命周期界定：Agent 重启即新进程、新 Service，暖保留随之消失。

### 阶段可观测性

- Agent 以 `log/slog` JSON 输出订阅链路结构化阶段日志，至少覆盖：`owner-discovery`、`deep-link-open`、`follower-initialize`（含 owner frame bytes）、`small-frame-overflow`、`large-follower-retry`、`owner-attach`、`normalize`、`queue-attach`、`first-snapshot`（含 snapshot bytes 与总耗时）。
- 每条记录含 attempt ID 与 threadId 的不可逆摘要（SHA-256 前 8 字节 hex），以及耗时与错误类别；绝不记录 token、正文或原始 snapshot。

## 不变量与错误处理

- 只读历史视图、Web cache 与暖保留 snapshot 均不开放任何 mutation；`turn.start`、`turn.interrupt`、`interaction.respond`、`queue.*` 仍要求 fresh owner 并通过既有校验。
- fresh snapshot 仍按 thread／device／subscription／stream identity 校验；用户已切换会话时迟到的 read 或 snapshot 一律丢弃。
- Relay 不保存正文；Agent 不建立第二套会话数据库；hint 与暖保留都在内存中、有界、随进程消失。
- 暖保留不延长任何错误容忍：`RESYNC_REQUIRED`／`NATIVE_STATE_UNCERTAIN` 路径保持立即失效。

## TDD 与验收

1. Red→Green（Agent）：暖保留内复用 live follower 且不重新 attach；到期后关闭；容量淘汰与 `invalidateLive` 仍立即关闭；`Close` 立即关闭全部。
2. Red→Green（Agent）：已知大帧会话第二次打开不再先走普通 follower；普通成功清除 hint；hint LRU 有界。
3. Red→Green（Agent）：首个 snapshot 的 turn 上限、`recentComplete` 标记与 7 MiB 预算有显式测试。
4. Red→Green（Agent）：阶段日志包含全部上述阶段、attempt ID 与 threadId 摘要，且输出不含原始 threadId。
5. Red→Green（Web）：`thread.read` 先返回时历史先可见、操作禁用、横幅出现；snapshot 到达后无闪空地接管并启用操作；订阅失败时保留只读历史；快路径不覆盖已有缓存视图；迟到 read 不覆盖已切换会话。
6. 真实浏览器（Playwright 移动 viewport + 隔离 WebSocket fixture）回归渐进打开全链路；物理 iPhone／Safari 与真实大会话的 2 秒级首屏由用户最终复验。
