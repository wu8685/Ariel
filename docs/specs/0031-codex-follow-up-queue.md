# 0031：Codex 原生后续输入队列

- 状态：Implemented（自动测试与真实 Codex queue CRUD／Desktop owner Steer 已验；large follower 能力透传回归已验；物理手机拖拽待用户复验）。
- 背景：Codex Desktop 在当前 turn 运行期间允许继续提交后续输入，并对排队项执行立即引导、编辑、删除和重排。Ariel 当前在运行期间只允许写草稿，发送按钮被禁用。
- 依赖：沿用 0004 的 Desktop owner 写入边界、0013 的有界同步和 0025 的最低版本兼容原则。

## 接口调研结论

1. Codex App Server 的稳定文档公开了 `turn/steer` 和 `turn/interrupt`；前者把新输入加入当前运行，后者停止当前 turn。
2. 当前 Codex `0.160.0` 还提供 `thread/queue/add`、`list`、`update`、`delete`、`reorder`、`start` 及 `thread/queue/changed`，但这些 method 在官方协议源码中标记为 experimental，不能视为稳定兼容承诺。
3. 队列由 Codex 自身持久化到 thread store。外部 App Server 写入后，Desktop owner 会发现 durable queue 变化；当前 turn 正常结束后按 FIFO 自动开始下一项。硬中断会暂停队列，不会删除排队项。
4. Codex Desktop 的“Steer”不是硬中断：它在当前 active turn 的安全边界追加指导，不撤销已经产生的文本或已经开始的工具调用。Ariel 中文界面使用“立即引导”，不把它误写为“中断”。现有“停止”按钮继续对应真正的 `turn.interrupt`。

## 用户可见行为

1. 当会话正在运行且设备声明支持 queue 时，输入框仍可发送。发送按钮文案和 accessible name 变为“加入队列”；成功后清空当前草稿和截图，并在输入框上方立即显示排队项。
2. 排队项按从上到下的执行顺序展示，最上方是队首；每项显示单行文本摘要和图片数量，并按 Codex 的交互布局提供：
   - 左侧拖拽手柄：手机和桌面均可直接重排；键盘聚焦后可用方向键完成同一操作。每次提交完整 queue ID 顺序，成功后更新显示。
   - 右侧“引导”：仅在 active turn 存在时可用；把该项 Steer 到当前 turn，确认 owner 接收后再从 durable queue 删除。
   - 右侧删除图标：仅删除选中的 durable queue 项，不影响当前 turn。
   - 最右侧更多菜单：提供“编辑”“上移”“下移”等低频操作。编辑会将内容装入原输入框；保存后更新原排队项，不新建重复项。图片随排队项保留，并可继续增删。
3. 多个浏览器或 Codex Desktop 修改队列后，Ariel 在下一次 owner 状态更新时回读权威 queue；Ariel 自己完成的变更用 method 返回值立即更新，不等待 Codex 的外部变更轮询。
4. 当前 turn 正常完成后，Codex Desktop owner 自动执行队首。用户硬停止当前 turn 时，队列保持并暂停；Ariel 不擅自创建第二个 executor，也不自动重放。
5. 不支持 queue 的 Codex 版本不显示排队面板，运行期间保持原来的草稿模式；首次原生 method 不可用时显式报告“当前 Codex 不支持排队输入”。

## Ariel 协议

- Agent capability 新增可选布尔值 `queue`。
- Thread 新增可选 `queuedMessages`：数组元素包含 `queueId`、`clientMessageId`、`text`、`images`；数组顺序就是 Codex durable queue 顺序。
- 新增请求：
  - `queue.add`：`threadId`、`clientMessageId`、`text`、可选 `images`。
  - `queue.update`：`threadId`、`queueId`、`text`、可选 `images`。
  - `queue.delete`：`threadId`、`queueId`。
  - `queue.reorder`：`threadId`、包含全部当前 queue ID 的 `queueIds`。
  - `queue.steer`：`threadId`、`queueId`、`expectedTurnId`。
- 每个成功响应都返回最新 `queuedMessages`；Web 只接受当前 device/thread 的响应。

## Owner 与一致性边界

