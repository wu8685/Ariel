# 0119：Desktop Agent 内置文件上下文，解除 Codex 代理对 Chilling 的依赖

- 状态：Implemented（2026-10-27；与 0117／0118 联合实施，自动化验收通过，未部署）。
- 日期：2026-10-27。
- 范围：Codex 远程会话与文件能力的 Ariel 内部实现、配置兼容、资源与安全边界；不迁移 Chilling 任务业务。
- 关联：0089、0090、0094、0100、0105、0107、0109、0118。
- 优先级：取代 0089／0094／0105 中“文件后端必须由 Chilling 承担”的归属约束；其他用户行为与安全约束继续有效。批准不等于当前部署已升级。
- 证据：[联合测试记录](../testing/2026-10-27-0117-0119-self-contained-files.md)；当前真实 native gate 仅覆盖 metadata／目标 turn items／workspace read／prepare，未新实测含 fileChange 的原生 turn，物理 iPhone／Safari 待用户复验。

## 1. 用户目标与范围

用户只安装／运行 Ariel Desktop Agent，就能远程继续本机 Codex 会话，同时浏览 workspace、查看原文／Diff、打开消息中的文件引用并选段带回会话。不要求另行安装、运行、升级或排障 Chilling；停止或不安装 Chilling 后，上述功能不应降级。

| 能力 | 迁移后的归属 | 保持的行为 |
| --- | --- | --- |
| 会话列表／历史／持续订阅、发送、队列、停止、审批、回答、模型与会话管理 | 既有 Ariel Service + Codex Adapter | 已有能力不重写，Codex 保持 SSOT 与唯一 mutation owner |
| turn 修改文件与 Diff | Desktop Agent 内置 file-context + Codex Adapter | 仅查询已展示 turn，按需读取；不从当前 Git diff 冒充历史 turn diff |
| 消息文件引用、文件原文、目录浏览 | Desktop Agent 内置 file-context | 三种来源校验、当前 workspace 版本、只读、按需加载 |
| 选区准备与引用发送 | 内置 file-context 校验，既有 owner 发送 | 精确原文、SHA／mtime／行范围复验，不自动发送或重放 |
| Markdown 原版／阅读与图片 | Web 展示 + 内置资源读取 | 按 0118，图片不进入会话快照或 hot cache |
| 任务库／MySQL／scheduler／任务生命周期 | 独立 Chilling，可选 | 不搬入 Ariel，不自动启用，也不影响普通 Codex 远程使用 |

“不依赖 Chilling”限定为 Codex 会话／文件代理链路，不宣称独立任务业务也已迁移。任务执行的 native 操作仍复用 Ariel 既有 owner，不能建立第二套 Codex 控制链路。

## 2. 目标架构与代码归属

```text
Web ←→ Relay ←→ Ariel Desktop Agent
                    ├─ Service／Codex Adapter ←→ Codex Desktop／App Server
                    └─ 内置 FileContext
                         ├─ 经 Adapter 获取 thread／turn／item 来源证明
                         └─ 在权威 workspace 边界内读取本机文件
```

1. 将文件 DTO、错误类型、来源验证、路径验证、文本／目录／选区与资源处理放入 Ariel `internal/filecontext`。复用现有 `FileContext` 注入接口，不让业务类型继续归属于 `internal/chilling`；迁移现有行为和测试，而非另做一套宽松实现。
2. Codex 方法与版本化 shape 继续隔离在 `internal/codex/appserver`／既有 Adapter。file-context 接收窄接口，不直接拼私有 IPC、不暴露任意 RPC 方法，不 import Chilling module／internal 包或通过本地 replace 形成构建依赖。
3. 复用 Agent 已有可重建的只读 App Server endpoint；不为文件浏览另起 Chilling 服务、第二个常驻 Codex App Server、per-Web follower 或 WebSocket。发送／停止／审批等仍只由现有 native owner 执行。
4. 文件系统普通只读访问由内置模块执行；thread 权威 cwd、item 身份与文件变更证明必须来自 Adapter。客户端不能指定 root，Relay／Web 不接触本机路径打开或 native 方法名。
5. 正常文件路径不再创建 Chilling client、探测 Unix socket、请求其 capabilities 或做失败回退。故障时不能静默切回旧 backend，避免两份权限规则并存。
6. Chilling task client 仍是独立可选集成。当前 `internal/chilling/task_client.go` 复用了 transport／error 类型，实现时需先分离共用部分，不能删除整个目录破坏任务构建；任务关闭时零连接／零探测。

