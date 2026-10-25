# 0102：低延迟发送与 Hot Cache 回流

- 状态：Implemented（2026-10-25；Go 全量／race、Web 单元／build 与移动 Chromium 全量回归通过）
- 日期：2026-10-25
- 来源：Web 点击发送后，当前必须等待 Desktop Agent 对完整 owner state 做发送前刷新和发送后轮询确认，用户消息与 composer 状态在此期间没有即时反馈；大会话的原始 owner frame 已实测约 35～37 MiB，单次完整刷新常达到数秒。
- 关联：[0001 M0 兼容性探针](0001-m0-compatibility-probe.md)、[0025 最低版本兼容性](0025-minimum-version-compatibility.md)、[0073 增量同步与无白屏缓存恢复](0073-incremental-thread-sync-and-stale-view-cache.md)、[0100 Agent 持续跟随与 Relay 双 Turn Hot Cache](0100-agent-fed-relay-two-turn-hot-cache.md)。
- 取代范围：本规格获准后，仅取代 0100 §7.3 中“普通 `turn.start` 必须在 mutation 内重新完整读取 owner state”和既有 Desktop IPC `turn.start` 的发送后主动轮询确认；停止、审批、补充回答、queue／steer、创建会话和其他 mutation 的确认语义不变。

## 1. 当前链路与根因

当前普通文本／图片发送实际经过：

1. Web 发送一次 `turn.start`；Relay 只做 request ID 重写和 Web→Agent 路由，没有触发 `thread.read` 或新建会话订阅。
2. Desktop Agent 在当前 hot controller 上检查 live owner。
3. Desktop IPC follower 主动调用 `thread-follower-load-complete-history`，等待一份新的完整 owner state。
4. follower 向精确 Desktop owner 发送一次 `thread-follower-start-turn`。
5. owner 已返回新的真实 `turnId` 后，follower 仍每 200 ms 再次请求完整历史，最长 8 秒，直到读回同一 `turnId + clientMessageId + input` 才向 Web 返回 accepted。
6. Web 在 accepted 前保留 composer 原文且不投影 pending user message，因此上述确认成本全部表现为“点击后页面迟迟没有更新”。
7. 与第 5 步并行，既有 Agent hot follower 已经会把 turn 结构变化立即推到 Relay cache，再由 `thread.cache.update` 广播给 Web；第 5 步的主动轮询与这条持久订阅回流重复。

对大会话，完整 owner state 的读取、传输和归一化远比一次 mutation frame 昂贵。主要矛盾不是 Web→Relay 网络跳数，而是 mutation 路径重复读取完整 owner state，并把权威回流与请求回执串行绑定。

## 2. 目标链路

普通 `turn.start` 改为两条并行但职责不同的链路：

### 2.1 命令链路

`Web → Relay → Desktop Agent → 当前 Codex Desktop owner`

1. Web 每次点击只发送一次 `turn.start`，携带唯一 `clientMessageId`；不得自动重试。
2. Relay 继续只透明路由 mutation，不读正文、不创建 read subscription、不等待 cache 更新。
3. Desktop Agent 只允许在目标 hot controller 仍连接、owner identity 仍匹配、当前 materialized native state 可寻址且显示 idle 时发送。
4. Desktop Agent 使用 hot follower 已持续维护的最新 materialized owner state做本地 fencing，不再为每次发送额外请求完整历史。
5. 精确 owner 对 `thread-follower-start-turn` 返回格式合法、非空且在发送前 snapshot 中不存在的新 `turnId`，即构成 `turn.start accepted` receipt；Desktop Agent 立即把该 receipt 返回 Relay／Web。
6. native 调用报错、返回格式异常、返回已有 `turnId`，或在拿到合法 receipt 前连接中断时，继续返回 rejected／unknown；不得根据 optimistic UI 或 Relay cache 猜测 accepted。

### 2.2 展示链路

`Codex Desktop owner event → Agent hot follower → Relay hot cache → Web watcher`

1. owner event 仍由已有 hot follower 持续接收；turn 开始、user item 出现、runtime 或 item 结构变化时立即生成双 turn cache snapshot，纯文本 streaming 继续最多每配置间隔合并一次。
2. Relay 原子替换 cache，并向当前所有 watcher 广播；Web 不在发送后调用 `thread.read`、`thread.subscribe`、`thread.cache.read` 或轮询 Agent。
3. Web 只把 cache 中出现的真实 `clientMessageId` 视为正文已同步；cache 仍是展示材料，不是 mutation receipt 或第二个 SSOT。

## 3. Web 即时反馈与校正

1. 点击发送且通过本地校验后，Web 在当前会话末尾立即显示一条仅存在于当前页面内存的 pending user message，内容来自本次 submission snapshot，并标明“发送中”。该投影不得写入 Relay、IndexedDB 或会话历史 cache。
2. pending projection 使用 `deviceId + agentEpoch + threadId + clientMessageId` 定位；不能只按文本去重。
3. composer 在 native accepted receipt 返回前继续保留原草稿并禁用重复发送；receipt 通常应很快返回。accepted 后仅在 composer revision 未变化时清空原草稿。
4. Relay cache 出现同一 `clientMessageId` 后，Web 删除 pending projection，由权威 user item 原位接管；页面不得短暂显示两条相同消息，不改变用户当前阅读锚点。
5. 明确 rejected 时移除 pending projection并保留原草稿；unknown 时保留原草稿和一条“结果待确认”的 pending projection，禁止自动重发。后续 cache 若观察到精确 `clientMessageId`，按既有 reconciliation 清空仍未被编辑过的草稿并转为权威 item。
6. accepted receipt 已返回但 cache 尚未回流时，pending projection 显示“已提交，等待同步”，不得触发全量读取。若 hot stream 失效，沿用 cache stale／resync 状态提示，但不重放 mutation。
7. 用户在等待期间切换会话时，pending submission 按会话隔离；返回原会话后仍可恢复本页内存中的 pending projection。刷新页面不持久化未确认 mutation。

