# 0075：侧栏会话重命名与原生置顶操作

- 状态：Implemented（2026-10-13；Go／race／真实 Codex fixture／Web／真实移动 Chromium 已验，物理 iPhone/Safari 待用户复验）。
- 来源：用户希望在左侧会话菜单中修改会话名称，并执行置顶／取消置顶。
- 关联：[0036 置顶会话展示](0036-pinned-session-sidebar.md)、[0040 bundled Codex 置顶分区兼容](0040-bundled-codex-pinned-section-compatibility.md)、[0068 侧栏会话操作菜单](0068-sidebar-thread-context-actions.md)、[0071 菜单字体统一](0071-uniform-thread-context-menu-type.md)。
- 范围：Codex App Server 元数据 mutation、Ariel 协议与能力声明、Web 会话菜单和重命名对话框；不建立 Ariel 自有标题／置顶数据库，不改变置顶排序，不提供自定义 section。

## 能力调查结论

1. OpenAI Docs 说明 App Server 通过 JSONL 驱动 thread，并要求客户端保存原生 thread ID；Ariel 继续只用该 ID 调用 Codex-owned mutation，不复制会话实体。
2. 本机 ChatGPT Desktop bundled Codex `0.160.0` 的非 experimental 生成 schema 明确包含：
   - `thread/name/set`，参数为必填的 `{threadId, name}`，响应为空对象；
   - `thread/section/move`，参数为 `{threadId, sectionId, beforeThreadId?}`，其中 `sectionId:null` 表示移出当前 section，响应为空对象。
3. 0040 已验证 bundled Codex 使用保留的 `Pinned` section 保存置顶状态，其 ID 为 `01984de2-8f74-7c91-a3b2-5c5e937cf318`；`thread/list` 与 `thread/read` 会返回原生 section 信息。
4. 0068 只检查了 `thread/metadata/update.isPinned`，因该字段在 `0.160.0` 不存在而关闭置顶写能力；本规格改用同版本公开 schema 中更符合其存储模型的 `thread/section/move`，不调用 Desktop 私有 App Tools，不修改 SQLite／rollout。
5. 两项写能力在实施前都必须通过隔离 fixture 真机探针；schema 存在不等于运行时 mutation 已验证。

## 用户可见行为

1. 继续复用现有会话行长按、右键、`Shift+F10` 与 Menu 键入口；只要 rename、pin、archive 任一能力可用，就允许打开菜单。
2. 菜单顺序固定为：
   - `重命名`；
   - 未置顶显示 `置顶`，已置顶显示 `取消置顶`；
   - `归档`。
   不支持的能力不显示；归档仍沿用既有 idle 门禁。
3. 点击 `重命名` 打开 modal dialog：
   - 输入框预填当前标题；空标题会全选，便于直接输入；
   - 自动聚焦输入框，桌面 Enter 保存、Escape 取消；手机使用显式“取消／保存”按钮；
   - 标题 trim 后必须为 1～200 个 Unicode code points，不允许控制字符；不支持用空字符串恢复自动标题；
   - 保存期间禁用重复提交，原会话内容和草稿保持可见。
4. rename 明确 accepted 后，同时更新侧栏目标行、当前会话 header 和内存历史缓存中的标题；不刷新正文、不重建订阅、不改变当前输入草稿。
5. pin 明确 accepted 后，从 Codex 重新加载当前列表页：
   - 置顶后目标进入顶部“置顶”区；
   - 取消置顶后目标回到 cwd 对应项目，并把该项目展开一次；
   - 当前选中会话、正文、滚动位置和输入草稿保持不变；搜索条件保持不变。
6. mutation 返回 unknown 时不做乐观修改；关闭菜单／dialog，触发一次只读列表刷新，并提示“结果不确定，请检查会话列表”。rejected 则保留可编辑输入并显示明确错误。
7. 同一会话的 rename、pin、archive 共用 busy 门禁；操作期间对应菜单项禁用，其他会话仍可浏览。

## Ariel 协议与能力

1. Agent hello 新增独立 capability：
   - `threadRename`；
   - `threadPin`。
2. 新增显式 mutation：
   - `thread.name.update {threadId, name}`；
   - `thread.pin.update {threadId, pinned}`。
3. Relay 只校验白名单、字段、大小、device/epoch 路由和 mutation timeout，不保存标题或置顶状态。
4. `name` wire limit 为 800 UTF-8 bytes，同时要求 trim 后 1～200 Unicode code points且无 C0/C1 控制字符；Agent 再做同样验证，禁止把任意 App Server 参数透传给客户端。
5. 两项 mutation 都不自动重试。提交前失败返回 rejected；提交后连接中断、deadline 或后置验证失败返回 unknown。

## Desktop Agent 与后置验证

