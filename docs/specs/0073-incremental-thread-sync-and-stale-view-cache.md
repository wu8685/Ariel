# 0073：会话增量同步与无白屏缓存恢复

- 状态：Implemented（2026-10-11；Go／Web／race／production build／真实移动 Chromium 已验）。
- 来源：用户观察到会话运行期间偶发整页进入“正在同步会话”，且在已浏览会话之间切换时重复等待；希望已确认历史保留在 Web 本地，后续只同步新增或变化内容。
- 范围：Desktop Agent 的订阅 diff、Ariel WebSocket 协议的增量事件、Web 页面内存中的最近会话缓存，以及切换／resync 时的 stale-while-revalidate 展示；Relay 仍只转发，不保存会话正文。
- 关联：[0062 Web 端有界会话历史缓存](0062-bounded-web-history-cache.md)、[0063 Agent 重连状态 fencing 与会话恢复](0063-agent-reconnect-state-fencing.md)、[0072 新会话首屏最近四回合](0072-four-turn-initial-history-window.md)。本规格获批后扩展 0062 的缓存范围，但不放宽 0063 的 owner fencing，也不改变 0072 的 4 turn 首屏窗口。

## 已确认的现状与根因

1. 正常 live 更新不会重传整段会话历史。Desktop Agent 已把 live window 限制为最近 4 个 turn，更早内容只通过历史分页读取。
2. 但每次 native live state 变化时，Desktop Agent 仍发送一份完整的最近 4 turn `thread`；Web 再用这份对象整体替换当前 `ThreadView`。因此它不是“全历史全量”，但也不是真正的增量同步。
3. Web 的消息节点已经以稳定的 `threadId + turnId + itemId` 作为 React key。普通、连续的 `thread.update` 不会主动进入全屏 loading；主要问题来自以下路径主动清空 `view`：
   - 选择任何会话时，无论是否刚看过，都先 `setView(null)`，再等待新的 `thread.snapshot`；
   - stream 序列断档、`RESYNC_REQUIRED` 或显式重新同步时，先清空当前 view，再重新订阅；
   - 部分 Agent／Relay 恢复路径也会废弃当前 view。
4. 0062 只缓存已经分页下载的旧 turn／item，不缓存最近 4 turn 的可见内容。因此切回已浏览会话时，fresh snapshot 到达前没有可展示的数据，必然出现整页“正在同步会话”。
5. 当前缓存只存在于当前浏览器页面内存，不进入 `localStorage`、IndexedDB 或 Relay。本文继续采用这一安全边界；浏览器刷新后的磁盘级正文缓存不属于本次范围。

## 目标与非目标

### 目标

1. 同一会话连续更新时，初始 snapshot 之后只发送发生变化的 turn／item 和轻量 thread state，不重复发送未变化的最近 turn。
2. Web 按 ID 原位合并增量，保留未变化 turn／item 的对象身份与 DOM 节点；新增消息只增加对应节点，streaming 文本只替换对应 item。
3. 同一页面、同一设备和同一 Agent epoch 内，切回已浏览会话时立即展示最近一次确认内容，并在后台取得 fresh snapshot，不再显示整页 loading。
4. 同一会话发生 seq gap／`RESYNC_REQUIRED` 时保留最后确认内容和用户草稿，以只读 stale 状态后台重订阅；fresh snapshot 到达后原子恢复。
5. 所有缓存和 per-subscription diff baseline 都必须有界；不因会话数量增长而逐会话请求、轮询或常驻订阅。

### 非目标

1. 不在 Relay 建立正文缓存、事件日志、离线队列或第二套会话数据库；Codex 继续是 SSOT。
2. 不缓存所有侧栏会话，不为未选择会话逐一调用读取接口；只有用户实际打开过的最多 8 个会话进入 Web LRU cache。
3. 不做字符级 text diff。发生变化的 item 以完整 item 替换，避免字符 patch 丢失或乱序；未变化 item 不传输。
4. 不承诺浏览器刷新、页面关闭、Web 进程被系统回收或 Agent epoch 改变后仍保留正文。
5. 不以缓存状态执行发送、停止、审批、回答或队列 mutation。

## 协议与 Desktop Agent 行为

1. `thread.subscribe.params` 增加可选 `updateMode: "item-delta-v1"`。未提供该字段时继续使用现有 full update，保证旧 Web／旧 Agent 在滚动升级期间可互通。
2. 初始 `thread.snapshot` 仍包含完整、权威的最近 4 turn window，`seq = 1`。新 Web 同时兼容现有 full `thread.update` 和新的 delta form。
3. 请求 `item-delta-v1` 后，后续 `thread.update` 可以携带 `patch` 而不是完整 `thread`。`patch` 包含：
   - `threadState`：不含 `turns` 的当前轻量状态，包括 title、cwd、updatedAt、runtime、history/recent complete、permissions、pending interactions 和 bounded queued messages；
   - `turnOrder`：当前最近窗口的完整 turnId 顺序，最多 4 个；
   - `turnPatches`：只列出新增或发生变化的 turn。每项包含完整 turn metadata、完整 itemId 顺序、变化 item 的完整 `upsertItems`，以及已经删除的 `removedItemIds`；
   - `removedTurnIds`：离开最近窗口或被 owner 删除的 turnId。
