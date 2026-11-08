# Ariel 全项目架构与代码 Review（Turn 55）

日期：2026-11-09

基线：`main@ba54502d`

范围：Web ↔ Relay ↔ Desktop Agent ↔ Codex、Puck Brain、协议、数据库与发布回滚

方法：静态代码审查、失败容器/数据库/本地日志的只读取证、定向回归测试与隔离环境验证。

## 结论摘要

Turn 54 的两个表象不是同一故障：greeting 慢来自真实 Brain 首轮推理；结构化提问缺失来自 Brain 生成了非 canonical 的 `type` 字段，严格参数校验拒绝了工具调用。前者通过 greeting 使用 `low` effort 降低延迟，后者通过仅接受已观测到的 selection-type 别名解决；两者都没有改变公开协议或 toolset schema。

当前最需要优先处理的系统性风险有三项：Puck 事件通道静默丢弃、Puck watch 的高频全量轮询、SQLite schema 升级后的回滚不兼容。它们分别会造成分钟级“卡住”、随 watcher 数量放大的传输/数据库开销，以及回滚时的数据损失窗口。后两项和事件队列重构都需要设计评审，本轮没有贸然修改。

## Turn 54 根因与证据

### 1. greeting_processing 长时间停留

- 失败数据库中三个临时 Bot 的 greeting 分别约为 116–123 秒。
- 对应 Brain JSONL 的时间线显示：进程/会话准备约 16–19 秒，随后约 100 秒发生在首轮模型 reasoning。期间没有 Relay 锁等待、队列阻塞或 schema 往返重试证据。
- Ariel 的 Brain 启动顺序符合 Codex app-server 的 initialize → thread/start → turn/start 生命周期；问题在于 greeting 这种低价值、短输出任务沿用了默认 reasoning effort，而不是生命周期顺序错误。
- 修复：`internal/puck/module.go:566` 为 greeting 单独发送 `effort: low`；普通用户 turn 不受影响。
- 新增可观测性：`internal/desktopagent/brainhost.go:119-145,191-230,260-263,379-391` 记录 process/thread/turn/tool 的无敏分阶段耗时、结果码和参数字节数，不记录提示词、工具参数或完整 thread ID。

### 2. ask_user 结构化卡片未出现

- Brain 在用户消息后约 6.7 秒调用了 `ask_user`，并非“没有调用工具”。
- 实参额外包含 `questions[0].type = "single-select"`；canonical schema 用 `multiSelect` 表达同一语义，因此严格校验立即返回 `INVALID_ARGUMENT / additional property not allowed`。
- Brain 随后完成为普通文本。因为成功的 tool call 从未到达 Relay，数据库和 Web 都没有结构化卡片可渲染；这排除了 Relay 写回或前端渲染失败。
- 修复：`internal/puck/toolcontract/contract.go:204-244` 只兼容已观测到的 `single-select` / `multi-select`，映射为 `multiSelect`；冲突值、未知值和其他未知字段仍拒绝。公开 schema 与 toolset version 不变。

### 3. 为什么此前日志难以定位

- Brain app-server stderr 被直接丢弃：`internal/codex/appserver/interactive.go:270-277`。
- Relay 容器日志只有进程启动信息，没有 Brain phase、tool validation 或队列 drop 指标。
- 本轮新增 phase log 能定位跨层耗时，但受控 stderr 与指标仍应作为后续工作。

## 按优先级排序的发现

