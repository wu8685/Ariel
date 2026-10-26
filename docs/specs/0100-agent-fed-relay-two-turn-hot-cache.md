# 0100：Agent 持续跟随与 Relay 双 Turn Hot Cache

- 状态：Approved（2026-10-25；用户确认采用 Relay 仅内存缓存、Web Relay-local read／watch，以及无固定会话数上限的字节预算方案）
- 日期：2026-10-25
- 影响范围：Ariel Protocol、Desktop Agent、Relay、Web、Codex Desktop 兼容性探针与运行容量
- 关联：[0073 会话增量同步与无白屏缓存恢复](0073-incremental-thread-sync-and-stale-view-cache.md)、[0083 前后台与会话切换的非破坏性恢复](0083-nondestructive-foreground-resume.md)、[0084 Passkey 滑动会话与本地持久恢复](0084-passkey-sliding-session-and-durable-resume.md)、[0086 大会话渐进打开](0086-progressive-open-frame-hint-and-warm-controller.md)、[0099 慢会话订阅恢复](0099-resilient-slow-thread-subscription.md)
- 边界变更：本规格只在“最近两个 turn 的会话 hot cache”范围内取代旧规格中的“Relay 不缓存会话正文”；Codex 仍是唯一 SSOT，Relay 不得持久化正文、建立会话数据库或以 cache 授权 mutation。

## 1. 已确认的问题与目标

大会话的 Desktop follower 需要先接收并解析完整原始 owner state。本机现场中单个 owner frame 约 31 MiB，冷启动通常需要 8～13 秒，极端情况超过 20 秒。把 Web recent window 从四个 turn 减为两个，只会降低归一化后的传输、缓存和渲染成本，不能消除这段原始 owner 初始化时间。

本规格将订阅和缓存前移到 Desktop Agent 与 Relay 之间：

1. 每台 Desktop Agent 通过既有单条长期 WebSocket，独立于 Web 页面持续跟随动态 hot set；hot set 不设固定会话数上限，而由 watcher、活跃度和进程级字节预算决定。
2. Desktop Agent 把每个 hot 会话最新两个 turn 的已归一化状态持续推送给 Relay。
3. Relay 在进程内保存有界的双 turn hot cache；不写磁盘、数据库、volume 或内容日志。
4. Web 打开 hot 会话时只从 Relay 读取 cache 并 watch 后续 cache snapshot；不得因为 Web 打开页面而向 Desktop Agent 新建 read subscription 或 follower。
5. Relay cache 不直接授权 mutation。cache 为 fresh 且 Agent 在线时 Web 可以展示操作入口，但发送、停止、审批、回答和 queue mutation 必须逐次转发 Desktop Agent，并由 Agent 基于当前 hot controller 现场校验。

## 2. 连接与所有权模型

### 2.1 WebSocket 不变量

1. 每个 Web 页面到 Relay 仍只有一条 authenticated WebSocket，并在连接内多路复用设备和会话请求。
2. 每台 Desktop Agent 到 Relay 仍只有一条 Agent WebSocket，并在连接内多路复用 hot cache stream、cache promotion 和 mutation。
3. 禁止为每个 thread 创建独立网络 WebSocket。
4. Desktop Agent 拥有 hot follower 和双 turn materialization；Relay 只保存 Agent 已经生成的 bounded snapshot，不成为 Codex 状态机的第二个实现。

### 2.2 Agent-owned hot set

1. 每台设备维护动态 hot thread controller 集合。hot set 独立于单个 Web 页面的生命周期；同一设备上五个或更多 Web 客户端打开不同会话时，不得因“固定四槽”产生来回淘汰。
2. hot set 的优先级依次为：
   - 当前存在 Relay-local Web watcher 的会话；
   - 已知正在处理 turn 的会话；
   - watcher 归零后仍处于 90 秒 warm grace 的会话；
   - 最近被 Web 打开／touch 的无 watcher 会话；
   - `thread.list` 中最近更新的会话。
