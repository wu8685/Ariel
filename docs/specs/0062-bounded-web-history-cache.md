# 0062：Web 端有界会话历史缓存

- 状态：Implemented（2026-10-10；Web 单测、production build、真实 Chromium 与 Go 全包回归通过）。
- 来源：用户希望减少在多个会话之间往返时重复下载已经浏览过的历史内容，并确认优先采用 Web 端缓存，不在 Relay 保存会话正文。
- 范围：Ariel Web 当前页面内存中的已下载旧 turn／旧 item 页；不改变 Relay、Desktop Agent、WebSocket 协议、Codex SSOT、live snapshot、写操作门禁或历史分页上限。
- 关联：[0013 大会话最近十回合与 64 MiB 有界同步](0013-paged-history-and-large-owner.md)、[0017 实时更新时保留历史阅读位置](0017-preserve-reading-position-on-live-updates.md)。获批后，本规格仅覆盖 0013 中“切换会话立即清除全部旧页”的 Web 内存行为；断线、重连、stream 失效时仍清除。

## 目标与非目标

1. 用户在同一页面、同一 Relay 连接和同一 Desktop Agent epoch 内，从会话 A 切换到 B 后再回到 A，A 已经下载的旧历史页可以直接复用，不再次请求相同的 `thread.history`／`thread.history.items` 页面。
2. 缓存只优化重复访问；首次打开会话、建立 owner 订阅和首次下载某一页的耗时不作虚假承诺。
3. Relay 仍不保存正文；缓存不进入 `sessionStorage`、`localStorage`、IndexedDB、Service Worker、磁盘日志或服务器。
4. 不用缓存替代 live snapshot，不离线展示为当前状态，不缓存 pending interaction、permissions、runtime、queued message 或未完成 turn 的权威状态。

## 输入、输出与行为

1. Web 维护页面级 history cache。缓存键至少包含 `deviceId + agentEpoch + threadId`；缺失 `agentEpoch` 时不缓存。Relay 连接断开或重新进入 ready、目标 Agent 离线／重连、agentEpoch 改变、stream `RESYNC_REQUIRED`／`NATIVE_STATE_UNCERTAIN` 时，清除相关缓存。
2. 缓存值只包含已经由当前连接成功取得的：
   - `HistoryState` 中的旧 turn、分页位置和 gap／exhausted 标记；
   - 属于这些旧 turn、且 turn 已完成的 `itemOverrides`；
   - 最近访问时间和按 UTF-8 JSON 序列化结果计算的字节数。
3. 切离当前会话前，将符合条件的旧页写入缓存；选择目标会话时仍必须重新执行 `thread.subscribe`。只有新的 owner snapshot 身份、device、thread 和 subscription 全部验证成功后，才把对应缓存合并到页面；snapshot 中的最近窗口始终覆盖缓存，按 turnId／itemId 去重。
4. 缓存恢复不得带回旧 runtime、交互卡、权限、队列、streamId、seq 或当前 turn 状态。写操作继续只依据新 snapshot；缓存命中不缩短、跳过或伪造订阅成功。
5. 缓存恢复后继续“加载更早消息”时，可以复用同一连接、同一 agentEpoch 下保存的 opaque cursor。若缓存 cursor 返回无效、结构异常或身份不匹配，丢弃该 entry，并从最新历史页重新建立分页锚点；只读重建最多自动执行一次，不循环重试，也不影响已确认的 live view。
6. 巨型 completed turn 的已下载 item 页与该旧 turn 一起缓存；未完成 turn、live 最近窗口中的 override、加载中的请求和失败响应不进入缓存。
7. 缓存命中后恢复用户此前已经下载的旧页，但不恢复精确滚动像素；切换回会话仍按现有行为定位最新消息。用户再次进入旧历史时无需重新下载缓存页。

## 容量与淘汰

1. 全局缓存上限为 32 MiB，最多 8 个 thread entry；现有单页面最多 50 个旧 turn 的限制继续生效。
2. entry 按最近访问时间执行 LRU 淘汰；更新 entry 时重新计算完整字节数。单个 entry 超过全局上限时不缓存，不能为了命中而截断 turn 或 item。
3. entry 自最后一次访问起保留 5 分钟；过期时惰性删除。缓存元数据和正文均计入有界内存设计，不能因多次切换保存重复副本。
4. 当前页面卸载后缓存自然消失；不注册后台定时任务，不承诺浏览器回收后的存活时间。

## 错误与安全边界

- 缓存损坏、重复 ID、非 completed 旧 turn、无法计算大小或超过容量时 fail closed：丢弃该 entry，回到现有网络分页流程。
- 新 snapshot 与缓存 turnId 重叠时以 snapshot 为准；缓存只能补充更早内容。itemId 重叠时使用新 snapshot／新分页返回值，不显示重复内容。
- `thread.error`、Agent 离线或 Relay 重连后不得继续展示缓存为已确认历史；现有只读降级必须重新从 Agent 取得，不使用旧缓存掩盖错误。
- 缓存不改变 7 MiB history page、8 MiB WebSocket frame、32 MiB Web 旧历史窗口或 Markdown／图片安全边界。
- 页面内存包含原本已经展示过的会话正文；本规格不扩大到其他设备、其他用户或服务器端共享。

## TDD 与验收

1. Red：先为独立 cache 模块测试 UTF-8 字节计数、同 key 更新、5 分钟过期、8 entry／32 MiB LRU 淘汰、超大 entry 拒绝和 epoch 隔离。
2. Red：App 测试先复现 A 加载旧页 → 切 B → 回 A；修复前再次发出相同 `thread.history` 请求，修复后新 snapshot 验证成功即恢复旧页，且不会为已缓存内容重复请求。
3. 测试缓存永远不恢复 runtime、pending interaction、permissions、queued message、streamId 或 seq；新 snapshot 与缓存重叠时无重复 turn／item。
4. 测试 Relay 断开／重新 ready、Agent epoch 改变、离线、`RESYNC_REQUIRED`、`NATIVE_STATE_UNCERTAIN`、过期 entry 和无效 cursor 都清除缓存并走现有读取流程。
5. 真实 Chromium 隔离 WebSocket fixture：在会话 A 加载旧页，切到 B 再回 A，断言旧内容立即恢复、网络请求计数不增加、最新 snapshot 仍控制运行状态和操作按钮；刷新页面后必须重新下载。
6. Web 全量单测、真实浏览器回归和 production build 通过；物理手机仅需复验内存占用与切换观感，不把 Chromium 冒充 Safari。

## 实施结果

- `web/src/history-cache.ts` 实现页面内存中的 32 MiB／8 thread／5 分钟 TTL LRU cache，键包含 `deviceId + agentEpoch + threadId`，并对 completed turn、重复 ID、UTF-8 大小和超限 entry 执行 fail closed。
- Web 每次进入会话仍重新订阅；只有 fresh owner snapshot 到达后才合并旧页，live turn 始终覆盖缓存。断线、重新 ready、Agent 离线、epoch 变化、stream error 和显式 resync 均清除相应缓存。
- 缓存 cursor 被 Agent 拒绝、返回畸形页或无法前进时，只丢弃 restored entry，并从最新页重建一次分页锚点。
- 验收证据见 [`docs/testing/2026-10-10-0062-bounded-web-history-cache.md`](../testing/2026-10-10-0062-bounded-web-history-cache.md)。