## 4. 安全与一致性不变量

1. Codex Desktop 仍是 thread／turn／item 的唯一 SSOT；optimistic projection 只是 UI 状态，不能开放停止、审批、queue 或其他后续 mutation。
2. 只有精确 owner 返回的合法新 `turnId` 才能产生 accepted receipt。普通 transport `ok`、Relay 已转发、cache runtime 变化或相同文本均不足以证明 accepted。
3. `clientMessageId` 继续由 Web 每次显式发送生成一次；Desktop Agent 的 recent-ID gate 继续阻止同一 ID 再次执行。unknown 结果不自动重试。
4. hot follower／Agent 连接失效、owner identity 不确定、materialized state 无法解析或显示非 idle 时 fail closed；不得为了速度退回 Relay cache 授权 mutation。
5. 普通 `turn.start` 不再强制“发送前完整刷新”，不表示跳过 live owner 校验：它复用 0100 已持续维护且与连接生命周期绑定的 hot controller state。controller 不 fresh 时发送入口本就必须禁用或返回 `RESYNC_REQUIRED`。
6. 本规格不放宽 interaction response 的确认要求。0001 已证明某些不存在的 request 也可能得到 `ok: true`；审批／补充回答仍须状态回显确认。
7. 带 file context 的发送仍需在 Desktop Agent 本地读取并验证引用内容；该必要工作可以单独计时，但不能把文件内容写入 Relay 日志或绕过 Chilling／路径权限边界。

## 5. 可观测性与性能验收

新增不含正文的阶段计时：

- Web：click→request queued、request queued→receipt、click→authoritative cache item；
- Desktop Agent：fence、file-context preparation、native start call、receipt return；
- cache：owner event→Agent snapshot、Agent snapshot→Relay broadcast。

不得记录草稿、图片、文件引用正文、完整路径、token 或原始 IPC frame。

对已打开且 fresh 的 hot 会话：

1. click→pending projection 在同一浏览器 task／下一渲染帧完成，不等待网络。
2. 每次普通发送恰好一次 native start mutation；发送路径产生 0 次 `thread-follower-load-complete-history` 和 0 次发送后主动 `Refresh`。
3. 合法 native receipt 到达后立即返回 Web，不等待 user item 回显或 assistant output。
4. turn／user item 结构更新由 cache 立即发送；后续纯文本增长延迟继续服从 0100 默认最多约 1 秒的合并窗口。
5. 性能报告分别给出 receipt latency 与 authoritative display latency，不能用 optimistic projection 冒充端到端同步完成。

## 6. TDD 计划

### 6.1 Desktop IPC／Agent

1. Red：warm follower 普通 `StartWithImages` 只读取内存中的 current snapshot，调用一次 `thread-follower-start-turn`，不调用 complete-history refresh。
2. Red：合法的新 `turnId` 立即 accepted，不等待后续 snapshot；已有／空／malformed turn ID 返回 unknown。
3. Red：stale／不可寻址／busy snapshot 不发送 native mutation；断线和 timeout 不重试。
4. Red：Service recent-ID gate 在快速 receipt 下仍拒绝重复 `clientMessageId`；Go race 覆盖并发双击和 Desktop 同时发起 turn。
5. 保留审批、回答、queue／steer 的既有 read-back confirmation 测试，证明本规格没有扩大到其他 mutation。

### 6.2 Web

1. Red：点击后在 receipt 前立即出现“发送中”user projection，同时 composer 保留且按钮禁用。
2. Red：accepted 后 composer 按 revision 安全清空，projection 保留为“等待同步”；精确 cache item 到来后无重复消息。
3. Red：rejected 保留草稿并移除 projection；unknown 保留草稿、显示待确认且不重试，精确 cache item 可随后完成 reconciliation。
4. Red：相同文本但不同 `clientMessageId` 不得错误去重；切会话、返回、stream resync 和新草稿 revision 均不丢内容。
5. Playwright 移动 viewport 断言点击即反馈、输入区不跳动、cache 回流后阅读锚点稳定。

### 6.3 真实兼容性

1. 仅在隔离 fixture 对当前 Codex Desktop 发送短消息，记录 native receipt 与 hot-cache 回流的阶段耗时，并核对真实 `turnId + clientMessageId`。
2. 覆盖普通文本、图片、文件 context 各一次；不得向业务会话注入验收消息。
3. 模拟 receipt 丢失与 cache 延迟，确认不自动重发、不出现重复 user item。
4. 当前 Codex 版本若不能稳定保证 owner response 中的新 `turnId` 表示 turn 已创建，则本优化标为 unverified，并保留原 read-back gate；不能用 UI 速度掩盖兼容性失败。

## 7. 验收标准

1. 已打开的 fresh hot 会话点击发送后立即出现 pending user message，不再等待数秒才有页面变化。
2. 普通发送路径严格为一次 Web→Relay→Agent→Desktop owner mutation；无额外完整历史 refresh、read subscription 或发送后轮询。
3. 权威内容只通过既有持久 hot follower→Relay cache→Web watcher 回流，且精确 `clientMessageId` 接管 optimistic projection。
4. rejected／unknown、重复 ID、并发 turn、断线和 cache stale 均不产生自动重放或虚假 accepted。
5. Go 全量与 race、Web 全量与 production build、移动 Playwright 及隔离真实 Desktop fixture 均通过后，才能随 0100／0094 的待发布版本一起 push 和部署。