3. Agent 初次连接或重连时，先恢复最近一个会话，再按上述优先级串行恢复其余 hot 会话；禁止多个 large follower 同时冷启动。
4. 若 Codex 当前版本没有可靠的全局 thread activity event，Agent 可以使用单个有界的批量 `thread.list` 刷新发现新的活跃会话；不得为侧栏每个 thread 发独立 probe 或轮询。
5. hot set 不以会话数量触发淘汰。达到 Agent 字节／RSS soft budget 时，依次淘汰已过 warm grace 的无 watcher stale follower、无 watcher fresh follower；当前有 watcher或正在处理 turn 的 follower 最后保留。淘汰观察 follower 不得中断 Codex 原生 turn；Relay 保留该会话最后一次 cache 并标记 stale。
6. 同一 thread 的多个 Web 客户端只在 Relay 注册独立 watcher；它们共享一个 Agent controller、一个 follower 和一个 Agent→Relay cache stream，不向 Agent 建立 per-Web read subscription。
7. Relay 收到 Web 的 `thread.cache.watch`／`thread.cache.promote` 时，向 Agent 发送有界的 `thread.cache.touch`，用于提升 hot-set recency 或在 cache miss 时加入 hot set；touch 不是 mutation，也不能隐式创建 turn。
8. 最后一个 Web watcher 关闭后，Agent 至少保留 90 秒 warm grace；之后 follower 才可由字节预算与设备级 MRU／activity policy 淘汰。
9. watcher 是引用计数，不复制 controller、snapshot 或正文。每个 Web 连接最多同时 watch 32 个 thread，用于防止单连接滥用；这是安全门槛，不是设备 hot cache 的产品数量上限。
10. 若 soft budget 已满且没有可淘汰的无 watcher follower，新的 promotion 返回 `AGENT_CAPACITY`，已有 watcher 与原生 Codex turn 不受影响；不得通过无限分配等待操作系统 OOM。

## 3. 双 Turn 归一化与轮换

1. 自动 recent window 从四个 turn 调整为两个，统一覆盖：
   - Desktop owner 首个 snapshot；
   - `thread.read` 快路径；
   - Agent→Relay cache snapshot；
   - Web recent live view；
   - Web 页面内存和 IndexedDB 降级缓存。
2. 当第三个 turn 进入 recent window 时，Agent 按 owner 的 turn order 原子移除最老 turn，只保留最新两个；禁止按本机时间猜测顺序。
3. 当前 Web 页面正在显示的 completed turn 被 live recent window 轮换时，Web 必须先把它 demote 到当前页面的临时 `history.older`，再从 recent view 移除，避免用户眼前内容消失。
4. Relay cache 和 durable display cache 只保留最近两个 turn，不持久保存用户手动加载的旧历史页；当前页面已加载的旧页在本次阅读期间继续存在。
5. 若最新 turn 太大，继续沿用 bounded item page 与 `itemsComplete: false` 语义；不得截断 JSON、文本字节或伪造完整 turn。

## 4. Streaming Cache Snapshot

`Streaming Cache Snapshot` 是 Agent 在 Codex 回复仍处于 streaming 时，周期性生成的一份“小型、完整、可直接替换”的双 turn 当前视图。它不是 token 流，也不是完整会话 owner snapshot：

1. Agent 继续接收 Codex owner 的细粒度更新，在本机校验 identity／revision 并 materialize 最新两个 turn。
2. turn 开始、turn 完成、item 结构变化时立即发送 cache snapshot。
3. 纯 streaming 文本增长时合并高频更新，默认最多每 5 秒向 Relay 发送一次；每份 snapshot 包含截至该时刻的完整最新 item 文本。
4. Relay 按 `device registration + agentEpoch + threadId + cacheStreamId + seq` 校验后原子替换旧 cache；Relay 不应用 Codex item patch，也不拼接 token。
5. 若中间一个 cache snapshot 丢失，下一份完整 snapshot 仍能独立恢复当前双 turn 内容；seq gap 只影响 freshness 标记，不要求 Relay 重放 mutation 或推断正文。

该模型以最多约 5 秒的纯正文显示延迟换取更少的 snapshot 传输与 Web 重绘。具体间隔必须可配置，并在兼容性与带宽测试后调整。

### 4.1 刷新间隔调整（2026-10-26）