1. `add/list/update/delete/reorder` 通过独立 App Server 访问 Codex durable queue；不开放 `thread/queue/start`，因此 Ariel 的 App Server 子进程永远不会开始 turn。
2. 普通新 turn、Steer、停止和交互响应继续只发给真实 Desktop owner。`queue.steer` 先经 owner IPC 确认 Steer，再删除 durable queue 项。
3. 对 queue mutation 禁止自动重试。连接在结果确认前中断时返回 `unknown/OUTCOME_UNKNOWN`，Web 保留草稿或编辑态并要求重新同步；不得猜测成功或重放。
4. `queue.reorder` 必须精确包含当前 queue 的每个 ID 一次。缺失、重复、外来或过期 ID 均拒绝，避免旧 Web 覆盖较新的队列变化。
5. queue item 和 thread ID 必须在操作前后匹配；Steer 必须匹配当前 `expectedTurnId`。active turn 已变化时返回 `STALE_TURN`，排队项保持不变。
6. owner 历史超过普通 IPC frame、切换到 64 MiB large follower 时，租约包装层必须完整透传 `Steer` 与带图片发送能力；不得因包装类型擦除可选 mutation interface 而误报 `PROTOCOL_UNSUPPORTED`。Steer 失败时 durable queue item 必须继续保留。

## 容量与内容边界

- 沿用发送消息的 65,536 字符、最多 3 张 PNG/JPEG 截图和单帧 8 MiB 边界。
- Web 最多渲染 64 个排队项；App Server 分页结果超过边界、重复 ID、空 ID、未知 input 类型或不可还原内容时按 `NATIVE_STATE_UNCERTAIN` 失败，不静默丢字段。
- 第一版只接受 Ariel 已支持的 text 与 data-URI image；原生 queue 中出现 localImage、audio 或结构化 app input 时仍可显示只读摘要，但禁止 Ariel 编辑或 Steer，以免破坏上下文。

## TDD 与验收

1. App Server red tests：精确验证六个原生 queue schema；mutation 不重试；`thread/queue/start` 永远不在 Ariel allowlist；分页、重复 ID、未知 input 与结果未知均 fail closed。
2. Desktop IPC red tests：Steer 只对匹配的 active turn 发一次；构造的 restore message 保留 client message ID、text、image 和 cwd；已完成／变化的 turn 不提交；结果无法确认返回 unknown。
3. Service／协议／Relay red tests：能力位、queuedMessages、五个新 method、完整 reorder、跨 thread queue ID、防重复 client ID、响应和订阅更新均有覆盖。
4. Web red tests：运行中发送变为 queue.add；成功清空、失败保留；拖拽手柄的键盘重排、更多菜单编辑、删除、引导；无 capability 时保持草稿模式；手机宽度无横向溢出并保留可访问名称。
5. 回归：Go race tests、`go vet`、Web tests 和 production build 全部通过；隔离 fixture 已验证 queue add／list／update／delete／reorder，以及 queue item 经 Desktop owner Steer、收到原生 `steeringUserMessage` 确认后再从 durable queue 删除。最后重启本地 Ariel，确认 LAN 页面与 health endpoint。
6. 大会话回归：强制普通 follower 发生 frame overflow 并进入 large follower，确认包装后的 owner 仍可执行 Steer；Web 对未来真实不兼容显示“立即引导不受支持且排队项仍保留”，不得再误写成历史分页失败。

## 实现说明

- Ariel 不自行复制一份队列：Web 展示和编辑的是 Codex durable queue，跨浏览器或 Desktop 的变化会以原生状态为准。
- “引导”通过 Desktop owner 私有 IPC 提交并逐字段确认 `steeringUserMessage`；只有确认成功后才删除对应 durable queue item，避免消息丢失。
- large follower 的租约包装器显式转发 `Steer` 和 `StartWithImages`，避免 Go interface 包装擦除底层扩展能力。
- experimental queue mutation 不自动重试；连接在结果未知时显式返回 `OUTCOME_UNKNOWN`，避免重复排队、误删或旧顺序覆盖新顺序。
- 页面流式更新期间只按 1 秒有界频率回读 queue，读取发生在会话状态锁之外，避免 token streaming 反复阻塞布局与交互。

## 非目标

- 不复制一套 Ariel 自有队列，不用 localStorage 伪造 durable queue。
- 不承诺 experimental method 在未来 Codex 版本保持字段兼容。
- 不把 Steer 描述成撤销当前工具调用或强制中断模型。
- 不通过 Ariel 的独立 App Server 执行 `thread/queue/start`，不创建第二个 owner。

## 官方依据

- [Codex App Server](https://learn.chatgpt.com/docs/app-server)
- [Codex Remote 的 Queue 与 Steer](https://developers.openai.com/blog/mastering-codex-remote-for-engineering)
- [App Server experimental queue protocol](https://github.com/openai/codex/blob/main/codex-rs/app-server-protocol/src/protocol/common.rs)
- [Durable queue service 与 external watcher](https://github.com/openai/codex/blob/main/codex-rs/ext/queue/src/service.rs)
