# 0107：Agent Transport 解耦与 Cache 自愈恢复

- 状态：Implemented（用户于 2026-10-25 确认；2026-10-26 完成实现与自动化验收）
- 日期：2026-10-25
- 依据：[0100 Agent 持续跟随与 Relay 双 Turn Hot Cache](0100-agent-fed-relay-two-turn-hot-cache.md)、[0103 轻量历史首屏与 Owner 后台同步](0103-lightweight-history-first-owner-sync.md)、[0104 原生大字段 Lazy 化与当前会话 Tool 实时摘要](0104-lazy-native-payloads-and-selected-tool-sync.md)
- 取代范围：本规格取代 0100 §6.3、§7.1.4、§7.2 中关于 freshness、重新 watched 必然重绑 owner、Agent 重连后全量恢复 cache follower 的行为；未明确取代的容量、安全和 mutation fencing 规则继续有效。

## 1. 问题

当前实现存在六类相互放大的故障：

1. Relay WebSocket 断开会关闭 Desktop Agent 的 App Server RPC、Service 和全部 cache follower；短暂网络抖动被放大为所有会话重新加载。
2. 大 snapshot 写入与 Ping／Pong 竞争写锁，可能导致 heartbeat 超时、Agent 断线和 `duplicate agent` 重连循环。
3. Relay cache 已命中时，首个 watcher 仍触发 `thread.cache.touch(watching=true)`；Desktop Agent 会注销 warm follower 并重新 attach owner，使已缓存会话再次进入长时间同步。
4. 内部 cache promotion 遇到队列过载、超时或 Agent 断线时，Relay 可能不清除 `cachePromoting`，后续 promotion 被永久抑制。
5. Agent 重连后 Relay 串行恢复所有有 cache 的会话；无 watcher 会话可能阻塞用户当前打开的会话。
6. Agent epoch 变化时 Web 删除旧 IndexedDB cache，失去断线恢复时最有价值的 stale／read-only 内容。

## 2. 目标与非目标

### 2.1 目标

1. Relay transport 的短暂断线不得销毁 Desktop Agent 的 Codex RPC、Service、owner controller 或 cache follower。
2. 控制帧必须在数据面拥塞时仍可按 heartbeat deadline 完成；cache snapshot 不得形成无界写队列。
3. fresh warm cache 重新 watched 时立即可读；只有 follower lease 明确过期、stream gap、owner 失效或 mutation 现场校验失败时才重新绑定 owner。
4. promotion 在成功、失败、过载、超时、断线和取消路径上都进入可观察终态，并允许后续安全重试。
5. 恢复顺序以当前 watcher／active turn 为先；无 watcher 历史 cache 不主动 cold attach。
6. Web 保留旧 Agent epoch 的双 turn display cache，以 stale／read-only 形式立即展示；旧 epoch 状态绝不授权 mutation。

### 2.2 非目标

1. 不改变 Codex 作为 thread、turn、item 和 mutation 的唯一 SSOT。
2. 不把 Relay cache、IndexedDB cache 或 transport receipt 提升为 mutation owner receipt。
3. 不增加 per-Web、per-thread WebSocket；Agent 与 Relay、Web 与 Relay 仍分别使用一条多路复用连接。
4. 不在 Relay 或浏览器持久化图片二进制、tool detail、完整 diff 或超过 0100／0104 边界的正文。
5. 不通过自动重放恢复任何发送、停止、审批、回答或 queue mutation。

## 3. Desktop Agent 生命周期

### 3.1 进程级 Service

1. App Server RPC、Codex Adapter、Desktop Service、owner controller 与 cache follower 属于 Desktop Agent 进程生命周期，而不是单次 Relay WebSocket 生命周期。
2. Relay transport 建立、断开和重连只替换 transport session；不得关闭进程级 Service 或 follower。
3. Desktop Agent shutdown、凭据撤销、设备解绑或不可恢复的 App Server 错误才关闭 Service。
4. transport 断线期间 follower 可以继续 materialize 最新双 turn snapshot；每个 thread 只保留一个尚未发送的最新 snapshot，旧的待发送 snapshot 被覆盖。
5. transport 重连并完成认证后，Agent 只发送当前需要恢复的最新 snapshot／status，不回放中间 snapshot 或 mutation。