用户本轮明确指定将既有可调 streaming 默认间隔从 1 秒改为 5 秒，本次调整该参数并补齐交互变化的即时发布判断，不改变协议、状态归属、缓存窗口或安全边界：

- 未指定或非正的 `CachePublishInterval` 使用 5 秒；显式指定的正值继续生效，兼容测试与嵌入场景。
- 纯正文更新在固定窗口内仅保留最新 snapshot，不排队累积；持续输入不能不断延长窗口，也不能在停止更新后遗失尾部文本。
- 首帧、runtime／turn 状态、item／交互结构变化继续立即推送；turn 完成不等 5 秒。mutation receipt、模型设置推送、heartbeat 与重连计时不调整。
- TDD：先实际运行证明生产默认仍为 1 秒的失败测试，再调整默认值；回归同窗口 latest-only、尾部 flush、完成／交互立即发送，以及旧 stream／订阅 fencing。
- 使用真实 Agent／Relay 的隔离 fixture 补 Chromium streaming 行为回归，不向真实业务会话发送内容；物理 Safari 不在本机验收结论内。
- [本轮验收记录](../testing/2026-10-26-five-second-streaming-cache.md)：Go／vet／race、Web 286、Chromium 57 通过，5 秒专项连续 3 次通过；用户已确认以 v0.9.1 双端发布。

## 5. Agent→Relay cache 协议

### 5.1 事件

新增版本化 Agent 事件：

- `thread.cache.snapshot`：完整替换某 thread 的双 turn cache；
- `thread.cache.status`：标记 stream fresh／stale／error，不携带正文；
- `thread.cache.remove`：设备移除、明确归档／删除或身份失效时删除 cache；
- `thread.cache.touch`：Relay→Agent 的只读 hot-set recency 提升请求。

snapshot 至少包含：

- `deviceId`、稳定的 device registration identity；
- `agentEpoch`、`threadId`、`cacheStreamId`、`seq`；
- `capturedAt`、`runtimeHint`、只用于展示的 bounded permissions／pending interactions／queue metadata；
- 已归一化的 title／cwd 展示字段和最新两个 turn；
- `recentComplete`、item pagination metadata；
- cache payload byte size。

不得包含 Agent token、Passkey、原始 Desktop IPC frame、未脱敏绝对路径日志字段或 mutation receipt。

### 5.2 Relay-local Web 方法

新增只在 Web↔Relay 范围生效的方法：

- `thread.cache.read`：读取 Relay 当前 materialized cache，不转发 Agent；
- `thread.cache.watch`：注册当前 Web 对某 device／thread 的 cache watcher，并立即返回 fresh／stale／missing；
- `thread.cache.unwatch`：释放 watcher，不影响 Agent hot follower；
- `thread.cache.promote`：cache missing／stale 时请求 Agent 将 thread 提升进 hot set；
- `thread.cache.update`：Relay 向 watcher 广播原子 cache replacement 或 freshness 变化。

Relay 必须合并同一 device／thread 的 watcher；Web 客户端数量增加不能增加 Agent follower 或 Agent cache stream 数量。`read`／`watch` 命中 cache 时不得创建 Web→Agent route。

### 5.3 更新与背压

1. 每个 thread 同一时刻最多一个未完成 cache snapshot 写入；新状态覆盖尚未发送的旧状态，禁止形成无界队列。
2. cache snapshot 合并不得阻塞 mutation receipt；Web watcher update 从 Relay 当前 cache 产生，不建立 Agent read subscription。
3. WebSocket 背压时优先保留结构事件与最新 cache snapshot，丢弃已经被更新 snapshot 取代的中间 cache snapshot。
4. 单会话 cache 超限时发送 `thread.cache.status=oversized`，Relay 保留最后一个合法 snapshot 并标记 stale；不得发送半个 payload。

## 6. Relay 内存 Hot Cache

### 6.1 存储边界

1. cache 只存在 Relay 进程内存；不写文件、数据库、Docker volume、事件日志、访问日志或 crash dump 附加字段。
2. Codex 始终是 thread、turn、item、runtime 和 mutation 的唯一 SSOT。Relay cache 不是离线数据库、事件溯源或 owner receipt。
3. 容器重启后 cache 为空；Agent 重连后按当前 hot set 串行重新填充。
4. Agent 断线或 cache stream gap 后可以保留正文，但必须标记 stale；stale cache 只能只读展示。
5. Relay 对同一 device／thread 的所有 watcher 广播同一份 cache replacement；不得按 watcher 复制 materialized 正文。