## 3. 输入、输出与兼容不变量

保留既有公共方法与合法 DTO：`file.context.changes`、`file.context.read`、`file.context.reference.read`、`workspace.files.list`、`workspace.file.read`，以及 turn／queue 输入中的 `fileContexts[]`。新图片方法仅按 0118 协商后使用。

- 输入仍绑定 device／Agent epoch／thread；按来源携带 turn／item／path 或 message reference；输出仍为相对路径、当前文件版本、合法 diff 或目录页、经验证的 context envelope。
- 同一 `FileContext` 内置实例在 Relay transport 重连期间继续存在；Agent 重启／epoch 变化、设备切换、登出后的旧选区和资源 cursor 不可跨身份复用。
- `turn_change` 必须匹配当前 thread／turn 的真实 fileChange item；`message_reference` 必须匹配同一 agentMessage 中的实际引用；`workspace_browser` 必须单独获准且使用该 thread 的权威 cwd。禁止用 basename 猜文件或用另一会话的 cwd 授权。
- 保留正文、空白、行号、软换行、返回路径、Diff 删除文件／截断提示、三类引用 union、canonical envelope 和 draft 清空／保留语义。
- 新旧版本的现有文件方法保持行为契约兼容，不新增未经协商的严格 wire 属性。旧 Web + 新 Agent 不因 provider 改变丢掉入口；新 Web + 旧 Agent 继续按其真实能力运行，不冒充已完成内置迁移。

## 4. 配置与灰度迁移

1. 新的文件开关为 `--enable-file-context`／`ARIEL_ENABLE_FILE_CONTEXT`，默认 false；Relay／Web 与 Agent 都显式启用后才开放。workspace browser 的 Agent 开关为 `--enable-workspace-browser`，默认 false，依赖 file-context；0118 的 `--enable-document-assets` 同样默认 false。
2. 旧 `--enable-chilling-file-context`／`ARIEL_ENABLE_CHILLING_FILE_CONTEXT` 保留为文件开关的弃用别名，含义改为启用内置实现，不再隐含连接外部服务。显式冲突的 CLI 新旧参数启动失败并说明原因；CLI 优先于环境变量；环境变量先取中性名称，缺失时才取旧名，不能绕过显式 false。
3. 旧 `--chilling-endpoint` 可为启动兼容而继续解析，但不连接、不探测，不参与权限判断；仅给一次不含路径／凭据的弃用提示。绝不复用它作为 task endpoint；task 配置与开关不变。
4. 目录浏览权限不因旧文件开关自动扩张。部署时记录旧目标设备实际已启用的目录能力；已确认启用者迁移为新 workspace-browser=true，未确认者保持关闭并报告，不能从“原来装过 Chilling”推断授权。
5. 当前 v1 的 `chillingFileContext`／`chillingMessageReferences`／`chillingWorkspaceBrowser` 严格 capability 字段继续使用，作为历史命名兼容别名，其值取内置功能真实可用状态。不能同时往 hello／device 增加中性字段导致旧 schema 拒绝。可在已允许扩展的 response.data 中附带 provider=desktop-agent 等诊断信息，不影响旧页面。
6. 新 Web 使用中性的配置／UI 文案，兼容旧 HTML meta 的开关值；新 Relay 为长期打开的旧 Web 保留原 meta 注入。兼容字段不是访问授权，Agent 每次仍验证来源与当前开关。
7. 内置失败使用当前 wire 已支持的 FILE_*／PATH_NOT_ALLOWED／REFERENCE_NOT_FOUND／PROTOCOL_UNSUPPORTED／TIMEOUT／OVERLOADED 等错误；不无协商增加 enum，也不再为内置故障生成 CHILLING_UNAVAILABLE。旧 Agent 的旧错误可继续解释，任务错误映射不被误改。
8. 文件关闭或 task manager-only 没有 Codex Service 时，不创建文件源、不探测 Codex 文件能力、不读 workspace，也不声明文件 ready。

## 5. 文件安全与来源证明