### 3.2 Transport epoch 与接管

1. 每次 Agent WebSocket 认证成功生成单调递增或不可复用的 transport generation，并绑定当前 `agentEpoch`。
2. Relay 收到同一注册设备的新认证连接时，可以原子替换旧 transport；旧 transport 随即失去 route、cache event 和 mutation response 的写权限。
3. 旧 transport 上迟到的响应、snapshot 或 status 必须按 generation／epoch 丢弃。
4. 新 transport 接管不得重建 Codex follower；它只重新声明 follower lease 与推送所需最新状态。
5. Relay 不能仅凭客户端声明替换其他认证主体或其他设备的连接。

## 4. Heartbeat 与数据面背压

1. cache snapshot、普通 RPC response 和 heartbeat control frame 不得依赖一个可被大 payload 长时间占用的应用层写锁。
2. Agent 使用有界 outbound scheduler：control／auth／terminal status 优先，mutation receipt 次之，每个 thread 的 cache snapshot 为 latest-only。
3. 同一 thread 同一时刻最多存在一个正在发送和一个等待发送的 cache snapshot；新 snapshot 覆盖等待项。
4. snapshot 发送超过独立 write deadline 时中止该数据帧、标记 stream stale，并保留最新 snapshot 等待下一 transport；不得阻止 Ping／Pong 直到 heartbeat deadline。
5. Relay 向 Web watcher 广播同样采用有界 latest-only 策略；慢 Web 客户端不能反向阻塞 Agent socket。
6. 所有超时、丢弃和覆盖日志只记录 thread opaque key、bytes、duration、seq 和原因，不记录正文。

## 5. Follower lease 与 Cache freshness

### 5.1 Lease

1. Desktop Agent 为每个 hot follower 维护独立于正文变化的 lease；空闲会话不需要伪造 turn 更新来证明存活。
2. Agent 定期在既有多路复用 transport 上发送有界 lease summary，包含 follower identity、stream identity、最后 seq 和状态，不携带正文。
3. Relay 以最近一次合法 snapshot 或 lease summary 更新 `leaseValidUntil`。
4. freshness deadline 采用服务端配置，默认建议为 90 秒；该值必须可测试、可配置，且不得按会话内容大小动态延长。

### 5.2 Freshness 判定

Relay 仅在以下条件全部成立时返回 `fresh`：

1. Agent 在线且认证主体、设备注册 identity、`agentEpoch` 匹配；
2. cache stream identity 和 seq 连续；
3. follower lease 未超过 `leaseValidUntil`；
4. entry 未被 owner invalidation、capacity eviction、oversized、native uncertainty 或明确 error 标记 stale。

`capturedAt` 表示正文 snapshot 时间，`leaseValidUntil` 表示 Relay 内部的 follower 活性期限，两者不得混用。`leaseValidUntil` 不进入 Web wire；会话长期无内容变化时，旧 `capturedAt` 可以与 fresh lease 共存。

### 5.3 Rewatch

1. fresh cache 的 watcher 从 0 变 1 时，Relay 只增加 watcher 并立即返回 cache，不发送强制 owner rebind。
2. Relay 可以发送轻量 recency touch，但 touch 不注销 follower、不 attach owner、不把 Web watcher 映射成新 follower。
3. lease 过期、stream gap 或明确 stale 时，Relay 才发起一次 coalesced promotion。
4. Desktop Agent 若确认 follower 仍有效，应通过 lease／status 恢复 fresh；只有 follower 不存在或 owner 失效时才 cold attach。

## 6. Promotion 状态机

每个 device／thread promotion 只能处于：

