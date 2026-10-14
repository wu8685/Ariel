# 0083：前后台与会话切换的非破坏性恢复

- 状态：Implemented（2026-10-15；Web 202／真实移动 Chromium 29／production build／公网 Docker 已验）。
- 来源：用户发现切换到其他应用再返回浏览器，或在当前页面切到另一会话再切回，都会看到当前会话重新同步；希望存量内容保持不动，只补充离开期间的增量。
- 关联：[0006 M4 恢复与容量](0006-m4-recovery-capacity.md)、[0073 会话增量同步与无白屏缓存恢复](0073-incremental-thread-sync-and-stale-view-cache.md)、[0077 Web 本地会话未读标记](0077-web-local-thread-unread.md)。
- 范围：WebSocket 前台唤醒判定、当前／上一会话的有界 warm subscription 与 Web 恢复交互；不改变 Relay／Desktop Agent 协议，不把正文写入 Relay 或浏览器持久化存储。

## 已确认的根因

1. 页面进入后台时记录时间；后台超过 5 秒后回到前台，Web 无条件调用 `client.wake()`。
2. `wake()` 即使面对 `ready + OPEN` 的健康 WebSocket，也会主动关闭旧连接、将所有未完成请求标为结果未知，并立即创建新连接。
3. App 对非 `ready` 状态会清空当前 subscription、`view`、历史状态和页面级缓存；新连接完成 hello 后再次读取设备并订阅当前会话。
4. 因而普通应用切换必然被制造成一次断线恢复，出现“正在同步”；它并不代表 Codex 会话或网络真的失效。
5. 该无条件 wake 是 0077 增加前台 unread 刷新时引入的副作用。unread 只需要复用批量 `thread.list`，不要求重建 WebSocket。
6. 页面内切换会话时，Web 会缓存 A 的最近内容，但同时立即 `unsubscribe(A)`；切回 A 时必须重新 `subscribe(A)` 并接收最近 4 个 turn 的 fresh snapshot，因此仍显示“正在同步”。
7. 当前协议没有已取消 subscription 的事件回放 cursor。A 被取消订阅后，Web 无法仅凭缓存推断离开期间的增量；要让紧邻的 A → B → A 真正连续，必须有界保留 A 的 live subscription。

## 用户行为

1. 浏览器在后台停留超过 5 秒后返回，如果当前 WebSocket 仍为 authenticated、`ready` 且底层状态为 `OPEN`：
   - 继续使用原连接和当前 subscription；
   - 当前会话正文、滚动位置、草稿和输入焦点保持不变；
   - 不显示“正在同步最新状态”，不新增 `thread.subscribe`；
   - 不把正在等待回执的 mutation 标成 `OUTCOME_UNKNOWN`。
2. 前台恢复仍可按 0077 触发一次有界的批量 unread 刷新；侧栏关闭时不为此额外读取会话列表。
3. 如果 WebSocket 已经断开、正在退避或尚未完成 hello，前台恢复可以取消退避并立即重连。
4. 真正重连、Agent epoch 变化、事件 seq gap 或 `RESYNC_REQUIRED` 仍按 0006／0073 重新订阅；不得为了避免同步而继续在陈旧 owner 状态上执行 mutation。
5. 页面被 Safari 回收并重新加载不属于普通前后台切换，仍按新页面启动恢复。
6. 当前页面从 A 切到 B 时，A 作为“上一会话”保留 warm subscription；从 B 切回 A 时：
   - 直接复用 A 的连续 subscription 和已在后台合并的最新 view；
   - 不发送新的 `thread.subscribe`，不显示同步提示；
   - 只渲染离开期间收到的新增或变化 item，不替换未变化消息 DOM；
   - A 恢复为当前会话，B 成为新的上一会话。
7. 每个 Web 连接最多保留两个 live subscription：当前会话和紧邻的上一会话。切到第三个会话 C 时，释放更早的 A，只保留 B + C；会话总数增加不会扩大后台订阅数。
8. 切回的目标已经超出 warm 范围、subscription 出错或发生 seq gap 时，立即展示 0073 的页面内存缓存，并在后台重新订阅最近 4 个 turn；fresh snapshot 按 turn／item ID 合并，不清空存量正文或替换未变化 DOM。取得 fresh owner 状态前，mutation 继续受门禁保护。