1. thread 元信息读取必须为 includeTurns=false；必要的 item 只按目标 turn 分页，复用当前 Adapter 已验证的 legacy item／timeline wrapper shape。不能调用 ReadFull／整仓搜索，不能从热缓存中缺失的详情反推“没有修改文件”。
2. 当前文件内容直接从受限文件句柄有界读取，避免原 backend 的 fs/readFile 全量 base64 返回后才判断大小。文本最多 1 MiB，读到上限＋1 即失败；图像按 0118。特殊文件不打开读取，不能因 FIFO／设备文件卡住。
3. workspace root 来自当前原生 thread metadata，规范化并固定受控根句柄；拒绝 cwd 含糊、root 变化、客户端 root／任意绝对路径、越界、编码混淆、NUL 和协议注入。native 消息中的绝对引用仅能按现有规则归一为同一 root 内相对路径。
4. 保留并加强 symlink／祖先路径／打开期间替换竞争的校验，检查与读取不能仅是 Lstat → 任意路径 ReadFile。采用可证明的根目录约束和 no-follow 打开策略；os.Root 本身不等于“禁止所有 symlink”，必须由攻击 fixture 验证。范围内链接也不自动授予读取权。
5. `.git` 仍可在列表中显示但不可展开或读取；其余 dotfile 继续遵循已批准 workspace 授权，不因此提供任意 home 目录读取。返回错误不包含绝对路径或原始系统异常。
6. 引用发送前重新校验来源、当前 cwd、digest／mtime、原文行范围与精确文本；失败保留草稿和引用，不能只信任浏览器 cache。prepare 成功不等于 Codex 已受理，后续 mutation receipt／unknown 语义不变。
7. 只读不代表来源可绕过：文件、图片、目录和选区均不支持客户端自选 URL、shell、任意 native 方法或通用 filesystem tunnel；不开公网文件服务，不持久化正文、目录、diff、图片或内容日志。

## 6. 性能、容量与失败隔离

- 不改变首屏最近 2 turn／更早最多 3 turn、5 秒 streaming 合并、终态即时更新或 Relay hot cache 行为。文件增强失败不得把已打开会话切为 loading 或影响发送／审批／认证。
- 继续只查当前已展示且缺少结果的 turn。单批最多 10 turn；同一目标 turn 的 items 每页最多 100、最多 32 页，合计保留的来源证明数据最多 8 MiB。超限明确返回 OVERLOADED，不假装空结果或完整摘要；不把 tool output／图片带到 Relay。不能为文件需求扩大现有 native frame 上限。
- 文件来源查询／目录／文本初始读取最多 2 个并发、等待最多 8 项，单操作预算最多 15 秒；超限局部失败。不能持有 Service controller／mutation／transport write 锁等待磁盘或分页；用户取消后停止后续页。共享 App Server 的响应仍可能受 native 耗时影响，不声称消除所有 native 延迟。
- 目录继续单层、100 默认／200 最大页、256 KiB 响应和 revision cursor，不递归预热。排序扫描最多 10,000 条目录项，超过返回容量错误，不能先无限 ReadDir 再截成一页。
- 文件原文、diff、选区与传输预算不放大：1 MiB 文本、64 KiB 单项 diff、5 选区／单个 8 KiB／合计 24 KiB；batch 还受既有完整帧上限限制，截断或超限不能冒充完整结果。
- Web 文本 cache 继续最多 10 文件／8 MiB；目录最多 32 页／2 MiB。图片 lease 与 Web 图片 cache 分别按 0118 上限管理；都不进入 Relay 两 turn cache。
- 不新增轮询，不因目录页或图片打开触发全会话历史刷新。取消／晚到结果继续通过 request／device／epoch／SHA fencing 丢弃，不清空页面上下文。

## 7. Markdown 图片资源内置契约

0118 中原拟交给 Chilling 的本地图片验证、读取和 lease 全部由内置 file-context 承担，取消 Chilling HTTP endpoint／document-assets 开关依赖：

1. `file.context.asset.read` 绑定父文档来源、documentSha256、实际 Markdown image／image-reference 及 imageReference；普通文本、普通链接或 code fence 不授权。相对路径按父目录解析，合法 `../assets` 必须归一后仍在 workspace 内。
2. 仅 PNG／JPEG／GIF／WebP，经签名、MIME 和头部尺寸核对；最大 4 MiB、12 MP、单边 8192px，拒绝 SVG／HTML／绝对本机路径和特殊文件。
3. 原始 chunk ≤256 KiB、完整帧 ≤384 KiB；短时 lease 30 秒，进程图片字节预算 16 MiB、最多 32 项、初始读取最多 2 并发。cursor 不透明并绑定来源、Agent epoch、SHA、顺序 offset 和期限。
4. lease 保留同一份已验证字节，不逐块全图重读、不混版本；新的授权重新验证父文档。压力下 LRU／TTL／明确容量错误，不以 OOM 作控制；进程重启／epoch 变化后失效。
5. 资源过期／变更使用现有 TIMEOUT／FILE_CHANGED，容量使用 OVERLOADED，损坏输入 INVALID_ARGUMENT；response.data 可选 reason 区分局部原因。新方法仍要求 0118 的 Relay 与 Agent 双端协商，旧端未知方法零发送。
6. 本地图片仅流经现有通道；外链由浏览器显式允许后加载，Agent／Relay 不代理外网请求。图片失败不影响文本、文件引用或原有会话。