| 优先级 | 层次 | 发现与证据 | 影响 | 建议 | 规模 | 风险/状态 |
|---|---|---|---|---|---|---|
| P0 | 稳定性 | Brain event 写入满为 128 的 channel 时静默丢弃：`internal/relay/puck_bridge.go:312-317`；device status 同样静默丢弃：`:320-328` | 丢失 tool call/turn completion 后，上游只能等分钟级 timeout，用户看到“卡住”，且日志无证据 | 先加 drop counter、assistant/tool 维度的无敏日志和明确失败 receipt；再改为每 Bot 有界 mailbox，并区分可合并状态与不可丢业务事件 | 大 | **明天讨论**；队列语义改变需设计 |
| P0 | 稳定性 | 新版本 schema 会原地 `ALTER TABLE` 并提升 `user_version`，旧程序遇到更高版本直接拒绝启动：`internal/puck/store.go:94-103,145-178` | 回滚必须恢复整库快照；升级后产生的数据会丢失 | 采用 expand/contract 两阶段迁移、声明 min/max schema compatibility、旧版忽略新增列；发布前做 SQLite online backup，并验证旧镜像可读副本 | 大 | **明天讨论**；涉及迁移政策与发布流程 |
| P1 | 传输效率 | 每个 Puck watcher 每 250ms 查询 Bot + 最近 200 条消息、JSON 全量序列化并比较：`internal/puck/module.go:84-135` | CPU、SQLite 查询与内存分配按 watcher 线性放大；消息历史越大越明显 | Store mutation 后递增 revision 并 notify；事件传增量，重连/缺口才给有界 snapshot | 大 | **明天讨论**；协议与状态机调整 |
| P1 | 传输效率/体验 | 即使已安装 `puck.watch`，Web 仍每 10 秒拉全量 history，列表每 5 秒轮询：`web/src/puck/PuckWorkspace.tsx:387-451` | 重复流量、移动端耗电；多个页面叠加放大；watch 故障与正常路径不可观测 | watch 建立后只做低频健康校验；用 revision 检测缺口后再补拉；列表变更走事件 | 中/大 | **明天讨论**；依赖上一项 revision 设计 |
| P1 | 稳定性 | `Supervisor.Call` 超时只让调用方返回，忽略 context 的 handler goroutine 可继续执行；`Go` panic 后不重启：`internal/puck/supervisor.go:41-71` | 幽灵 mutation、重复执行、goroutine 泄漏；“supervisor”并不保证恢复 | 改为单 owner worker queue；所有 operation 明确 cancellation/幂等；后台 loop 使用带 backoff 的 restart policy | 大 | **明天讨论** |
| P1 | 稳定性 | create 的 Bot 上限检查与插入不是单一事务/互斥区间；模块多数操作持 `RLock`：`internal/puck/module.go:435-447,517-545` | 并发 create 可能越过 10 个上限 | Store 中用事务完成 active count + insert，或为 provisioning 建立串行 owner | 中 | 明天讨论；需并发测试与 SQLite 锁策略 |
| P1 | 稳定性 | provisioning 已写库后，确定性的坏状态（缺 thread identity / SetAssistantBrain 失败）会留下无法恢复但占容量的幽灵 Bot；瞬时 ensure 失败则已有 update 重试语义 | 用户反复遇到坏响应后可能耗尽 10 Bot 上限；误删可重试对象又会破坏恢复 | 仅对确定性坏状态 soft-delete；SetAssistantBrain 失败时同时 retire 已建 Brain；瞬时失败保留 provisioning | 小 | **已在 turn 55 实现**：`internal/puck/module.go:547-559`，并保留既有重试回归 |
| P1 | 产品体验 | Web 默认 12s timeout 小于 Relay Puck mutation 的 45s 上限，慢 Brain 会让 UI 先显示 outcome unknown：`web/src/client.ts:17-30` | 操作可能最终成功但用户以为失败，重试造成重复意图 | Puck mutation 客户端 deadline 设 50s；读请求策略不变 | 小 | **已在 turn 55 实现**，含 timeout contract 测试 |
| P1 | 稳定性/诊断 | app-server stderr 被丢弃：`internal/codex/appserver/interactive.go:270-277` | schema 拒绝、启动异常和 runtime warning 无法追溯 | 建立有界、脱敏、可轮转的本地 diagnostic sink；默认不上传，不含正文 | 中 | phase timing **已实现**；stderr sink 明天讨论 |
| P1 | 稳定性 | Brain app-server 使用固定 `--disable` 列表；Codex 0.145 移除 `view_image` 后，未知 feature 令进程启动即退出：原位置 `internal/codex/appserver/interactive.go:261-267` | Codex 升级可让所有新 Bot 立即 `BRAIN_UNAVAILABLE` | 启动前读取当前 binary 的 feature inventory，只传仍支持的控制项；无法验证安全 surface 时 fail closed | 小 | **已在 turn 55 实现**：`internal/codex/appserver/interactive.go:261-309`，真实 app-server gate 通过 |
| P1 | 传输效率 | Agent 重连后 cache follower 按优先级串行恢复，任一 send 失败便终止：`internal/relay/cache_server.go:11-49` | hot thread 多时恢复耗时线性增长；一个失败阻断后续恢复 | 有界并发批次；单 key 失败不阻断其他 key；保留 active/watching 优先级 | 中 | 明天讨论；需要压力与顺序测试 |
| P2 | 稳定性 | Relay transport backoff 成功连接后不复位：`internal/desktopagent/agent.go:233-253` | 经历过多次短断线后，之后每次重连固定多等 5s | ready 后将 delay 复位为 200ms，并为抖动/长期稳定两类序列加测试 | 小 | 明天可直接做；本轮避免扩大验证面 |
| P2 | 传输效率 | Relay 与 Agent 的 websocket 建连没有显式 compression 配置：`internal/relay/server.go:381-385`、`internal/desktopagent/agent.go:262-267` | 全量 thread/history snapshot 可能浪费带宽；但开启压缩也会增加 CPU/内存 | 先采样 frame 大小与可压缩率，再决定 permessage-deflate；敏感内容场景同时评估压缩侧信道 | 中 | **明天讨论**；不能凭假设直接开启 |
| P2 | 可观测性 | heartbeat 已采用 30s/15s 和连续两次失败阈值：`internal/wsheartbeat/heartbeat.go:9-55`，但缺少 RTT/失败原因统计 | 无法区分公网抖动、事件循环阻塞与对端过载 | 增加 RTT histogram、连续失败计数、disconnect reason；按 device 聚合，不记录 token | 小/中 | 机制本身保留；指标明天做 |
| P2 | 产品体验 | authorization 剩余时间只在存在 running message 时每秒触发重渲染：`web/src/puck/PuckWorkspace.tsx:381-385` | 页面空闲时倒计时文字可能陈旧 | pending authorization 存在时也启动低频 clock | 小 | 明天可直接做，需 fake timer 测试 |
| P2 | 维护性 | 核心文件过大且混合多类职责：`internal/desktopagent/service.go`、`internal/desktopagent/agent.go`、`internal/relay/server.go`、`internal/puck/module.go`、`web/src/App.tsx` | 局部改动回归面大，并发 owner 与安全边界难审查 | 先按 owner/route 拆包与 facade，不改变协议；每次拆分以现有测试为 characterization gate | 大 | **明天讨论**；纯重构也需分阶段 |
| P2 | 产品体验 | `FEATURE_DISABLED` 的通用映射带有“文件引用”特定文案：`web/src/App.tsx:126` | 其他能力被禁用时提示误导 | 按 method/feature 提供上下文化文案，保留未知 fallback | 小 | 明天可直接做 |

