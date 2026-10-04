# 0013：大会话最近十回合与 64 MiB 有界同步

- 状态：Draft；用户于 2026-10-05 确认本机原会话容量 64 MiB、手机默认最新 10 个 turn，并保留按需加载更早消息；本版文字待确认后进入 TDD。
- 来源：2026-10-04 手机／Web 反馈：部分会话一直显示“正在同步会话”，同时 Agent 反复离线。
- 依据：[0004 原 Desktop Agent](0004-m2-real-desktop-agent.md)、[0006 断线恢复与有界容量](0006-m4-recovery-capacity.md)、[分页 API 只读复核](../testing/2026-10-05-0013-pagination-probe.md)、[OpenAI App Server 文档](https://learn.chatgpt.com/docs/app-server)。本规格只放宽本机原会话读取／同步容量，不放宽 0006 的 Relay→Web 8 MiB 单帧上限；原 owner 权威、无第二套会话数据库和不自动重发变更继续有效。本文的 64 MiB 指 67,108,864 字节。

## 已确认的故障

- 目标开发会话的 `thread/read(includeTurns: true)` 单条响应约 43.3 MB，超过当前 App Server 8 MiB 读取上限；Reader 将整条连接关闭，Agent 误报 App Server 不可用。
- 同一会话由 Desktop owner 发出的完整 IPC 快照帧约 44.6 MB，超过当前 8 MiB IPC 上限。仅修复前一层仍无法建立实时订阅。
- `thread/turns/list` 在当前 Desktop 内置 Codex 0.160.0 可按 cursor 读取 `itemsView: full`；这是 experimental API，必须明确 opt-in。2026-10-05 对隔离文件变更 fixture 的只读对比发现，`itemsView: summary` 省略 `fileChange`、`reasoning` 与一条中间 `agentMessage`，不能作为完整历史展示。
- 对用户反馈的大会话进行仅输出大小与计数的只读复核：47 个 turn 中有一个约 14.3 MB／1,628 item，单 turn 本身无法作为 7 MiB Web 页发送；该 turn 最大单 item 约 1.08 MB。`thread/items/list` 每页 100 item 共 17 页可精确取回全部 1,628 item；倒序前两页也与原 turn 的最新 200 item 完全一致。以上探针没有发送消息或保存会话正文。

## 输入、输出与行为

1. Web 选择会话时，Agent 只用 `thread/read(includeTurns: false)` 取得身份、cwd、标题和存储状态，不再取整条历史。原 owner 身份与 cwd 核对、未加载历史自动打开和变更安全门禁保持不变。`thread.read` 和 `thread.subscribe` 对 Web 均返回同一最近窗口语义。
2. App Server 只读响应、Desktop IPC 原 owner 帧、follower reducer 状态与事件缓冲的**单会话**容量统一设为 64 MiB；8 MiB 以内的普通 follower 仍走普通缓冲，超过后至多启动一个大 follower，失败要释放名额。进程级预算必须计入未处理帧、reducer 副本和事件队列，避免 64 MiB × 64 会话的无界累计。超过 64 MiB 时只让该会话报明确的 `HISTORY_TOO_LARGE`／只读退化，不使 Agent 或其他会话假离线。
3. Agent 校验完整的原 owner 身份、cwd、runtime、pending interaction 和当前权限，再仅规范化并向 Web 同步**最新 10 个 native turn**（不足 10 个则全部）；10 指 turn，不是消息或 item。它们按时间正序展示，当前 pending/runtime/权限始终来自原 owner。`historyComplete` 表示是否还有更早 turn；实时更新进入新 turn 后最近窗口滑动，但已加载的旧页仍明确区分于 live 窗口。不得静默截断消息、隐藏待审批项或编造 ID。
4. Relay 与 Web 继续保持 8 MiB 单帧上限；规范化响应／事件的目标上限为 7 MiB，保留 envelope 裕量。若最近 10 个 turn 合计超过 7 MiB，首帧发送能完整容纳的最新 turn 和 owner 状态，显式标记 `recentComplete: false`，Web 自动分段补齐其余最近 turn 到 10 个后才标记完整；不要求用户点击“加载更早消息”才能补齐默认窗口。若单个 turn 超 7 MiB，首帧只带该 turn 的明确标记的最新有界 item 段（`itemsComplete: false`），其余 item 以分页补读；当前 live turn 无法安全分段或单个 pending 卡片本身超限时，明确报 `HISTORY_TOO_LARGE`，禁用该会话远程变更，不让 UI 一直停在同步中。
5. 新增只读 `thread.history` 请求，输入 `threadId`、可选原生 opaque `cursor`、`limit`（最大 10）；输出按“较新到较旧”排序的规范化 turns 与 `nextCursor`。Agent 优先使用 `thread/turns/list(itemsView: full)`，响应或 Web 页超限时缩小 limit 重试。不得用 `summary` 冒充完整 turn，不得静默截断消息、工具或文件变更 item；不支持 experimental 分页的版本显式报 `PROTOCOL_UNSUPPORTED`。
6. 当单个完整 turn 仍超出 7 MiB 页预算时，Agent 使用 `thread/turns/list(itemsView: notLoaded)` 获取该 turn 的身份／状态与后续 turn cursor，并用 `thread/items/list(turnId, sortDirection: desc)` 读取其最新有界 item 页。`thread.history` 标为 `itemsComplete: false` 并给出 `nextItemCursor`；新增只读 `thread.history.items`，输入 `threadId`、`turnId`、原生 opaque item cursor、`limit`（最大 100），返回更早的规范化 items、`nextItemCursor` 和是否完整。Web 将倒序页恢复成正序、按 itemId 去重，提供“加载此回合更早内容”；单 item 仍超限时仅该 item 页报 `HISTORY_TOO_LARGE`，保留其他可见内容。自动补齐默认最近 10 turn 与用户按需加载更早 turn 使用同一分页能力，但 UI 分别显示完成状态。
7. Web 在 `historyComplete: false` 时提供“加载更早消息”。历史页只保存在该标签页内存，按 turnId 去重并拼接在最近窗口之前；巨型 turn 的 item 页与 turn 页分别跟踪完成标记和 cursor。第一页与 live 窗口重叠时自动继续取下一页，直到得到更早 turn、到达末尾或发生错误。加载失败不抹掉已有消息；切换会话、stream 改变或重连时清除旧页与 cursor，从当前 owner 重新构建视图。Web 同时只保留有界数量的旧页；翻阅更早内容时释放不在当前窗口的页，并提供返回最新消息入口。
8. App Server 单条 JSONL 响应超过 64 MiB 时，Reader 有界丢弃该行，让对应只读调用返回 `HISTORY_TOO_LARGE`；无法可靠归属的异常响应则重启该只读 App Server 子进程，不关闭 Relay Agent。畸形 JSON、子进程退出仍按原有错误语义处理，但不能把一个大会话失败误报为所有设备离线。分页单页目标仍是 7 MiB，而非把 64 MiB 原样发到手机。
9. 若 owner 帧超过 64 MiB、大 follower 名额已占用或版本适配失败，Web 仍可用 `thread.history` 浏览已持久化的分段历史，但该会话标记为“历史只读／当前 Desktop 状态不可确认”，禁用发送、停止和审批。不能把持久化历史页冒充原 owner 的实时状态。其他会话与设备保持在线。

## 不变量、边界与错误

- Relay 不保存正文、不拼接历史；Agent 不建立永久历史副本。Web 的浏览窗口只在内存中，页面刷新后从 Codex 重新读取。
- 每个 WebSocket 事件仍小于现有 8 MiB 限额；每页序列化结果需留出 envelope 裕量。IPC 大帧能力仅适用于版本验证过的 Desktop 26.930.31730／内置 Codex 0.160.0 Adapter。
- turn／item cursor 仅对相同 thread（item cursor 还绑定同一 turn）和当前浏览周期有效；错误 cursor、重叠页、空页、重复 turn／item ID、分页期间新内容到来均不得造成重复展示、缺失提示或错误写入。
- 未完整传输的 turn／item 必须显式显示“尚有更早内容”；不能用部分文本冒充完整原文。最新 live turn 无法安全分段或 pending 卡片本身超过安全传输上限时，明确报告 `HISTORY_TOO_LARGE` 并禁用该会话远程变更；持久化历史的显式分段展示不能伪装成已验证的原 owner 当前状态。
- `thread.history` 是只读操作，不触发 Desktop 加载、发送或恢复变更；Web 对任何变更仍以原 owner 快照和精确身份为门禁。

## TDD 与验收

1. 先测 App Server 响应低于／等于／超过 64 MiB 的边界、超大响应仅使单个调用失败、后续 `thread/list` 仍可用，再实现有界丢弃；验证畸形 JSON 仍 fail closed。
2. 先测 `thread/read(false)`、`thread/turns/list(itemsView: full)` 与巨型 turn 的 `thread/items/list` 参数、两级 cursor、排序、版本拒绝、重复／空页和容量递减；测试须证明文件变更与中间消息没有被 `summary` 行为吞掉，再实现分页 Adapter；用当前 Desktop 内置 binary 做只读兼容性回归。
3. 先测 8 MiB 普通 follower、大快照单名额／64 MiB 上限、失败释放名额、总内存预算和其他会话仍在线，再实现大 follower；不得向业务会话发送测试文本。
4. 先测最近 10 turn 的准确计数与顺序、当前 pending／权限、7 MiB 首帧、超预算自动补齐而非静默丢弃、巨型 turn item 页、实时滑动窗口、Web 旧页去重／切换清理／错误保留，再实现协议和 UI。
5. 全量 Go／Web 测试、构建、Relay 真链路回归；对真实大会话只读验证“可见最近消息、可翻到 1,628-item 的巨型旧 turn 并继续加载其完整内容、Agent 仍在线”，发送／停止只在隔离 fixture 上实测。物理手机由用户最终复验。