## 8. TDD、真实环境验证与交付

批准后逐项先 red，实际运行失败，再迁移最小实现并 green／refactor：

1. **无 Chilling 启动 red**：仅 Ariel + 隔离 fake Codex + 临时 workspace；endpoint 缺失或指向无服务 socket，文件 changes → 原文／Diff → message reference → tree → read → prepare 全链可用，断言零 Chilling socket／HTTP 调用、零额外 Codex owner／常驻读取进程。
2. **旧行为与安全 red**：迁入已验证 backend 的语义测试；补 root／cwd 改变、来源伪造、真实链接／替换竞争、FIFO／设备、大文本、编码、重复原文／SHA／mtime 与不自动发送。引用 envelope 和既有消息回执契约不变。
3. **性能 red**：只查目标 turn、32 页／8 MiB 上限、空／坏 cursor、目录扫描上限、队列与并发／deadline／取消、上限＋1 读取；测试文件慢请求期间列表、持续 follower、终态与 mutation 不被 Agent 全局锁阻塞。
4. **配置与版本 red**：flag 默认关闭／显式 false／新旧冲突／旧 meta 与 capabilities；旧 Web + 新 Agent、新 Web + 旧 Agent、新旧 Relay，及文件开关与 tasks manager-only 组合。不能因迁移导致 protocol1008／登录循环。
5. **资源 red**：0118 正常图、引用式图、路径与类型攻击、TTL／cursor／digest／预算／取消与 epoch；断言不请求 Chilling，不把图片／正文写入 Relay cache 或日志。
6. **浏览器**：隔离 WS fixture，320／390／1280px 验证 turn 卡／正文引用／workspace 三入口、原文／阅读／Diff、选区返回与草稿、lazy 图和不撑宽；交互断言和固定截图同时具备。物理 iPhone／Safari 未验证项保留用户复验。
7. **真实 Codex compatibility gate**：只用隔离 fixture 验证当前版本 metadata、目标 turn items 的实际 shape，以及原文／Diff／引用和当前 owner；不向真实用户会话发消息／停止／审批。未经 gate 的能力明确 unverified，不凭方法名声明支持。
8. Ariel Go 全量／race／vet、Web 全量／build／Chromium；task client 既有 fixture 保持通过。编译和 core E2E 不需要 Chilling 仓库、二进制或 socket。补充只读 probe 的结果、版本与未验项到仓库。
9. 实现后同步更新 Ariel AGENTS／README／架构／配置与部署测试，历史规格留存并标注被 0119 替代的归属。Chilling 独立任务数据和未提交改动不迁移、不删除、不重启；旧文件模块可留历史兼容，但 Ariel 不再使用。
10. 发布另按当次明确授权执行：更新 Agent 与 Relay/Web、保留回滚版本／身份／凭据／任务关闭状态；先在隔离环境验证不启动 Chilling，再做生产非变更 smoke。不能为验收直接停止现有 Chilling 或其任务进程，也不能把 Draft 文档当完成交付。
11. Docker 的 Relay build stage 必须包含 `cmd/relay` 的全部本仓库 Go 依赖；以 `go list -deps` 对照该 stage 的路径保留式 COPY 做契约回归，并在真实 Linux Docker 构建中验收。文件开关解析不能导致本机 Go 测试通过而 Docker 缺包；runtime 仍只含 Relay／Web，不加入 Desktop Agent、Codex 或本机状态。2026-10-27 发布前修复属于既有构建与交付要求，不扩展能力或安全边界。

## 9. 非目标

不搬迁任务库、MySQL、scheduler 或持久化状态机；不删除 Chilling 项目／数据；不增加云端 filesystem、数据库或离线 mutation 队列；不绕过 native owner 或把 Relay cache 变为 mutation receipt。0117 排队项紧凑化与 0118 阅读体验已由用户于 2026-10-27 明确联合批准。