## 已确认的良好设计

- Desktop Agent 对 thread snapshot 已做结构变化立即发、普通变化节流、每 thread 只保留 latest pending：`internal/desktopagent/agent.go:1165-1229`。这比无差别批处理更符合 streaming 体验，后续 delta 协议应保留这一语义。
- Relay cache 的待发送更新同样按 key latest-only 合并：`internal/relay/server.go:544-580`，能抑制慢 Web 导致的无界积压。
- heartbeat 要求连续两次失败才断开，避免一次瞬时抖动触发重连；无需改成更激进的单次失败策略。
- Puck send 已有 optimistic processing card：`web/src/puck/PuckWorkspace.tsx:453-478`。本轮将 mutation deadline 调整到与服务端真实耗时边界一致，避免破坏这一体验。

## 已在 Turn 55 实现的小改动

1. greeting 专用 `low` effort，普通 turn 不变。
2. `ask_user` 对已观测 selection `type` 漂移做窄兼容；冲突和未知字段仍拒绝。
3. Brain process/thread/turn/tool 分阶段无敏耗时日志。
4. provisioning 失败自动 soft-delete；部分成功时回收 Brain thread。
5. Puck mutation Web timeout 从默认 12s 调整到 50s，与 Relay 45s 边界匹配。
6. 复用并扩展隔离的真实 Brain E2E，覆盖五个串行场景，并记录 greeting/首个工具调用耗时；临时 Bot 用后删除。
7. 启动 Brain 前按当前 Codex feature inventory 构造 `--disable` 参数；已移除的 feature 不再导致 app-server 启动即退出，inventory 不可用时 fail closed。

