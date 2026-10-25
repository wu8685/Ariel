# 0099：慢会话订阅的确定性超时与非空白恢复

- 状态：Implemented（2026-10-25；Go 全量、Web 全量 241、race、build、移动 Chromium 目标回归 4 项通过；物理 iPhone／Safari 待用户复验）
- 日期：2026-10-25
- 影响范围：Ariel Web、Relay、Desktop Agent、真实移动浏览器回归
- 关联：[0086 大会话渐进打开](0086-progressive-open-frame-hint-and-warm-controller.md)、[0093 Large follower 空闲抢占](0093-large-follower-idle-preemption-and-readonly-recovery.md)
- 不涉及：增加 large follower 并发数、提高 64 MiB owner 帧上限、在 Relay 持久化会话正文、自动重发任何 mutation

## 1. 已确认的问题

远程打开一个需要 large follower 的会话时，Desktop Agent 可能需要依次完成空闲 large follower 抢占、Codex deep link 打开、约 30 MiB owner snapshot 初始化、状态归一化和有界首屏生成。2026-10-25 的现场日志显示：目标会话最终成功，但从 Agent 收到订阅请求到首个 snapshot 发出共耗时 23.928 秒。

当前链路仍有三个缺口：

1. `thread.subscribe` 在 Desktop Agent 内已经被视为 read request，但 Relay 和 Web timeout 分支仍按结果可能不确定的 mutation 处理，因而错误返回／显示 `OUTCOME_UNKNOWN`。
2. Agent、Relay、Web 的订阅 deadline 分别约为 28 秒、30 秒、35 秒，层间余量过小；排队、移动网络抖动或 WebSocket 写入会让外层先于已经接近成功的内层结束。
3. Web 并行发出的 `thread.read` 尚未返回时，如果 `thread.subscribe` 先失败，Web 会将 subscribe 标记为 settled 并拒绝随后到达的 read-only 历史。结果是页面保持空白全屏 loading，而不是进入可阅读、不可写的降级状态。

## 2. 目标

1. `thread.subscribe` 的超时和链路中断具有确定的、可重试的读语义，永不提示“不要直接重发”。
2. 慢 owner 冷加载有明确且嵌套的三层 deadline；正常的 30 MiB 级 owner snapshot 不因边界过窄被误判失败。
3. fresh owner 暂时不可用时，Web 继续接纳并展示同一次选择产生的 `thread.read` 结果，不清空缓存正文和草稿。
4. WebSocket 短暂重连或订阅超时后，恢复动作只重新建立订阅，不产生消息发送、停止、审批、interaction 或 queue mutation。

## 3. 协议与 deadline

### 3.1 `thread.subscribe` 分类

- Relay 将 `thread.subscribe` 视为可安全重试的 subscription setup，而不是 mutation。
- Relay 等待 Agent 超时后返回 `outcome: rejected`、`error.code: TIMEOUT`。
- Web 本地等待超时也返回 `outcome: rejected`、`error.code: TIMEOUT`。
- WebSocket 在请求期间断开时，Web 可以把 subscribe 归一化为可恢复的 `RESYNC_REQUIRED`；不得显示 mutation 专用的 `OUTCOME_UNKNOWN` 文案。
- Relay 对超时／断连后迟到的 accepted subscription 继续发送 `thread.unsubscribe`，避免 orphan subscription。

### 3.2 预算不变量

默认预算调整为：

- Desktop Agent owner load：45 秒；
- Relay subscription route：50 秒；
- Web subscription request：60 秒。

必须满足 `Agent < Relay < Web`，相邻层至少保留 5 秒用于排队、序列化与网络传输。超时仍是有界的；不允许无限等待。

## 4. Web 状态机

### 4.1 渐进历史不得因 subscribe 先失败而丢弃

对于同一 selection epoch：

