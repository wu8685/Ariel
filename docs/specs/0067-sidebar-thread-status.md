# 0067：侧栏会话运行与未读状态

- 状态：Approved（用户于 2026-10-11 确认）
- 来源：用户希望在左侧会话列表中展示 Codex 原生状态：运行中显示旋转 loading，已完成但未查看显示蓝点，查看后不显示标记；同时要求会话数量增加时不得退化为逐会话查询。
- 关联：[0001 M0 兼容性探针](0001-m0-compatibility-probe.md)、[0004 M2 真实 Desktop Agent](0004-m2-real-desktop-agent.md)、[0025 最低版本兼容门禁](0025-minimum-version-compatibility.md)、[0032 左侧会话按项目分组](0032-project-grouped-session-sidebar.md)、[0063 Agent 重连状态 fencing](0063-agent-reconnect-state-fencing.md)、[0068 侧栏会话长按菜单与原生置顶／归档](0068-sidebar-thread-context-actions.md)。
- 范围：macOS Codex Desktop 私有 `codex-app-tools` socket 的版本化 Adapter、Desktop Agent 有界状态缓存、Ariel 协议状态方法和 Web 侧栏标记；不在 Relay 建立会话数据库，不保存会话正文。

> 2026-10-11 实施探针结论：当前 Desktop 为已启动 MCP 子进程分配的 native pipe 不接受 Ariel 独立建立第二条 App Tools client 连接，`tools/list` fail closed。按本规格的兼容性门禁，`threadStatus`／`threadReadState` 暂不上报，详见 [兼容性记录](../compatibility/2026-10-11-sidebar-state-and-actions.md)。

## 已确认事实与接口边界

1. 当前 Codex App 内部 `list_threads` 一次调用会批量返回 recent threads 的 `status`、`isUnread`、`updatedAt`，并同时返回全部 pinned threads；它不是按 thread 调用的接口。2026-10-11 本机只读实测中，当前 Ariel 会话返回 `status: active, isUnread: false`，其他会话可返回 `isUnread: true`。
2. 当前 App 内部 `set_thread_read_state` 可以按一个 thread 更新 read/unread。标记已读是用户实际查看某个会话后才发生的单会话写操作，不用于周期性扫描。
3. `codex-app-tools` MCP 通过 `CODEX_APP_TOOLS_PIPE_PATH` 连接 Codex App 提供的临时 native socket；socket 与 App／executor 生命周期绑定，同一时刻可能存在多个候选。该接口没有出现在 OpenAI 官方 App Server 文档中，属于 Ariel 的版本化私有 Adapter 依赖。
4. 公开 `thread/list`／`thread/status/changed` 只描述调用方 App Server 进程的状态。Ariel 的独立历史读取进程不能据此判断 Desktop 内其他会话是否 active；不得把 `notLoaded` 转换为 idle。
5. 本机经完整 MCP/tool 链路的观测值为：`limit=1` 冷调用约 8.5 秒、约 5.6 KB；`limit=20` 随后的热调用约 1.8 秒、约 16 KB。该耗时包含工具调度，不能冒充 raw socket benchmark，但足以禁止亚秒级轮询。

## 产品行为

1. 侧栏每个会话右侧最多显示一个状态标记，优先级如下：
   - 原生状态为 `active`：显示可访问名称为“正在处理”的旋转 loading icon；
   - 非 active 且 `isUnread: true`：显示可访问名称为“有新结果”的小蓝点；
   - 已读、idle、notLoaded、systemError、未知或状态能力不可用：不显示标记。
2. pinned 区和项目分组中的同一 thread 使用同一状态，不因重复渲染产生不同结果。搜索结果仅在 snapshot 含该 threadId 时显示标记；缺失状态不作推断。
3. 用户选择一个 unread 会话后，只有同时满足下列条件才请求标记已读：
   - 设备、Agent epoch、thread 和 selection generation 仍与点击时一致；
   - `thread.subscribe` 已成功，第一份身份匹配的 live snapshot 已经渲染；
   - 页面当前可见，且该会话仍是选中会话。
4. 标记已读得到明确 accepted 后清除蓝点；rejected、timeout、socket 重连或结果 unknown 时保留蓝点，不自动重试写操作。用户再次查看或手动刷新后可以重新发起新的显式已读请求。
5. 选中会话在页面可见且跟随最新消息时完成，可以在终态 snapshot 渲染后标记已读；页面隐藏、用户正在阅读旧历史或会话已经切换时保留 unread。
6. 状态标记不改变发送、停止、审批、排队、历史分页和 owner 判定。状态能力失败时只移除标记，不让现有会话列表或控制链路失败。