### 6.2 字节预算与 admission

- 每个 hot thread：最近 2 turn，单 entry 最大 8 MiB；同一稳定 device registration／thread 只存一份正文，watcher 只保留轻量引用；
- Relay cache soft budget：512 MiB，按实际序列化 payload bytes 加固定 metadata overhead 计费；不得以 entry 数量替代字节计费；
- Relay Go runtime：`GOMEMLIMIT=768MiB`，给 WebSocket、request、索引和 GC 保留 headroom；
- Relay Docker／cgroup hard memory limit：1 GiB；这是事故隔离的最后防线，不是正常 eviction 或 admission 机制；
- stale TTL：24 小时；无 watcher fresh entry 在最后一个 watcher 离开后至少保留 90 秒 warm grace；
- 每个 Web 连接最多 32 个 watcher；单帧继续服从 Relay／WebSocket 协议上限，不能因 cache 绕开容量检查。

Relay 在写入新 snapshot 前执行 admission。预算不足时依次清理：过期 stale entry、无 watcher stale LRU、已过 warm grace 的无 watcher fresh LRU，再合并／覆盖同一 thread 尚未广播的旧 snapshot。若剩余 entry 全部仍有 watcher且预算仍不足，拒绝新的 promotion／snapshot，返回或广播 `CACHE_CAPACITY`，并保留最后一个合法 snapshot为 stale。不得主动分配到 OOM；OOM exit 只表示 soft budget、计费或运行环境出现缺陷。

512／768 MiB 是首版可配置默认值，必须由五客户端打开五个不同大会话的真实测试校准；调整预算不改变“双 turn、内存-only、无固定会话数上限、mutation fail closed”不变量。

### 6.3 Freshness

Relay cache 对 Web 返回：

- `fresh`：目标 Agent 在线、epoch 匹配、cache stream 连续且最近一次 snapshot 未超过 freshness deadline；
- `stale`：Agent 离线／重启、seq gap、过期或 follower 被淘汰；
- `missing`：不存在合法 cache。

响应必须包含 `capturedAt`、`agentOnline`、`agentEpoch` 和不含正文的 stale reason，供 Web 明确展示后台状态。

## 7. Web 打开与操作流程

### 7.1 Hot cache 命中

1. Web 选择 thread 后调用 Relay-local `thread.cache.watch`。Relay 立即返回当前 cache，并将该 Web 注册为 watcher；整个读取路径不访问 Desktop Agent。
2. Web 立即展示双 turn，不等待 Desktop Agent，不显示覆盖正文的全屏“正在同步会话”。
3. 后续 `thread.cache.update` 直接由 Relay cache 广播；Web 按 cache stream identity／seq 原子替换 recent view，不自行拼接 token。
4. cache follower 从无人查看的 warm 状态重新转为 watched 时，Desktop Agent 必须重新绑定当前 Codex owner 并发布新的完整 snapshot；不得仅修改本地 `watching` 标记并继续把可能已经静默停更的旧 snapshot 声明为 fresh。
5. 多个 Web 页面 watch 同一 thread 时只增加 Relay watcher，不增加 Agent follower、cache stream 或网络 WebSocket。

### 7.2 Cache miss／stale

1. `missing` 时 Web 保持 watcher，并发送一次 `thread.cache.promote`。Relay 向 Agent 发送 bounded touch；Agent 串行打开 follower 并将首个双 turn snapshot 推回 Relay，再由 Relay 广播给 watcher。
2. `stale` 时 Web 立即展示 stale cache 并保持只读，同时发送至多一次 promote；不得定时形成 retry storm。
3. 支持 0100 的 Agent／Relay 组合不得把 recent read 退回旧 `thread.read + thread.subscribe` 穿透链路。旧链路只作为滚动升级期间的 capability fallback，并必须明确标记 compatibility mode。
4. Relay 不可用时，Web IndexedDB 双 turn cache 作为第二级只读降级；本地 cache 不替代 Relay／Agent freshness 校验。

