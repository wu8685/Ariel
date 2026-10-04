# 0013：大会话分页历史与原 owner 隔离

- 状态：Draft，待用户确认后进入 TDD。
- 来源：2026-10-04 手机／Web 反馈：部分会话一直显示“正在同步会话”，同时 Agent 反复离线。
- 依据：[0004 原 Desktop Agent](0004-m2-real-desktop-agent.md)、[0006 断线恢复与有界容量](0006-m4-recovery-capacity.md)、[分页 API 只读复核](../testing/2026-10-05-0013-pagination-probe.md)、[OpenAI App Server 文档](https://learn.chatgpt.com/docs/app-server)。本规格仅对大会话的历史窗口、分页与本机大帧适配作例外；原 owner 权威、无第二套会话数据库和不自动重发变更继续有效。

## 已确认的故障

- 目标开发会话的 `thread/read(includeTurns: true)` 单条响应约 43.3 MB，超过当前 App Server 8 MiB 读取上限；Reader 将整条连接关闭，Agent 误报 App Server 不可用。
- 同一会话由 Desktop owner 发出的完整 IPC 快照帧约 44.6 MB，超过当前 8 MiB IPC 上限。仅修复前一层仍无法建立实时订阅。
- `thread/turns/list` 在当前 Desktop 内置 Codex 0.160.0 可按 cursor 读取 `itemsView: full`；这是 experimental API，必须明确 opt-in。2026-10-05 对隔离文件变更 fixture 的只读对比发现，`itemsView: summary` 省略 `fileChange`、`reasoning` 与一条中间 `agentMessage`，不能作为完整历史展示。以上只读探针没有发送消息或保存会话正文。

## 输入、输出与行为

1. Web 选择会话时，Agent 只用 `thread/read(includeTurns: false)` 取得身份、cwd、标题和存储状态，不再取整条历史。原 owner 身份与 cwd 核对、未加载历史自动打开和变更安全门禁保持不变。
2. 正常 owner 快照仍走 8 MiB IPC 额度。若且仅若该会话快照帧超过 8 MiB，关闭该 follower 并为该会话尝试一次大快照 follower：本机帧、reducer 状态与事件缓冲各设明确的 64 MiB 上限，同一 Agent 进程最多一个大快照 follower。不得提高 Relay／Web 的 8 MiB 单帧限制，也不得把其他普通会话一起改为大缓冲。
3. Agent 完整校验 owner 状态，再仅向 Web 发送一个有界的最近历史窗口：最多最近 10 个 turn，且整个规范化 `thread.snapshot`／`thread.update` 不超过 7 MiB。优先保留最新 turn、当前 pending interaction、runtime 和当前权限；不能为了满足容量静默截断单条消息、隐去 pending 决定或编造 ID。截掉较旧 turn 时显式标记 `historyComplete: false`；全部包含时为 `true`。
4. 新增只读 `thread.history` 请求，输入 `threadId`、可选原生 opaque `cursor`、`limit`（最大 10）；输出按“较新到较旧”排序的已规范化 turns 与 `nextCursor`。Agent 使用 `thread/turns/list(itemsView: full)`，页面容量超标时减小 limit 重试，最小到单个 turn；单个 turn 仍超标则只报该页 `HISTORY_TOO_LARGE`，保留 Agent、当前视图与其他会话。不得用 `summary` 冒充完整 turn，不得静默截断消息、工具或文件变更 item；不支持 experimental 分页的版本显式报 `PROTOCOL_UNSUPPORTED`。
5. Web 在 `historyComplete: false` 时提供“加载更早消息”。历史页只保存在该标签页内存，按 turnId 去重并拼接在当前窗口之前。第一页与 live 窗口重叠时自动继续取下一页，直到得到更早 turn、到达末尾或发生错误。加载失败不抹掉已有消息；切换会话、stream 改变或重连时清除旧页与 cursor，从当前 owner 重新构建视图。Web 同时只保留有界数量的历史页；翻阅更早内容时释放不在当前窗口的页，并提供返回最新消息入口。
6. App Server 某一响应行超出 8 MiB 时，Reader 有界丢弃该行、让对应只读调用返回 `HISTORY_TOO_LARGE`，但不关闭健康的 App Server session 或 Agent；其他并发调用可重试。真正的 framing／JSON 错误、子进程退出仍按原有断线规则处理。
7. 若 owner 帧超过 64 MiB、大快照名额已占用或版本适配失败，Web 仍可用 `thread.history` 浏览已持久化的分段历史，但该会话标记为“历史只读／当前 Desktop 状态不可确认”，禁用发送、停止和审批。不能把持久化历史页冒充原 owner 的实时状态。其他会话与设备保持在线。

## 不变量、边界与错误

- Relay 不保存正文、不拼接历史；Agent 不建立永久历史副本。Web 的浏览窗口只在内存中，页面刷新后从 Codex 重新读取。
- 每个 WebSocket 事件仍小于现有 8 MiB 限额；每页序列化结果需留出 envelope 裕量。IPC 大帧能力仅适用于版本验证过的 Desktop 26.930.31730／内置 Codex 0.160.0 Adapter。
- cursor 仅对相同 thread 和当前浏览周期有效；错误 cursor、重叠页、空页、重复 turn ID、分页期间新 turn 到来均不得造成重复展示、缺失提示或错误写入。
- 单个最新 turn 或 pending 卡片本身超过安全传输上限时，明确报告 `HISTORY_TOO_LARGE` 并禁用该会话远程变更；不允许只显示一部分却保持“可发送”。
- `thread.history` 是只读操作，不触发 Desktop 加载、发送或恢复变更；Web 对任何变更仍以原 owner 快照和精确身份为门禁。

## TDD 与验收

1. 先测 App Server 超大响应仅使单个调用失败、后续 `thread/list` 仍可用，再实现有界丢弃；验证畸形 JSON 仍 fail closed。
2. 先测 `thread/read(false)` 与 `thread/turns/list(itemsView: full)` 的参数、cursor、排序、版本拒绝和单页超限；测试须证明文件变更与中间消息没有被 `summary` 行为吞掉，再实现分页 Adapter；用当前 Desktop 内置 binary 做只读兼容性回归。
3. 先测 8 MiB 普通 follower、大快照单名额／64 MiB 上限、失败释放名额、其他会话仍在线，再实现大 follower；不得向业务会话发送测试文本。
4. 先测最近窗口保留当前 pending／权限、旧 turn 不静默混入更新、7 MiB 上限、Web 旧页去重／切换清理／错误保留，再实现协议和 UI。
5. 全量 Go／Web 测试、构建、Relay 真链路回归；对真实大会话只读验证“可见最近消息、可加载更早、Agent 仍在线”，发送／停止只在隔离 fixture 上实测。物理手机由用户最终复验。
