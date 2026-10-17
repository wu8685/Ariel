# 0093：Large follower 空闲抢占与只读会话恢复

- 状态：Implemented（Go／race／Web／真实移动 Chromium／build 已验；物理 iPhone/Safari 待验）
- 日期：2026-10-17
- 影响范围：Desktop Agent、Ariel Web、诊断日志
- 不涉及：提高 64 MiB 单会话上限、增加 Relay 正文缓存、抢占活跃会话、自动重发变更请求

## 1. 背景与问题

为控制 Desktop Agent 内存，0013 规定同一进程最多持有一个 64 MiB `large follower`。0086 又将最后一个 Web subscription 退出后的 live follower 暖保留 90 秒，以加速应用前后台切换和会话往返。

两项策略叠加后存在一个缺口：大会话 A 已无订阅但仍处于暖保留期时，用户打开大会话 B，B 无法取得唯一 large follower 名额，`thread.subscribe` 返回 `OVERLOADED`。Web 虽能展示持久化历史，却只显示笼统的“Desktop 状态未确认”，禁用发送，用户无法区分暂时容量冲突与会话自身超过 64 MiB。

本规格在不增加 large follower 并发数的前提下，让当前明确选择的会话优先于无人使用的暖缓存，并让只读原因和恢复入口对用户可见。

## 2. 输入、输出与状态

### 2.1 输入

- `thread.subscribe` 对目标 thread 建立实时 owner follower。
- ordinary follower 因 owner frame 超出普通帧预算而升级到 large follower。
- large follower 唯一名额已被其他 controller 占用。

### 2.2 输出

- 若占用者只是空闲暖缓存：安全退休该 follower，目标订阅在原请求内自动重试一次。
- 若占用者仍有活跃订阅、请求 lease、bootstrap hold 或 migration：不抢占，返回 `OVERLOADED`。
- 若目标 owner frame 自身超过 64 MiB：返回 `HISTORY_TOO_LARGE`。
- Web 保留已加载历史，但明确展示具体原因和手动“重新连接”操作。

### 2.3 Web 只读原因

Web 将当前单一 `readOnlyHistory` 布尔状态扩展为至少能够保存以下 reason：

- `HISTORY_TOO_LARGE`：当前 owner 状态超过安全容量；
- `OVERLOADED`：实时连接容量暂时被活跃会话占用；
- `NATIVE_STATE_UNCERTAIN`／`TIMEOUT`／其他订阅错误：Desktop 当前状态无法确认。

reason 只用于当前页面的 UI 与恢复决策，不写入 Relay、Codex 或浏览器持久存储。

## 3. Desktop Agent 行为

### 3.1 保持原容量边界

1. 同一 Desktop Agent 进程仍最多同时持有一个 large follower。
2. 单 follower 的 `MaxFrameBytes` 与 `MaxEventBytes` 仍为 64 MiB。
3. 不通过提高 channel 容量、复制 owner 状态或延长缓冲来规避冲突。

### 3.2 可抢占对象

只有同时满足以下条件的 controller 才能成为 victim：

- 不是当前目标 controller；
- live 确认持有 large follower lease；
- `leases == 0`；
- `subscriptions == 0`；
- 不处于 bootstrap hold；
- 不处于 migration；
- 正处于 0086 定义的暖保留期。

活跃 Web subscription、正在执行的 Agent 请求、bootstrap 或 migration 任一存在时，禁止抢占。

### 3.3 抢占与重试

1. 目标 attach 首次收到 large follower `OVERLOADED` 后，在 Service controller 集合中选择最近最少使用的合格 victim。
2. 在持锁状态下重新校验 victim 条件，原子地从 victim 分离 live 并取消其退休 timer。
3. 在释放 controller 锁后关闭 victim live；关闭动作必须释放 large follower lease。
4. 在同一个 `thread.subscribe` context 与总超时预算内重试目标 attach **一次**。
5. 找不到 victim、关闭后仍无法取得名额或重试失败时，返回原始明确错误；不得循环抢占或无限重试。
6. 被退休会话保留 Codex 持久化历史与 Web 本地缓存；用户再次选择它时按正常冷 attach 重建，不丢正文。

### 3.4 并发不变量

