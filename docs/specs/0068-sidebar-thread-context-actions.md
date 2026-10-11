# 0068：侧栏会话长按菜单与原生置顶／归档

- 状态：Approved（用户于 2026-10-11 确认）
- 来源：用户希望长按侧栏中的单个会话后弹出操作菜单，首版提供置顶／取消置顶和归档，并尽量与 Codex App 的交互保持一致。
- 关联：[0036 置顶会话独立展示](0036-pinned-session-sidebar.md)、[0040 bundled Codex 置顶兼容](0040-bundled-codex-pinned-section-compatibility.md)、[0063 Agent 重连状态 fencing](0063-agent-reconnect-state-fencing.md)、[0067 侧栏会话运行与未读状态](0067-sidebar-thread-status.md)。
- 范围：Web 侧栏交互、Ariel 显式 mutation 协议、Desktop Agent 对官方 Codex App Server 方法的版本化调用；不提供删除、重命名、自定义分区、置顶排序或归档浏览器。

> 2026-10-11 实施探针结论：bundled Codex `0.160.0` 的 public `thread/metadata/update` schema 尚无 `isPinned`，故置顶写能力按 capability fail closed；archive／unarchive 可用 public API 实现。详见 [兼容性记录](../compatibility/2026-10-11-sidebar-state-and-actions.md)。

## 已确认事实与接口选择

1. OpenAI 官方 App Server 文档已把下列方法列入 thread API；其中置顶字段属于比当前 bundled CLI 更新的接口面：
   - `thread/metadata/update`：较新文档描述可更新 `isPinned`，但当前 bundled schema 尚未包含该字段；
   - `thread/archive`：把持久化 thread log 移到 archived sessions，并可能同时归档由它派生且尚未归档的后代；
   - `thread/unarchive`：恢复已归档会话。
2. 本机 Codex Desktop bundled CLI `0.160.0` 的非 experimental 生成 schema 已包含 archive／unarchive；`thread/metadata/update` 只允许 `gitInfo`，不允许 `isPinned`。本版本仅启用公开 App Server 已验证的归档能力，不回退到私有 socket 置顶。
3. Codex App 内部工具面也暴露 `move_thread_to_sidebar_section`、`set_thread_archived` 和 `list_archived_threads`，证明 Desktop UI 具备相同产品能力；但 Ariel 首版不把这些私有 App Tool 作为 mutation 后端。
4. 0036／0040 已能读取并展示 Codex 原生 pinned 状态。0068 只新增写入和交互，不建立 Ariel 自有 pinned／archived 数据。
5. 官方文档明确区分 archive 与 delete；本规格禁止调用 `thread/delete`。归档是可恢复的组织操作，不等于永久删除。

## 用户可见行为

1. 手机／touch 设备在会话行持续按压 500 ms 后打开菜单；手指移动超过 10 px、页面开始滚动、`pointercancel` 或提前抬起均取消长按。
2. 长按成功后抑制紧随其后的 click，不能既打开菜单又切换会话。普通短按仍按现状选择会话，滚动侧栏不得误开菜单。
3. 桌面端支持右键／trackpad secondary click；键盘聚焦会话行后支持 `Shift+F10` 和 Menu 键。首版不额外常驻“更多”按钮，避免挤压现有日期和 0067 状态标记。
4. 菜单针对目标会话显示：
   - 未置顶：`置顶`；
   - 已置顶：`取消置顶`；
   - 非运行中：`归档`；
   - 运行中或运行状态无法可靠确认：`归档`禁用，并说明“会话处理完成后可归档”。