### 7.3 Mutation

1. Web 不因发送、停止、审批、回答或 queue 操作建立 read subscription；每个 mutation 继续通过 Relay 单独转发 Desktop Agent。
2. `fresh` cache 且 Agent 在线时 Web 可以启用相应操作入口，但这只是交互可用性提示，不是授权证据。
3. Desktop Agent 收到 mutation 后必须使用当前 hot controller 重新读取／校验 owner state，并执行既有 thread、turn、interaction、permission 和 queue fencing。hot follower 已失效、目标不匹配或 owner 不确定时返回 `RESYNC_REQUIRED`／`NATIVE_STATE_UNCERTAIN`，不得依据 Relay cache 执行。
4. `stale`／`missing` 时所有 mutation 控件禁用；Web 仍允许阅读 cache 和编辑草稿。
5. 加载两个 recent turn 之前的历史、图片、文件原文和 diff 继续按需通过 Relay 转发 Agent；这些显式读取不改变 hot follower 所有权。

## 8. Large Follower 兼容性与容量门槛

1. 当前 production large follower 固定单槽；不得把固定槽位简单扩大为另一个固定会话数。新实现以 controller／raw frame 实际 bytes 和 Agent RSS soft budget 做 admission。
2. 实现前必须用隔离 fixture 验证当前 Codex Desktop 同时存在至少五个 64 MiB follower，至少覆盖：
   - 五个约 30 MiB owner frame 并存；
   - 五个会话同时 streaming；
   - follower 初始化串行化；
   - 某 follower 淘汰不影响 Codex 原生 turn；
   - Agent Close／重连无 lease 泄漏。
3. Agent 初始 controller／raw-frame soft budget 为 512 MiB，RSS 观察线为 768 MiB；两者必须可配置，并与 Relay 的 1 GiB hard limit 分别计量。Relay 容器限额不能保护运行在用户设备上的 Desktop Agent。
4. 验证通过后 production 至少支持五个约 30 MiB hot follower；记录实际 frame bytes、Agent RSS、Codex Desktop RSS、CPU、初始化耗时和 cache bandwidth。更多 follower 只要通过字节 admission 即可存在，不再受产品数量上限限制。
5. 超过 Agent soft budget 时按 2.2 的优先级淘汰无 watcher follower；若全部 follower 均被 watcher／active turn 占用，则新 promotion 返回 `AGENT_CAPACITY`，不得杀进程或影响已有 follower。
6. 若当前 Codex 版本无法稳定支持五个 large follower，必须把能力标记为 `unverified/unsupported` 并向用户报告；不得用逐 thread 高频轮询冒充持续订阅。

## 9. 安全与失败处理

1. 本规格明确接受新增风险：公网 Relay 进程被读取时，攻击者可能获得仍在内存中的 hot／stale 双 turn 正文。通过内存-only、双 turn、容量、TTL、设备身份隔离和无正文日志限制暴露面。
2. cache key 必须包含服务端确认的稳定 device registration identity；不能只相信客户端可复用的 `deviceId`。
3. Agent credential 轮换、设备移除、认证主体变化和显式安全清理必须删除对应 cache。
4. cache 的 runtime hint、pending interaction、permissions 或 queue metadata 均不产生写权限；所有 mutation 继续要求 Desktop Agent 对当前 hot controller 执行现场校验和既有 owner fencing。
5. `CACHE_CAPACITY`／`AGENT_CAPACITY`：先淘汰符合条件的无 watcher LRU；若没有可淘汰对象则保留已有 cache／follower，拒绝新增 promotion。cache stream 不无限重试，进程不以 OOM 作为容量控制。
6. `RESYNC_REQUIRED`／native revision gap：Agent 停止声明 fresh，重新读取 owner 后发送新的 cache stream snapshot；Relay 不自行修复正文。
7. Agent 离线：Relay cache 标记 stale；Web 保留阅读能力，不自动重放 mutation。

## 10. TDD 与兼容性计划

### 10.1 Protocol／Relay