这些改动均不改变 Ariel protocol、Puck toolset schema、数据库 schema 或现有持久化格式。

## 明天讨论：需要决策的方案

### 1. Puck event delivery

- **方案 A：每 Bot 有界 mailbox + 不可丢事件背压。** 语义清楚，能隔离 noisy Bot；实现复杂度中高。
- **方案 B：持久 event outbox + ack。** 宕机恢复最强，但会引入第二类持久状态与清理策略，不宜在需求未证明前采用。
- 建议先做 A，并把状态类事件 coalesce、tool/turn completion 定义为不可丢；满载时快速失败当前 turn，而不是静默等待 timeout。

### 2. Puck watch 与增量同步

- **方案 A：单调 revision + mutation notify + 缺口补 snapshot。** 与现有 API 最接近，推荐。
- **方案 B：通用 JSON Patch。** 协议通用，但 patch 生成、校验和客户端状态漂移成本高。
- **方案 C：领域事件日志。** 可审计、可重放，但容易形成第二套状态机，和当前轻量 Relay 原则冲突。

### 3. DB 向后兼容与回滚

- 规定至少 `N` 与 `N-1` 两个应用版本可读同一 schema。
- 加列先 expand：nullable/default、旧版可忽略；下一发布才开始依赖；删除/重命名延后 contract。
- 发布工具对数据库副本运行“新镜像升级 → 旧镜像只读打开”兼容 gate。
- 真正不可兼容时，升级前短暂停写并保存 WAL 一致性快照；回滚需明确数据 replay 或维护窗口，不能只靠整库覆盖。

### 4. Thread 全量 snapshot 与压缩

- 先增加 `snapshot_bytes`、`updates_coalesced`、`RESYNC_REQUIRED{reason}`、重连恢复耗时指标。
- 有数据后再选择 append/delta + 周期 checkpoint，还是只启用 websocket compression。前者节省 CPU/网络更多，但协议复杂；后者改动较小，却不能解决重复 normalize/serialize。

### 5. 可观测性最小集

- Brain：process/thread/turn/tool latency（本轮已有日志，后续转 histogram）。
- Relay：route latency、timeout、event drop、queue depth、cache bytes/eviction、RESYNC reason。
- Agent：reconnect reason/backoff、heartbeat RTT、follower restore duration、app-server restart。
- Puck：active/provisioning Bot 数、watcher 数、DB busy/latency。
- 所有维度必须使用摘要 ID 或低基数标签，不记录提示词、token、完整 thread ID 或工具实参。

## 验收说明

代码门禁与本地真实 Brain 五场景冒烟结果在本次交付报告中记录；若真实 Brain 冒烟任一场景失败，本分支不应被视为可部署候选。