## 私有 socket Adapter

1. 新增独立于公开 App Server reader 和现有 Desktop owner IPC 的版本化 `AppToolsStatusAdapter`。Relay 与 Web 不接触 socket 路径、namespace、私有方法名或原始 App Tool 响应。
2. Adapter 只能发现当前用户、当前 Codex Desktop bundled App Server 的 `codex-app-tools` 子进程：
   - 验证进程 UID、祖先进程、bundled binary 路径和 socket 文件 owner；
   - 不硬编码用户名、PID、随机目录、socket 路径、threadId、turnId 或 namespace；
   - 不把进程环境、socket 路径、标题、摘要、cwd、凭据或原始响应写入日志。
3. 启动或首次请求时发现并验证候选；成功连接后复用一个长连接。正常刷新不得重复扫描进程。只有连接断开、协议错误或 Desktop 重启后才重新发现，并使用有界指数退避。
4. 同时存在多个候选时，Adapter 最多检查 8 个；通过 `tools/list` schema、目标工具存在性和一次只读 fixture／status 请求验证候选。不能仅按最新 PID 或 socket mtime 选择。
5. 每次状态刷新只允许一次 `list_threads` tool call。无论侧栏包含多少会话，都不得再对单个 thread 调用读取接口。Adapter 只保留并输出：
   - `threadId`；
   - 规范化 runtime；
   - 可选 `isUnread`；
   - 不透明的 `updatedAt`／version marker，仅用于同 thread 新旧比较。
6. 响应中的 title、summary、cwd、project、section、ChatGPT conversation 和其他字段在 Agent 内立即丢弃。仅接受 `kind=codex`、目标 `hostId=local` 且 threadId 格式合法的记录。
7. 原生 `active` 规范化为 `inProgress`，`idle` 为 `idle`；`notLoaded`、`systemError`、未知枚举和结构异常统一为 `unknown`。单条畸形记录丢弃；顶层结构或身份异常使整次刷新失败。
8. `set_thread_read_state` 只允许 `read: true`，且 threadId 必须来自当前有效状态 snapshot 并与已经渲染的选中会话一致。Ariel 不提供远程“标记未读”，也不允许任意 App Tool 转发。

## 状态缓存、刷新与容量

1. Desktop Agent 维护可重建的页面状态 snapshot，不成为第二套会话 SSOT：最多 512 条或 1 MiB，以先到者为准；超限时保留 App 返回顺序靠前的记录并显式标记 snapshot 为 truncated。
2. snapshot TTL 为 2 秒；TTL 内的并发读取复用结果。刷新使用 singleflight，同一时刻最多一个 raw socket 请求；触发发生在请求进行中时，只合并为至多一次 trailing refresh。
3. Web 在下列时机立即请求一份批量状态：
   - 首次成功取得非搜索会话列表；
   - 手动刷新、设备切换、Agent epoch 改变或页面从 hidden 恢复为 visible；
   - 新建会话成功并进入列表。
4. Web 已连接、设备在线且页面 visible 时采用自适应轮询：
   - 上一份 snapshot 中存在 active thread：前一次请求结束 5 秒后刷新；
   - 不存在 active thread：前一次请求结束 15 秒后刷新；
   - 页面 hidden、Agent 离线或 Relay 非 ready 时不轮询。
   定时从“请求结束”开始计算，慢请求不得堆积。
5. 当前已订阅会话的 live snapshot／update 可以立即覆盖该 thread 的 runtime；不得为了当前 thread 的每次 streaming update 调用 `list_threads`。终态后只安排一次去抖的批量刷新，以取得权威 unread 状态。
6. Agent 只把规范化状态 snapshot／diff 发给已鉴别 Web；Relay 不缓存、不落盘、不合并 unread。Web 只在页面内保存状态，断线、设备切换或 Agent epoch 改变时清空。
7. App Tool 支持的 non-pinned limit 由兼容性探针确认，最低验收值为 100。状态结果未覆盖已加载的更旧搜索／分页会话时，这些行保持无标记，不进行补充的逐会话查询。

## 协议与错误语义