4. Desktop Agent 为每个 subscription 单独保存上一份已经发出的 normalized live baseline。diff 必须由该 subscription 自己的 baseline 计算，不能跨订阅共享 seq 或假定不同 Web 已收到同一事件。
5. native update 若归一化后与 baseline 无差异，不发送空 update，也不递增 seq。存在变化时，仍严格满足 `baseSeq == previous seq`、`seq == baseSeq + 1`。
6. 新 turn 必须在一个 patch 中携带完整 turn metadata、item order 和全部当前 item；既有 turn 只携带变化 item。itemId／turnId 重复、顺序引用未知 ID、patch 超出既有 frame／thread 容量或无法确定可靠 diff 时 fail closed，以 `RESYNC_REQUIRED` 或现有容量错误结束该 stream。
7. snapshot 发送和 subscription 激活之间若发生变化，activation update 也必须相对刚发送的 snapshot 计算 delta，不得漏事件或回退到不匹配 baseline。
8. Relay 不解释、不合并、不缓存 patch，只维持现有 subscription identity、stream、seq 与 owner route 校验。

## Web 增量合并

1. Web reducer 只在 deviceId、threadId、subscriptionId、streamId 与 seq 连续性全部匹配时应用 patch。
2. `threadState` 替换轻量权威状态；`turnOrder` 决定最近窗口顺序；`turnPatches` 按 turnId／itemId upsert 或删除。未出现在 patch 中且仍在 order 内的 turn／item必须复用原对象。
3. patch 中出现重复 ID、未知引用、缺失新 turn／新 item、顺序不完整或非法删除时，不部分应用；保留最后确认 view，进入后台 resync。
4. fresh snapshot 永远可以替换 recent live window；0062 缓存的 completed older history 只作为更早内容合并，不能覆盖 snapshot 中同 ID 内容。
5. full update 兼容路径继续有效。收到现有 `thread` form 时按当前规则替换最近窗口，使更新后的 Web 可以配合尚未升级的 Agent。

## 已浏览会话缓存与切换行为

1. 将 0062 的 history cache 扩展为页面级 conversation cache，键仍为 `deviceId + agentEpoch + threadId`。每个 entry 可以保存：
   - 最近一次权威 snapshot／连续 update 后的 recent thread view；
   - 已下载的 completed older history 与 completed item overrides；
   - 分页 cursor、最近访问时间、估算字节数；
   - 必要的展示锚点，但不保存 subscriptionId、streamId、seq 作为新订阅依据。
2. 从 A 切到已缓存的 B 时：
   - 立即显示 B 的缓存内容和轻量“正在同步最新状态”提示；
   - 输入框保持可见、可编辑，B 的 draft 可以继续输入；
   - 同时在后台执行新的 `thread.subscribe`；
   - fresh snapshot 到达前，发送、停止、审批、回答和队列修改全部禁用。
3. 从 A 切到从未打开、没有缓存的 B 时，才显示现有首次加载占位；A 的内容不能冒充 B。
4. fresh snapshot 验证通过后，原子替换 B 的 recent live window、runtime、interactions、permissions 和 queue，再合并可信的 completed older history，并解除 mutation 门禁。不得先清空缓存内容制造中间白屏。
5. 切离当前会话前保存其最后确认 view。只缓存真正通过 owner snapshot／连续 update 确认过的状态；正在应用、畸形或 identity 不匹配的事件不进入缓存。
6. cache 继续使用 32 MiB、最多 8 个 thread、5 分钟 TTL 的全局 LRU 上限；recent view 与 older history 共同计入大小。单 entry 超限时整体不缓存，不静默截断正文。
7. 页面卸载、Relay session 失效、设备移除或安全身份变化时清空相关 entry。Agent epoch 改变后旧 entry 不作为新 epoch 的已确认缓存复用。

## 无白屏 resync 与交互门禁

1. 同一会话 seq gap、`RESYNC_REQUIRED`、显式刷新或 0063 的短暂恢复期间，不再立即 `setView(null)`；保留最后确认内容并标记 `stale/syncing`。
2. stale 状态下：
   - 会话正文、已加载历史、滚动位置和 composer draft 继续显示；
   - composer 允许编辑草稿，但发送按钮禁用；停止、审批、回答、归档和队列 mutation 同样禁用；
   - 页面使用非阻塞提示表示正在同步，不覆盖正文，不反复追加相同 notice。