- 同一个 live 只能关闭一次，large lease 只能释放一次。
- 退休 timer 与容量抢占竞态时，只有成功分离 live 的一方负责关闭。
- 抢占期间新 subscription 若已取得 lease，victim 必须立即失去资格。
- 不允许持有 Service 全局锁执行 Desktop IPC close 或新的 attach。

## 4. Web 行为

### 4.1 明确提示

- `OVERLOADED`：显示“实时连接资源繁忙；当前历史可读，但发送暂时禁用”，并提供“重新连接”。
- `HISTORY_TOO_LARGE`：显示“当前会话状态超过 64 MiB 安全上限；历史可读，但无法建立实时连接”。不进行自动重试风暴。
- 其他错误：显示对应现有错误文案，并提供一次用户触发的重新连接入口（确定性不支持错误除外）。

### 4.2 重新连接

1. 点击“重新连接”仅重新发起当前 `thread.subscribe`；不发送消息、不停止 turn、不提交审批。
2. 重试期间保留当前历史和草稿，发送按钮维持同尺寸 loading 状态并继续禁用。
3. 成功收到 fresh owner snapshot 后解除只读、恢复发送，并保留阅读位置。
4. 失败时继续保留历史，更新为最新明确原因；不得清空页面或重复 `thread.read` 已有历史。
5. 同一时刻只允许一个恢复请求；重复点击不得制造并发订阅。

## 5. 诊断与隐私

Desktop Agent 对订阅失败输出结构化日志，至少包含：

- phase：`large-follower-preempt`、`large-follower-retry` 或 `subscribe-failed`；
- 目标 thread 的现有不透明 ID；
- error code；
- 是否找到并退休空闲 victim；
- 重试次数与耗时。

日志不得包含会话标题、cwd、消息正文、附件或 owner 原始 frame。

## 6. 错误与降级

- 没有可抢占 victim：返回 `OVERLOADED`，历史只读。
- victim close 失败但 lease 已释放：记录 close error，仍只进行一次目标重试。
- victim close 后名额仍不可用：返回 `OVERLOADED`，不继续淘汰其他 controller。
- 目标状态超过 64 MiB：保持 `HISTORY_TOO_LARGE`，不得将其误报为 `OVERLOADED`。
- Web 恢复过程中 Agent 离线：进入既有 Agent 重连流程，保留历史与草稿。

## 7. TDD 计划

### 7.1 Desktop Agent

先写并运行失败测试，再实现：

- 空闲暖缓存 large follower 被退休，新大会话订阅成功。
- 有活跃 subscription 的 large follower 不被抢占，目标返回 `OVERLOADED`。
- 有 lease、bootstrap hold 或 migration 的 controller 不被抢占。
- 多个候选只选择 LRU victim，并且只重试一次。
- timer 与抢占并发不会 double close／double release；使用 `go test -race` 验证。
- 目标自身超过 64 MiB 仍返回 `HISTORY_TOO_LARGE`。

### 7.2 Web

- `OVERLOADED` 与 `HISTORY_TOO_LARGE` 展示不同提示。
- 只读历史、草稿和阅读位置在手动重连期间保留。
- 重连按钮只产生一个 `thread.subscribe`，不重复 `thread.read`，不产生 mutation。
- fresh snapshot 到达后解除只读并恢复发送。
- 重试再次失败时继续显示历史并更新原因。

### 7.3 真实浏览器

- 以两个需要 large follower 的隔离 fixture 验证 A 退出后立即打开 B，不再进入只读。
- 保持 A 活跃时打开 B，确认 A 不被抢占、B 明确显示容量繁忙。
- 手机 viewport 验证提示、重连按钮和 composer loading 不遮挡正文。

## 8. 验收标准

1. 唯一 large follower 被无人使用的暖缓存占用时，新选中的大会话可自动接管并恢复发送。
2. 活跃会话永不因另一会话被隐式抢占。
3. 64 MiB 总边界与单名额约束不变。
4. Web 能区分容量繁忙、会话过大和其他状态不确定，且提供安全的恢复入口。
5. 恢复过程不清空历史、草稿或阅读位置，不触发任何变更请求。
6. Go、Web、race 与真实浏览器回归全部通过；物理 iPhone/Safari 仍由用户最终复验。