1. 新增 App Server Adapter：
   - rename 调用一次 `thread/name/set {threadId, name}`；
   - pin 调用一次 `thread/section/move {threadId, sectionId: PinnedSectionID}`；
   - unpin 调用一次 `thread/section/move {threadId, sectionId: null}`；
   - 不传 `beforeThreadId`，由 Codex 把新置顶项追加到原生 section 顺序末尾。
2. mutation 前要求目标 thread 已在当前 Agent 的有界已知列表或 controller 中；禁止 Web 对任意猜测 ID 写入。
3. rename 后使用 `thread/read includeTurns:false` 验证返回的 `thread.id` 与 `name` 完全一致；accepted response 返回经 NormalizeStored 生成的目标 thread 元数据。
4. pin/unpin 后使用 `thread/read includeTurns:false` 验证：
   - pin：`section.id == PinnedSectionID`；
   - unpin：`section == nil`。
   随后调用既有 `ListPinned` 做第二重有界验证；超过 100 条置顶导致无法观察目标时返回 unknown，不扩大为无界分页。
5. 写 response 为空对象不能作为成功证据；只有后置读取满足不变量才返回 accepted。
6. rename/pin 不要求 live owner，也不要求 thread idle；它们只改 Codex 持久化元数据，不接触 turn、queue、interaction 或正文。归档的现有 idle 门禁保持不变。
7. 每个 thread 的 metadata mutation 串行，不与同 thread archive 并发；全局最多 2 个 metadata mutation。callback、锁与后置验证都受 Relay/Agent 现有 mutation deadline 约束。

## 缓存与增量视图

1. rename accepted 时按 thread ID patch：
   - `threads` 列表；
   - 当前 `view.thread.title`；
   - Web 页面内有界 `BrowserThreadListCache`；
   - 当前会话 BrowserHistoryCache 的 metadata。
2. 不因 rename 清空 turns、item overrides、history cursor、subscription sequence、composer 或 screenshot drafts。
3. pin/unpin 的列表重载只重建 sidebar projection；不得把 conversation body 置为 loading，也不得重新 subscribe 当前会话。
4. 后续 Codex 原生列表／snapshot 若返回不同标题或 section，以新读取结果覆盖 Web cache，Codex 始终是 SSOT。

## TDD 与验收

1. Red：App Server Adapter 测试覆盖 rename、pin、unpin 的精确方法和参数；断言每项 mutation 只调用一次，response receipt 不直接视为成功。
2. Red：后置验证测试覆盖正确状态、目标缺失、标题不一致、section 不一致、读取失败、deadline 和 response 丢失；不确定路径统一为 `ErrOutcomeUnknown`。
3. Red：Desktop Agent 测试覆盖 capability、known-thread fencing、同 thread 串行、全局并发、rename Unicode/控制字符验证，以及 mutation 不需要 owner/idle。
4. Red：协议与 Relay 测试覆盖两个新方法、额外字段、超长标题、空白标题、非法 pinned 类型、epoch replacement 和 mutation timeout。
5. Red：Web 测试覆盖菜单能力组合、动态置顶文案、rename dialog focus/Enter/Escape、busy 防重、accepted patch、unknown 刷新、搜索态和取消置顶后的项目展开。
6. Red：Web 回归断言 rename/pin 不清空正文、不显示整页 loading、不丢 composer 草稿、不改变当前 subscription。
7. 隔离真机探针创建一条持久化 fixture，依次执行 rename → read、pin → read/ListPinned、unpin → read/ListPinned，确认不新增 turn/item，最后删除 fixture；不得修改真实业务会话。
8. 真实 Chromium 覆盖 390×844 touch 长按菜单、rename dialog、pin 后位置变化，以及桌面右键／键盘路径；截图与行为断言并用。物理 iPhone/Safari 在公网部署后由用户复验。
9. 完成 Go 全量、`go vet`、相关 race、Web 全量、production build 和全部 Playwright 后，才把 spec 标为 Implemented。

## 发布与回退

1. 协议新增 capability，先部署公网 Relay/Web，再更新本机 Desktop Agent；旧 Agent 不声明能力时菜单只保留现有归档。
2. 公网沿用 `www.kan.chat` 的 Docker 部署；本机只更新 Desktop Agent。部署后分别检查公网 health、Agent epoch/capability 和真实只读列表。
3. 回退时先撤回 Agent capability，再回退 Web/Relay；Codex 已完成的 rename/pin 是用户明确触发的原生状态，不自动反向修改。

## 非目标

- 不实现置顶拖拽排序、批量重命名／置顶、自定义 section、删除、归档列表浏览或标题恢复历史。
- 不使用 Codex App Tools native pipe、Desktop 内存、SQLite、rollout 文件或 Ariel 自有数据库作为写入后端。
- 不用本地 optimistic state 冒充 Codex mutation 成功。