3. fresh snapshot 到达后原子切换回 authoritative 状态。旧 subscription 的迟到事件必须继续被 fencing 丢弃，不能让页面回退或再次触发 resync。
4. 若目标会话从未成功显示、缓存不存在，或安全身份已经失效，允许显示全屏 loading／明确错误；减少 loading 不能以跨设备、跨 epoch 展示错误正文为代价。
5. resync 不再仅因 stream 断档删除 completed older-history cache；同一 Agent epoch 下保留它。若 fresh cursor 后续证明失效，沿用 0062 的一次性只读重建。

## 性能与资源边界

1. Desktop Agent 每个活跃 subscription 只保留最近 4 turn 的上一份 normalized baseline；subscription 结束立即释放。仍遵守现有每 thread subscription 数上限和 controller 淘汰规则。
2. diff 计算时间与最近 4 turn window 大小线性相关，不扫描完整历史，也不调用每个会话的独立读取接口。
3. Web 只订阅当前选中会话；切离后 unsubscribe。缓存命中不会产生后台常驻订阅、轮询或一次性 fan-out。
4. streaming assistant 文本变化时只发送该 item 的完整最新值；相比当前每次发送最近 4 turn，网络与 JSON parse 成本随“变化 item”而非“全部 recent window”增长。
5. 若单个变化 item 本身超过安全上限，沿用现有 `HISTORY_TOO_LARGE`／frame 限制，不能用无界分片绕开容量门禁。

## 错误与安全边界

- 缓存内容只用于 stale 展示，不是 owner receipt；任何写操作都必须等待当前 subscription 的 fresh snapshot。
- patch 应用是原子的。任何一处验证失败都不得留下半更新 UI，也不得自动重放用户 mutation。
- 用户草稿继续按会话隔离并保留；缓存恢复或 snapshot 替换不能清空未发送草稿。
- pending interaction、permissions、runtime 与 queue 可以作为 stale 内容显示，但 fresh snapshot 前不能据此执行动作。
- 浏览器磁盘持久化会把会话正文长期写入设备，并引入退出清理、版本迁移和共享设备泄露风险；若未来需要“刷新页面后仍秒开”，必须另写 spec 明确加密、TTL、清除入口和账户边界。

## TDD 与验收

1. Red：协议契约先验证 optional `updateMode`、full/delta update union、非法混合 form、重复 ID 与容量边界；确认失败后再修改 schema／生成类型。
2. Red：Go 单测覆盖新增 turn、单 item streaming 替换、item／turn 删除、顺序变化、仅 runtime／interaction／queue 变化、无变化不发事件，以及两个 subscription 各自独立 baseline；随后实现最小 diff。
3. Red：activation race 测试在 snapshot 与 activate 之间改变 live state，断言首个 delta 的 `baseSeq = 1`、内容完整且应用后等于最新 normalized state。
4. Red：Web reducer 测试 patch 原子合并、稳定对象身份、full update 兼容、seq gap、未知引用、重复 ID 和 removed item；畸形 patch 必须保留旧 view 并请求 resync。
5. Red：App 测试 A → B → A。修复前回到 A 显示全屏 loading；修复后立即显示 A 缓存正文和 syncing 提示，输入框可编辑但 mutation 禁用，fresh snapshot 后无白屏恢复。
6. Red：App 测试运行中的同会话 seq gap／`RESYNC_REQUIRED`。正文、滚动位置和 draft 始终存在；旧 stream 迟到事件被忽略；fresh snapshot 后发送能力恢复。
7. Red：缓存测试覆盖 32 MiB／8 thread／5 分钟 LRU、recent + older 共同计费、epoch 隔离、Relay session 失效清理和单 entry 超限拒绝；断言不会为所有侧栏会话发请求。
8. 真实 Chromium 隔离 WebSocket fixture：
   - 连续发送 item delta，断言既有消息 DOM 节点不被替换、composer focus 与 draft 不丢失；
   - 手机 viewport 下 A → B → A，断言缓存正文立即可见、无中央 loading、fresh snapshot 后 mutation 恢复；
   - 注入 seq gap，断言 stale 内容可读且 mutation 门禁生效。
9. 完成 Go 全包／race、Web 全量单测、production build 与真实 Chromium 回归。物理 iPhone/Safari 的网络切换和页面观感保留给用户复验，不能用 Chromium 冒充。

## 发布顺序与兼容性

1. 先发布同时接受 full/delta update 的 Relay + Web，并让 `updateMode` 保持 optional。
2. 再更新 Desktop Agent；旧 Agent 忽略或不支持 delta 时继续发送 full update，新 Web 仍可工作。
3. Desktop Agent 确认收到 `item-delta-v1` 才发送 patch；旧 Web 未请求时始终收到现有 full form。
4. 任一阶段出现不兼容，回退到 full update，不回退 seq fencing、mutation 门禁或容量限制。