1. 新增只读 `thread.status.list`：返回 `statuses[]`、`observedAt`、`truncated` 和 capability/source generation；单项仅含 `threadId`、`runtime`、可选 `isUnread`、可选 version marker。
2. 新增显式变更 `thread.readState.update`，首版只接受 `{threadId, read: true, expectedVersion?}`。Relay 仍只做白名单校验和路由；Desktop Agent 负责 selection 证据、状态来源与私有调用。
3. socket 不存在、版本未验证、候选歧义、超时、响应过大、结构异常或 App 重启分别映射为稳定错误；只读状态请求可以安全重新读取，已读写操作在提交后失去回执必须返回 unknown，不能自动重试。
4. capability 明确区分 `threadStatus` 与 `threadReadState`。只读可用但写探针未通过时，可以显示 spinner／蓝点，但 Web 查看不会清除 Codex 原生 unread，并应保持蓝点而不是伪装成功。
5. 全部 raw frame 上限 8 MiB，单次调用期限 10 秒。超限、超时或解析失败立即断开该候选，进入退避；不得把部分列表冒充完整 snapshot。

## 兼容性与安全探针门禁

实现前先在隔离 fixture 上完成以下探针；任一写路径未通过时不得启用 `threadReadState`：

1. 发现至少一个满足 UID、祖先、binary、socket owner 和 `tools/list` schema 的候选，且不依赖固定 PID／随机路径。
2. 对 1、20、100 个 non-pinned limit 分别执行至少 10 次 warm bulk read，记录 p50／p95、payload bytes、超时和 Codex Desktop CPU；证明每轮 raw 调用数恒为 1。
3. 在包含 active、idle、notLoaded 和 unread fixture 的批量响应中，threadId、status、isUnread 与 Codex App 可见状态一致；不得用模型自述或历史 turn 状态代替。
4. 对隔离 fixture 调用 mark read，确认 Codex App 的 unread 消失；对不存在或已切换 thread fail closed。
5. 检查 read 和 mark-read 不向 fixture 新增 turn、message、tool activity、summary 或历史正文；若私有调用污染会话记录则停止实现并报告。
6. 关闭／重启 Codex Desktop、结束承载候选的会话 executor、同时保留多个候选，确认 Adapter 能失效、退避和重新发现，且不影响公开历史和 owner IPC。
7. 探针结果记录到 `docs/compatibility/`，包含 Desktop／bundled Codex 版本和脱敏性能数据，不记录真实会话内容、socket 路径或 PID。

## TDD 与验收

1. Red：Adapter fake process/socket 测试覆盖候选身份验证、最多 8 个、长连接复用、断线重发现、指数退避、8 MiB frame 和不记录敏感字段。
2. Red：批量状态测试放入 100 个 thread，断言一次刷新恰好一次 native `list_threads`，规范化 active／idle／unknown、过滤非 local Codex、丢弃原始正文，并覆盖 512 条／1 MiB 上限。
3. Red：cache/singleflight 测试覆盖 2 秒 TTL、并发复用、慢请求不重叠、一次 trailing refresh、5／15 秒自适应调度及 hidden/offline 停止。
4. Red：协议测试覆盖 `thread.status.list` 只读安全重试和 `thread.readState.update` 的 accepted／rejected／unknown，禁止 `read:false` 和任意私有工具名。
5. Red：Web 测试覆盖 pinned／项目／搜索重复行、spinner 优先于蓝点、unknown 无标记、切换设备和 epoch 清空，以及状态更新不重排会话列表。
6. Red：Web 查看 unread 会话时，订阅回执尚未成功、第一份 snapshot 尚未渲染、页面 hidden 或 selection 已变化均不得 mark read；明确 accepted 后蓝点消失，unknown 后保留。
7. 真实 Chromium fixture 以至少 100 个会话运行 60 秒，验证状态轮询次数符合 5／15 秒边界、无并发请求、交互和滚动流畅；截图覆盖 spinner、蓝点和无标记三态。物理 iPhone／Safari 仍需部署后复验。
8. 运行 Go 全量／race、Web 全量、production build 和全部 Playwright 回归。兼容性探针没有真实通过前，不得把能力标为 supported 或把规格改为 Implemented。

## 降级与回滚

- 私有状态 Adapter 不可用时，现有 `thread.list`、历史、订阅和写操作继续工作；侧栏不显示任何状态标记，也不弹阻塞性错误。
- 新能力由 Agent capability 和版本化 profile 控制，可单独关闭，不要求回滚 Relay／Web 的其他功能。
- Codex 升级后 profile 变为 unverified 时，先运行只读探针；mark-read 写路径必须重新通过 fixture 验证后才能启用。
- OpenAI 官方 App Server 当前只文档化独立 App Server 的初始化、thread/turn 驱动与事件。本规格不把私有 App Tool 行为描述为官方稳定承诺。