## 实现约束

0. 浏览器到 Relay 始终只有一条 WebSocket；当前与上一会话的逻辑 subscription 通过 `deviceId + threadId + subscriptionId + streamId + seq` 在该连接上多路复用，不为会话创建独立 WebSocket。Desktop Agent 到 Relay 同样保持单连接。
1. `ArielSocket.wake()` 在连接已经 `ready` 且 `WebSocket.readyState === OPEN` 时必须是幂等 no-op。
2. `wake()` 仅在连接不可用时清理 retry／hello timer、结束旧 socket 和 pending 请求并调用 `open()`；保持现有不自动重放 mutation 的安全语义。
3. App 的 visibility handler 可以继续调用 `wake()`，但不得自行清空当前 view；健康路径只执行已允许的 unread 批量刷新和已读水位更新。
4. 不新增每会话 probe、轮询或 fan-out；连接健康判定为本地常数时间检查。
5. 半开连接由既有 Relay heartbeat、WebSocket close 和事件 seq fencing 发现。检测到真实故障后允许恢复，不把“页面曾 hidden”本身当作故障证据。
6. Web 增加有界 subscription 状态表，按 `deviceId + agentEpoch + threadId` 保存当前与上一会话的 `subscriptionId`、`streamId`、连续 seq 和最新确认 view；不得保存到 `localStorage`／IndexedDB。
7. 背景 warm subscription 的合法 update 必须经过与当前会话相同的 identity、stream 和 seq 校验，再原子合并进页面内存缓存并更新 unread；不得触发当前会话 DOM、滚动位置或 composer 状态变化。
8. 所有 mutation 始终只允许作用于当前选择且 owner 状态连续的 subscription。上一会话虽然保持 live，也不得接收发送、停止、审批、回答或队列操作。
9. 新选择使 live subscription 超过两个时，先对最旧的非当前 subscription 发送一次 `thread.unsubscribe` 并从状态表删除；迟到事件由 identity fencing 丢弃。

## TDD 与验收

1. Red：Client 测试在 `ready + OPEN` 状态调用 `wake()`，旧实现会创建第二个 socket 并令未完成 mutation 变成 `unknown`；新行为应保持一个 socket 和原 pending 请求。
2. Red：Client 测试在 socket 已关闭／重连退避时调用 `wake()`，应立即创建新连接，不等待原退避 timer。
3. Red：App 测试模拟页面 hidden 超过 5 秒再 visible，断言 socket 数量、`thread.subscribe` 次数、正文、草稿和同步提示均不变化。
4. App 测试确认真实 socket close 后仍会重连并重新取得 owner snapshot，旧 subscription 的迟到事件继续被丢弃。
5. Red：App 测试覆盖 A → B → A，断言只创建 A、B 两个 subscription；切回 A 不产生第三个 subscribe、不显示同步提示，并保留正文 DOM、草稿和滚动位置。
6. Red：A 在后台收到连续 delta 后切回，断言只新增／修改对应 item；当前 B 的 DOM 和 composer 在背景 update 到达时不变化。
7. Red：A → B → C 时断言 A 被 unsubscribe、live subscription 始终不超过两个；再切回 A 时走缓存 + fresh snapshot 回退，mutation 在 fresh owner 到达前禁用。
8. Red：背景 subscription 的错误、seq gap、迟到 update、Agent epoch 变化和连接重建均被 fencing，不把 stale view 当作可写 owner。
9. 运行 Web 全量测试、production build 和全部 Playwright 回归。
10. 真实移动 Chromium fixture 分别模拟前后台切换和 A → B → A，断言当前消息 DOM、composer draft、连接与 warm subscription 保持稳定；物理 iPhone／Safari 由用户部署后复验。

## 回滚

- 移除 warm subscription 状态表并恢复只订阅当前会话，即可退回 0073 的缓存 + fresh snapshot 行为；恢复 `wake()` 对健康 socket 的强制重建即可退回旧前台行为。不涉及协议、Relay、Desktop Agent 或持久化数据迁移。