1. Red：cache snapshot 的 identity、seq、大小和 schema 校验；非法／越界 snapshot 不进入 cache。
2. Red：完整 snapshot 原子替换；中间 snapshot 丢失后下一份仍可恢复，不在 Relay 拼接 token。
3. Red：fresh／stale／missing、Agent disconnect、epoch change、TTL、单 entry 与全局按 bytes admission／LRU。
4. Red：cache 仅内存存在，Relay restart 后为空；日志不含正文。
5. Red：cache read／watch 命中时不创建 Agent route；多个 watcher 共享一份 materialized cache 和一个 Agent stream。
6. Red：cache miss 的 promote 至多产生一次 bounded touch，不创建逐 thread 轮询或 retry storm。
7. Red：五个 Web 客户端 watch 五个不同 thread 时五份 cache 同时存在且不抖动；同 thread 多 watcher 只存一份正文。
8. Red：512 MiB soft budget 的 eviction／`CACHE_CAPACITY` 可确定复现，1 GiB hard limit 不作为正常测试成功条件；慢 watcher 只保留最新待发 snapshot。

### 10.2 Desktop Agent

1. Red：Agent 无 Web 连接时仍持续生成 hot cache snapshot。
2. Red：snapshot 始终只有最新两个 turn；第三 turn 原子轮换。
3. Red：streaming 高频更新最多按配置间隔发送，turn start／complete 立即发送；无界队列不成立。
4. Red：watcher 驱动动态 hot set、90 秒 warm grace、按 bytes 淘汰、active follower 优先和同 thread follower 复用。
5. Red：五 large follower compatibility fixture 与 Go race；失败必须 fail closed 并报告 unsupported。
6. Red：全部 follower 被 watcher 占用且预算不足时返回 `AGENT_CAPACITY`，已有 follower 和 Codex turn 不受影响。

### 10.3 Web

1. Red：hot cache read／watch 完全 Relay-local，不产生 `thread.subscribe` 或其他 Agent read request。
2. Red：Relay cache update 原位替换 recent view，不重复 turn／item，不丢草稿和阅读位置。
3. Red：fresh cache 可以显示操作入口，但 mutation 仍逐次转发 Agent；stale cache 与 Agent 离线时不能发送。
4. Red：current live window 轮换前把 completed turn demote 到临时 history，正文不从当前 DOM 消失。
5. Red：Relay cache missing 时只发送一次 promote 并等待 watcher update；仅 compatibility mode 可回退旧 read subscription，Relay unavailable 时只读恢复 IndexedDB。

### 10.4 真实环境

- 真实 Codex Desktop 隔离会话验证五 follower、双 turn snapshot、streaming、淘汰、重连和资源占用。
- Playwright 移动 viewport验证 cache 秒开、后台同步 loading、fresh 接管、stale 提示和发送门禁。
- 物理 iPhone／Safari 由用户最终验收；Chromium 不能冒充物理 Safari。

## 11. 验收标准

1. Desktop Agent 与 Relay 持续维护 watcher／active／recent hot set 的最新双 turn cache；没有固定会话数上限，五个客户端打开五个不同会话时不得发生固定四槽抖动。
2. Web 打开 cache 命中的会话时立即出现正文；read／watch 全程停留在 Relay，不创建 Web-owned Agent subscription。
3. hot 会话纯 streaming 正文在默认最多约 5 秒后出现在 Relay cache；turn 完成立即更新。
4. 第三个 turn 到来后 Relay cache 只保留最新两个；当前 Web 已显示的 completed turn 不消失。
5. Web 与 Relay、Agent 与 Relay 各维持单条多路复用 WebSocket；Web watcher 数量和 hot thread 数量都不创建额外网络连接或 per-Web Agent follower。
6. Relay restart 清空 cache；任何路径都不把正文写入磁盘、数据库、volume 或日志。
7. 五个 large follower 通过当前 Codex Desktop 兼容性验证且资源在确认预算内；Relay 使用 512 MiB cache soft budget、768 MiB `GOMEMLIMIT` 和 1 GiB 容器 hard limit，正常容量压力通过 eviction／capacity error 处理而非 OOM。
8. Go 全量、race、Web 全量、production build、真实移动 Chromium 通过，物理 iPhone／Safari 保留用户验收。