`idle -> queued -> running -> fresh | terminal_error | cancelled`

行为要求：

1. 同一 key 的并发 promotion 合并为一个 operation；watcher 数量不增加 Agent 工作量。
2. Relay 必须为内部 touch 建立 operation identity、deadline 和完成处理，不得依赖 Web route 才消费 Agent response。
3. `OVERLOADED`、`AGENT_CAPACITY`、`CACHE_CAPACITY`、`TIMEOUT`、`NATIVE_STATE_UNCERTAIN`、Agent disconnect 和 authentication replacement 都必须清除 running／queued 标记并广播 terminal status。
4. terminal error 保留最后合法 cache 为 stale／read-only；没有 cache 时保留不含正文的 stale reason。
5. watcher 仍存在时允许用户操作或有界退避触发新 promotion；不得形成固定周期 retry storm。
6. queue 按 thread key coalesce；当前有 watcher的 promotion 优先于无 watcher维护任务，active turn 优先级最高。
7. 无 watcher cache 在 Agent 重连后不主动 promotion；仅保留 stale cache，直到 watcher、active turn 或显式 hot-set policy 重新激活。

## 7. Web 跨 Epoch Durable Cache

1. Agent epoch 变化不得删除该设备已有的 IndexedDB 双 turn display cache。
2. Web 可以读取最新一份旧 epoch cache并立即展示，但必须标记 stale／read-only，并显示后台恢复状态。
3. 旧 epoch cache 中的 permission、runtime、pending interaction、queue、approval、answer 和 mutation receipt 必须在持久化前剥离；不得恢复为可操作状态。
4. 新 epoch fresh cache 到达后按 thread identity 原子替换旧 display cache，保留草稿与合理阅读锚点。
5. 设备解绑、认证主体变化、显式安全清理或 schema 不兼容时删除对应 durable cache。
6. durable cache 仍只保存最近两个 turn，并遵守既有 bytes／TTL 限制。

## 8. 错误处理与安全不变量

1. Relay transport 不确定时所有 mutation 控件禁用；草稿编辑和 stale cache 阅读继续可用。
2. transport 重连不得自动重放未确认 mutation。原请求若没有确定 receipt，Web 继续展示“结果不确定”，并要求读取当前状态。
3. fresh cache 只决定展示和操作入口；Desktop Agent 执行 mutation 前继续基于当前 controller 现场校验 owner、thread、turn、interaction 和 permission。
4. generation、epoch、stream identity 或 seq 不匹配的事件一律丢弃并记录不含正文的计数指标。
5. 所有 queue、mailbox、promotion map、watcher map 和 snapshot buffer 都必须有明确容量或按 bytes admission，不能依赖 OOM 回收。

## 9. 可观测性

至少提供下列不含正文的指标／结构化日志：

- transport connect／disconnect／takeover 次数和原因；
- heartbeat latency、control write timeout、data write timeout；
- outbound snapshot coalesced／dropped 数；
- promotion queued／running／fresh／terminal error 与排队时间；
- cold attach 次数、原因、frame bytes 与耗时；
- warm rewatch 未 cold attach 的次数；
- follower lease 数、expired 数；
- stale durable cache 命中与 fresh 接管耗时。

## 10. TDD 计划

实现必须严格按以下顺序新增会失败的测试，再写实现：

### 10.1 Desktop Agent

1. Relay transport 断开后 Service 和 follower 仍存活，重连不再次启动 App Server RPC。
2. follower 在离线期间多次更新时只保留最新 snapshot，重连后不回放中间状态。
3. 长时间 snapshot write 不阻止 heartbeat control path；snapshot timeout 后 stream 进入 stale且连接可继续或明确重连。
4. 同一 thread 多次 touch 被 coalesce；warm follower touch 不调用 unsubscribe／owner attach。
5. promotion queue 满时返回 terminal status，不留下永久 running 状态。

### 10.2 Relay