1. `thread.read` 与 `thread.subscribe` 继续并行。
2. subscribe 返回 `TIMEOUT`、`RESYNC_REQUIRED`、`DEVICE_OFFLINE` 或由连接中断归一化出的可恢复错误时，若 read 仍在途，Web 等待这一个既有 read；不得重复发 read。
3. read 成功后立即展示历史并进入明确的 read-only 状态，关闭空白全屏 loading；草稿保持可编辑，发送按钮保持同尺寸 loading／disabled。
4. read 失败时关闭无限 loading，展示可重试错误和“重新连接”入口。
5. 用户已经切换会话或 selection epoch 已变化时，迟到 read／subscribe／snapshot 全部丢弃。

### 4.2 自动恢复

- 当前会话存在缓存或 read-only 历史时，WebSocket 恢复 ready 后自动发起至多一个 `thread.subscribe`。
- 同一时刻只允许一个恢复请求；多次 online／visibility 事件合并，不形成 retry storm。
- fresh snapshot 通过既有 device／thread／subscription／stream fencing 后原位接管，保留阅读位置与草稿并恢复发送。
- 自动恢复失败时继续保留历史，等待下一次明确的连接恢复或用户点击“重新连接”；不定时循环重试。

## 5. Desktop Agent 与可观测性

- Agent 的 subscribe owner load timeout 调整为 45 秒，不改变 owner 验证、large follower 单名额和首屏 4 turn／7 MiB 上限。
- Agent 记录 subscribe request 的排队开始、owner load、reply send 和 first snapshot send 总耗时；只记录 opaque request ID、thread 摘要、阶段、耗时和错误码，不记录标题、cwd、正文或原始 snapshot。
- Relay 记录 subscription route 的完成原因（accepted／timeout／web disconnected／agent disconnected）与耗时，不记录正文和 params。

## 6. 错误与安全边界

- 本规格只自动重试 `thread.subscribe`；任何 mutation 继续保持 `OUTCOME_UNKNOWN` 后禁止自动重发的规则。
- read-only 历史的 runtime、permissions、pending interactions 继续设为 unknown／empty；fresh owner 到达前不得发送、停止、审批或回答交互卡片。
- Agent 真正离线时保留缓存内容，不把缓存冒充 fresh owner。
- Relay 不保存会话正文，不增加离线队列。

## 7. TDD 计划

### 7.1 Relay

先写失败测试，再实现：

1. subscribe route 超时返回 rejected／TIMEOUT，而不是 unknown／OUTCOME_UNKNOWN。
2. 超时或 Web 断开后迟到 accepted subscription 被 unsubscribe。
3. 默认 deadline 满足 Agent < Relay < Web 的预算约束。

### 7.2 Web

先写失败测试，再实现：

1. subscribe 先 TIMEOUT、read 后成功时，历史仍出现且页面退出空白 loading，进入只读状态。
2. subscribe 期间 WebSocket 断开时不显示“执行结果不确定、不要重发”。
3. read 与 subscribe 都失败时 loading 必须结束，并出现单一恢复入口。
4. 连接恢复只产生一个 subscribe，不产生 mutation 或重复 read。
5. 迟到结果不覆盖新 selection；fresh snapshot 接管时保留草稿与阅读位置。

### 7.3 Desktop Agent

先写失败测试，再实现：

1. subscribe owner load 使用 45 秒预算。
2. 超时仍映射为 TIMEOUT。
3. 结构化阶段日志不泄露正文或 cwd。

### 7.4 真实浏览器

- 使用移动 Chromium viewport 和隔离 WebSocket fixture 注入慢 subscribe、迟到 read、连接中断与恢复。
- 验证正文、composer、loading 按钮、只读提示和重新连接入口的关键几何关系。
- 物理 iPhone／Safari 与真实 30 MiB 级 owner 会话由用户最终复验。

## 8. 验收标准

1. 现场同类大会话冷加载不再在约 24 秒时误报结果不确定。
2. 即使 owner sync 超时或移动网络短断，已缓存／持久化历史仍可阅读，页面不永久停在空白 loading。
3. `thread.subscribe` 的超时统一显示为可重试的同步失败；mutation 的不确定性保护不被放宽。
4. WebSocket 恢复后当前会话可自动重新取得 fresh owner；全过程不清空草稿、不改变阅读位置、不重复发送消息。
5. Go、Web、race、build 与真实移动 Chromium 回归通过；物理 iPhone／Safari 保留为用户验收项。