5. 菜单通过 Portal 渲染在 sidebar／backdrop 之上，并限制在 viewport 内；打开后首项获得焦点。`Escape`、点击菜单外、切换设备、Agent epoch 变化或目标会话消失都会关闭菜单并把焦点尽量还给原会话行。
6. 每个 thread 同时最多一个操作。操作进行中该行和菜单项显示 busy，禁止重复提交；其他会话仍可浏览。
7. 置顶成功后该会话进入“置顶”区且保持选中状态；取消置顶成功后回到原项目组，并自动展开该项目一次，避免目标突然不可见。搜索态下保持当前搜索条件。
8. 归档明确 accepted 且经后置读取确认后，从当前列表移除；若它是当前选中会话，则先取消订阅并回到“选择一个会话”状态，不自动选择或启动其他会话。
9. 归档成功后展示 8 秒 `已归档「标题」` toast 和 `撤销`按钮。标题仅来自现有已渲染列表，不从私有响应复制。撤销成功后恢复会话并重新拉取列表；toast 过期或页面重载后不再承诺 Web 内撤销，仍可在 Codex 的归档列表恢复。
10. 因官方 `thread/archive` 可能连带归档派生会话，归档菜单项的辅助说明和成功 toast 均说明“派生会话也可能一并归档”。首版不在未知 descendant 集合上伪报精确数量。

## mutation 与状态语义

1. 新增 `thread.pin.update`，请求只接受 `{threadId, pinned}`。Desktop Agent 只把它翻译为 `thread/metadata/update` 的 `threadId` 与 `isPinned`，禁止透传 `gitInfo` 或其他 metadata。
2. 新增 `thread.archive.update`，首版普通菜单只接受 `{threadId, archived: true}`。只有针对 Agent 内存中尚未过期的成功归档记录，`thread.archive.undo` 才能调用 `thread/unarchive`；Web 不可借此任意恢复未在本次页面归档的 thread。
3. Relay 只做消息白名单、字段／大小校验和目标 Agent 路由，不持久化 pinned、archived 或 undo record。Desktop Agent 是 App Server 写操作的唯一发起方。
4. 所有写请求绑定 device ID、Agent epoch、threadId 和 request ID。epoch 已变化、thread 不在当前有效列表、目标已经归档／消失或 capability 不匹配时 fail closed。
5. mutation 不自动重试。调用前失败返回 rejected；请求可能已提交但连接、deadline 或 response 丢失时返回 unknown。Web 对 unknown 不做乐观状态变更，而是立即触发一次只读列表重建并提示用户先检查状态。
6. 置顶只有同时满足以下证据才返回 accepted：
   - `thread/metadata/update` 成功返回目标 thread 且 `isPinned` 等于请求值；
   - 随后一次有界 `thread/list`／`ListPinned` 读取与目标状态一致。
7. 归档只有 `thread/archive` 成功，且随后有界 `thread/list archived:false` 不再包含目标、`thread/list archived:true` 能找到目标时才返回 accepted。撤销则要求相反的双向后置条件。
8. 后置读取失败或不一致一律返回 unknown，不根据 transport receipt、通知方法名或 Web 本地状态冒充成功。
9. 归档前必须从 0067 权威状态 snapshot 或当前 live owner snapshot 证明目标不是 `inProgress`；证据缺失、过期、`unknown` 或 active 时不发送归档请求。置顶／取消置顶不受运行状态限制。
10. 每种能力单独上报：`threadPin`、`threadArchive`、`threadArchiveUndo`。某项不支持只隐藏或禁用对应菜单项，不阻断会话列表和其他操作。

## 容量、时序与降级

1. Agent 仅保留最多 16 条、最长 15 秒的归档 undo record；每条只含 operation ID、threadId、Agent epoch、过期时间和必要后置验证状态，不保存正文、标题或 cwd。
2. 同一 thread 的 mutation 串行；不同 thread 全局最多 2 个并发 mutation，避免用户连续长按造成 App Server 写入风暴。
3. mutation deadline 为 10 秒；后置验证总 deadline 为 10 秒，读取复用既有有界 history reader，不无限翻页。无法在容量内确认则返回 unknown。
4. 0067 状态能力不可用时，置顶仍可用；归档 fail closed。公开 App Server 某一方法在当前版本缺失时，仅关闭对应 capability，不回退到私有 App Tool 或直接修改 SQLite／JSONL。
5. Web 仅在内存中保存菜单、busy 和 undo toast。刷新、登出、设备切换或 Agent epoch 变化时清空；Codex 仍是最终 SSOT。