1. 同设备新 Agent transport 原子接管，旧 generation 的迟发消息被丢弃。
2. fresh lease 的 cache rewatch 不发强制 promotion；expired lease 只产生一次 promotion。
3. 内部 touch 的 success、overload、timeout、disconnect 均清除 `cachePromoting`。
4. Agent reconnect 只优先恢复有 watcher／active turn 的会话，不 cold attach 无 watcher cache。
5. `capturedAt` 过旧但 lease 有效仍可 fresh；lease 过期后即使 Agent 在线也为 stale。

### 10.3 Web

1. Agent epoch 变化后旧 IndexedDB 正文立即以 stale／read-only 显示，不被删除。
2. stale durable cache 不恢复 permission、pending interaction 或 mutation receipt。
3. fresh cache 到达后原位接管，不清空草稿、不重复 turn、不跳动阅读锚点。
4. transport 恢复期间发送、停止、审批和回答均禁用。

### 10.4 真实浏览器与故障注入

1. Playwright 固定手机 viewport：打开 fresh warm cache 立即显示，且没有 owner attach 请求。
2. 注入 15 秒 owner delay：当前 watcher 会话优先，无 watcher cache 不阻塞它。
3. 注入慢 snapshot writer：heartbeat 继续成功，Web 不因 transport 抖动回到空白页。
4. 注入 Agent reconnect：旧内容立即显示为 stale，fresh 接管后发送恢复。
5. 物理 iPhone／Safari 的后台切换和弱网恢复由用户最终复验，自动化结果不得冒充物理设备验证。

## 11. 验收标准

1. Relay 短暂断线／重连不重启 App Server RPC，不删除已存在 follower，不触发全部历史会话 owner attach。
2. 单个 50 MiB 兼容 owner frame 或等价慢写条件下，Agent 不因 Pong 写锁超时断线。
3. fresh warm cache 打开后正文立即显示；在 lease 有效时不发生 cold owner attach。
4. 任一 promotion 失败路径在 deadline 后均不残留 queued／running 标记，再次显式打开可重新尝试。
5. Agent 重连后当前 watcher 会话先恢复；无 watcher历史 cache 不进入串行 cold attach 队列。
6. Agent epoch 变化后旧双 turn cache立即可读但不可写；fresh cache 到达后安全接管。
7. 既有 Go test／race／vet、Web unit／build、Playwright 全通过，并补齐上述故障注入回归。
8. 真实日志中不再出现由 snapshot write lock 导致的 Pong timeout；同设备正常重连不形成持续 `duplicate agent` 循环。

## 12. 实现与验收记录

2026-10-26 已完成：

1. Desktop Agent 将 Codex Service／owner follower 提升为进程级生命周期；Relay transport 在同一 `agentEpoch` 下独立重连，不再因短暂网络故障重建 Codex follower。
2. Agent cache snapshot 使用每 thread latest-only mailbox 和独立 5 秒 write deadline；Relay→Web cache update 使用每 watcher latest-only mailbox，慢 Web 不再阻塞 Agent event read loop。
3. Relay 引入 follower lease，默认 freshness deadline 为 90 秒；fresh rewatch 不触发 cold attach，expired lease 才进入 coalesced promotion。
4. promotion 的拒绝、超时、断线和 transport replacement 均清理 operation identity，可安全重试；Web watcher 先于 Agent 上线的竞态也会在 Agent 连接后恢复。
5. 同设备认证 transport 支持原子接管；旧 transport 的迟到 cache frame 通过 peer identity fence 丢弃。
6. Web 跨 `agentEpoch` 保留最近双 turn durable display cache，并在离线／恢复期间剥离 permissions、pending interactions 和 queue，仅以 stale／read-only 方式展示。
7. 自动化结果：`go test ./...`、`go vet ./...`、关键包 `go test -race`、259 个 Web unit、Web production build、47 个 Playwright 真实浏览器用例全部通过。物理 iPhone／Safari 弱网与后台切换仍按 §10.4 由用户复验。