## 兼容性与安全探针门禁

所有写探针只作用于专门创建的隔离 fixture；不得用真实工作会话试写：

1. 用当前 bundled CLI 的非 experimental schema 确认 `thread/metadata/update`、`thread/archive`、`thread/unarchive` 及字段形状。
2. 对隔离 fixture 执行 pin → list → unpin → list，确认置顶区和项目区的读取结果与 mutation 一致，且重启 App Server 后仍保留。
3. 对隔离 parent／child fixture 执行 archive，记录 descendant 的实际行为；随后 unarchive 并确认 active／archived 列表和日志文件均恢复到预期位置。
4. 确认 pin、archive、unarchive 不新增 turn、message、tool activity 或 model-visible transcript；只允许预期 metadata／存储位置变化。
5. 构造 App Server 在提交前断开、提交后丢响应、后置读取超时和重复点击场景，验证 rejected／unknown 分界以及零自动重试。
6. 验证 active fixture 的归档在 Ariel 层被拒绝且 App Server 未收到 mutation；idle fixture 才允许提交。
7. 探针结果写入 `docs/compatibility/`，记录 Codex 版本、方法和脱敏结果，不记录真实 thread ID、标题、正文、绝对路径或凭据。

## TDD 与验收

1. Red：App Server client 测试覆盖 pin/unpin 的精确参数、archive/unarchive、方法缺失、deadline 和 response 解码，断言永不调用 `thread/delete`。
2. Red：Desktop Agent 测试覆盖 thread／epoch fencing、active／unknown 归档拒绝、同 thread 串行、全局并发 2、mutation 无自动重试、后置条件和 accepted／rejected／unknown。
3. Red：undo record 测试覆盖 16 条／15 秒上限、一次性使用、跨 epoch 失效、目标不匹配和成功恢复。
4. Red：协议测试覆盖三个显式方法，拒绝额外 metadata、任意 archived:false、伪造 undo operation ID、超长 threadId 和未知字段。
5. Red：Web 单测覆盖 500 ms 长按、10 px 移动／scroll／pointercancel 取消、短按不受影响、长按后 click 抑制、右键和键盘入口。
6. Red：Web 单测覆盖 menu focus／Escape／outside click、Portal 层级、动态置顶文案、active／unknown 禁用归档、busy 防重和 epoch 清理。
7. Red：Web 状态测试覆盖 pin 后移动且不重复、unpin 后项目展开、archive 后 selected 清理、accepted 的 8 秒 undo、unknown 不乐观移除和搜索态保持。
8. 真实 Chromium 在 390×844 touch viewport 验证长按不误选、侧栏滚动不误触、菜单不越界、不被 backdrop 或 composer 遮挡；截图覆盖普通、置顶、运行中禁用归档和 undo toast。
9. 真实桌面 Chromium 验证右键、`Shift+F10`、焦点恢复和键盘执行。物理 iPhone／Safari 仍需公网部署后复验。
10. 运行 Go 全量／race、`go vet`、Web 全量、production build 和全部 Playwright。隔离写探针未通过前，capability 必须为 unsupported，规格不得改为 Implemented。

## 非目标与回滚

- 不提供删除会话；任何层都不得暴露或调用 `thread/delete`。
- 不在首版提供归档列表浏览、批量操作、自定义 section、拖拽置顶排序或 rename。
- 不通过私有 App Tool、SQLite 或文件移动兜底官方 App Server mutation；官方能力失效时宁可隐藏操作。
- 0068 可按 capability 独立关闭；关闭后恢复 0036／0040 的只读置顶展示，不影响 0067 状态标记、历史、订阅、发送和队列。
