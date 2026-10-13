# 0076：本地新会话草稿与首条 turn bootstrap

- 状态：Approved（2026-10-13；用户已确认，先执行 compatibility gate，再进入 TDD）。
- 来源：公网 Web 点击“新建会话”后，空 thread 无法由 Codex Desktop 接管，页面长期停留在“正在同步会话”并最终超时。
- 关联：[0033 在指定项目中新建 Codex 会话](0033-create-thread-in-project.md)、[0065 新建会话的 bootstrap 订阅](0065-new-thread-bootstrap-subscription.md)、[0074 新建空会话的 Desktop 接管试验](0074-created-thread-owner-adoption.md)。
- 官方依据：[OpenAI Codex app-server 文档](https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server)要求客户端保存 `thread/start` 返回的 thread ID，在同一执行链路调用 `turn/start`、消费 streaming events，并在后续进程通过 `thread/resume` 恢复。

## 已确认根因

1. 当前 Web 在“创建”时立即发送 `thread.create`。Desktop Agent 启动一次性 App Server，只调用 `thread/start`，取得 ID 后关闭进程，再要求 Codex Desktop 通过 deep link 接管空 thread。
2. 2026-10-13 在 Codex Desktop bundle `0.160.0` 上的隔离真机试验已经证明：无 turn 的 thread 在创建进程关闭后没有进入独立进程可读的持久化索引；创建进程保持存活 45 秒也不能让 Desktop owner 接管。
3. Codex Desktop 自身的新会话也是 `client-new-thread:*` 本地 draft，但当前没有返回 ID、指定 cwd 或让 Ariel 确定关联该 draft 的接口。
4. 因此问题不是 timeout 太短，而是 Ariel 把“创建空 thread”和“获得可订阅 owner”错误地当成了同一事实。继续重试 subscribe、延长 timeout 或再次调用 `thread/start` 都不能修复生命周期错误。

## 方案结论

采用“Web 本地 draft + 首条真实消息原子 bootstrap”：

1. 点击“创建”只在当前 Web 页面建立一个本地 draft，不立即调用 Codex，也不产生 native thread ID。
2. 用户第一次显式发送真实内容时，Desktop Agent 在一个受控 App Server owner 中依次执行一次 `thread/start` 和一次 `turn/start`。
3. App Server owner 在首条 turn 期间保持存活，负责 streaming、approval／user input、精确 interrupt 和状态快照；不能像现有只读 Session 那样丢弃 notification 或拒绝交互请求。
4. 首条 turn 到达终态且独立持久化读回成功后，bootstrap owner 才释放。随后沿现有 Desktop deep-link/follower 链路接管同一 thread；后续 turn 继续由 Desktop owner 执行。
5. 如果当前 Codex 版本无法通过下述兼容性 gate 证明 streaming、交互和 owner 迁移，能力必须报告为 unsupported，停止发布；不得退化为占位消息、标题／时间猜测或无交互的长时间 loading。

## 用户可见行为

### 建立本地 draft

1. 用户选择或输入本机绝对项目目录并点击“创建”后，对话框立即关闭，进入“未命名会话”草稿页；此时不显示“正在同步会话”，输入框立即可用并获得焦点。
2. draft header 显示所选 cwd，并以明确但不打扰输入的文字说明“发送第一条消息后创建 Codex 会话”。
3. draft 只存在于当前已鉴别 Web 页面的有界内存中，不写 Relay、不写浏览器磁盘、不进入会话历史列表缓存，也不声称已经是 Codex thread。
4. 每个 device 在一个 Web 页面内最多保留一个未提交的新会话 draft。再次新建时若现有 draft 含文本或图片，必须让用户明确放弃或返回；不能静默覆盖。
5. 页面刷新、退出登录、设备切换、Agent epoch 改变或该设备离线时，未提交 draft 可以丢失；第一版不建立磁盘级正文缓存。

### 第一次发送

1. Web 新增 `thread.bootstrap` mutation，请求参数为：
   - `cwd`：本地 draft 的项目绝对路径；
   - `clientMessageId`：Web 生成的 UUID；
   - `text` 与可选 `images`：用户第一次真实输入，沿用现有大小、MIME 和图片签名限制。
2. Agent 先执行现有路径 canonicalization；非法、缺失或非目录目标在调用 Codex 前明确拒绝。
3. Agent 只启动一个 App Server，按顺序调用：
   - `thread/start {cwd, ephemeral:false, serviceName:"ariel"}`；
   - 验证返回的 thread ID 与 canonical cwd；
   - `turn/start {threadId, clientUserMessageId, input}`；
   - 验证返回的 exact turn ID。
4. `turn/start` 明确受理后，Agent 把该 App Server owner 安装进对应的有界 `threadController`，再返回 `accepted`，数据包含规范化初始 thread、exact thread ID、turn ID 与 `clientMessageId`。
5. Web 只有在该 accepted receipt 通过身份校验后，才把本地 draft 原子替换为 native thread，清空已确认提交的 composer，并调用现有 `thread.subscribe`。订阅直接连接 bootstrap owner，不执行 Desktop deep link，也不重新调用 `thread/start`。
6. accepted 前发生断线、timeout、Codex 回执畸形或身份不一致时返回 `unknown/OUTCOME_UNKNOWN`；Web 保留原 draft，明确提示先刷新会话列表核对，不自动重试 `thread.bootstrap` 或首条消息。

## Bootstrap owner

### App Server transport

1. 新增独立的读写 App Server session，不能放宽现有只读 `appserver.Session`：
   - 读取 JSON-RPC response、notification 和 server-initiated request；
   - response 继续按 request ID 相关；
   - notification 与 server request 使用有界 channel／队列，不能阻塞 stdout reader；
   - 单帧、pending request、interaction 和累计状态继续有明确容量上限；超限 fail closed。
2. 只允许本规格所需的公开方法和事件。未知 server request 必须可见地报 unsupported 并终止 bootstrap，不能自动批准。
3. App Server 原始 schema、私有字段与 method 名只存在于版本化 Adapter；Relay 与 Web 仍只看 Ariel 的公共协议。

### Live 状态

1. bootstrap owner 必须实现与现有 `Live` 等价的能力：当前权威快照、更新信号、精确 interrupt、interaction respond、连接终止与释放。
2. App Server thread／turn／item 使用专用 normalizer 直接转换为 Ariel `ThreadView`；不得伪造成 Desktop 私有 IPC state 再交给 `NormalizeLive`。
3. 首条 user item 必须保留并暴露 `clientUserMessageId`，供 0069 的发送确认与 unknown 对账使用。
4. 至少处理以下公开事件：`thread/started`、`thread/status/changed`、`turn/started`、`item/started`、`item/updated`、`item/completed`、`item/agentMessage/delta`、`turn/completed` 和 error。Web 继续收到现有 snapshot／item-delta-v1，不新增 token-only 不可恢复流。
5. 每次事件应用必须验证 thread ID、turn ID、item ID 和状态机顺序；未知 ID、倒退状态、重复终态或 seq 缺口使当前 stream 进入 `NATIVE_STATE_UNCERTAIN`，不得继续执行 mutation。

### Approval、补充回答与停止

1. bootstrap owner 必须支持当前 Codex schema 暴露的：
   - command execution approval；
   - file change approval；
   - permission approval；
   - `request_user_input`；
   - 当前 Ariel 已支持的 interaction response decision／answers。
2. 每个 server request 由原 JSON-RPC request ID、thread ID、turn ID 和 item ID 组成稳定 interaction identity；只接受一次精确响应。过期、重复或不匹配响应明确拒绝。
3. approval policy、sandbox、model 与其他执行设置不由 Web 猜测或覆盖；`thread/start` 继承用户当前 Codex 配置。Ariel 必须把 owner 返回的当前设置规范化展示，无法证明时显示未知。
4. `turn.interrupt` 只调用公开 `turn/interrupt`，并携带 exact thread ID／turn ID；终态后返回 already terminal，不停止碰巧开始的其他 turn。
5. bootstrap 期间不开放第二条 `turn.start`、queue、rename、pin 或 archive。composer 可以编辑下一条草稿，但发送入口显示“首条消息处理中”，直到迁移完成。

## 首条 turn 终态与 Desktop 迁移

1. 只在收到匹配 exact turn 的 `turn/completed` 且状态为 `completed`、`failed` 或 `interrupted` 后开始迁移；终态不是成功的同义词，Web 必须保留真实状态／错误。
2. 迁移前通过同一 App Server `thread/read` 核对 thread ID、cwd、首条 turn ID、终态和 `clientUserMessageId`。核对失败时 bootstrap owner 继续保留为只读可见状态，并报告 `NATIVE_STATE_UNCERTAIN`，不能假装已持久化。
3. 核对成功后关闭 bootstrap owner，再由独立 App Server `thread/read` 验证持久化索引可见；随后执行一次现有 Desktop deep link／`OpenFollower`，验证 thread ID 与 cwd 完全一致。
4. Desktop follower 成功后，`threadController` 在锁内原子替换 live owner；现有 subscription 收到连续的 fresh snapshot／update，不清空页面，不丢 composer，不复用旧 owner 的交互请求。
5. Desktop 在迁移 budget 内未接管时：
   - 不重新创建 thread、不重放首条消息；
   - 页面保留最后确认正文，进入只读 stale 状态；
   - 用户可以手动刷新／重新选择以触发现有历史加载；
   - mutation 保持禁用，直到 fresh Desktop owner snapshot 验证成功。
6. Bootstrap owner 与 Desktop owner 永远不得同时接受 mutation。live replacement、subscription baseline 和 owner generation 必须共享 fencing。

## 协议、能力与滚动升级

1. Agent capability 新增 `threadBootstrap`。新 Web 只有在该能力为 supported 时才采用本地 draft；否则隐藏新建入口，不回退到已知失败的空 `thread.create`。
2. `thread.create` 标记为 deprecated compatibility path。新 Web 不再调用；旧 Web 调用新版 Agent 时明确返回 `PROTOCOL_UNSUPPORTED`，避免继续制造不可接管的空 thread。
3. Relay 白名单新增 `thread.bootstrap`，按 mutation 处理：不缓存、不重放、不广播 request body，沿用 request route fencing 和 pending 上限。
4. `thread.bootstrap` receipt 只表示 `thread/start + turn/start` 已由同一 bootstrap owner 精确受理，不表示首条 turn 已完成或 Desktop 已接管。
5. 发布顺序为 Relay／Web → Desktop Agent。Relay／Web 在 capability 缺失时保持现状但隐藏入口；Agent 未更新前不会收到新 method。

## 错误、不变量与资源边界

1. 一次用户点击最多调用一次 `thread/start` 和一次 `turn/start`。任何 unknown 结果都禁止自动重试。
2. `thread/start` 成功但 `turn/start` 明确拒绝时，关闭 App Server；由于空 thread 是否留下不可独立观察，返回 `unknown`，不声称已安全回滚。
3. `turn/start` 可能已受理但 receipt 丢失时，返回 `unknown` 并保留 `clientMessageId`；不发送补偿 turn。
4. 同时最多允许 2 个 bootstrap owner；每个 owner 只服务一个 thread，沿用最多 64 个 controller、每 thread 16 个 subscription、32 个 pending RPC 和现有正文／图片容量上限。
5. Web 本地 draft 不进入 Relay 或 Agent。native thread 创建后，Codex 仍是 thread、正文和执行状态的 SSOT；Ariel 不建立第二套会话数据库或离线 mutation queue。
6. Agent 退出、设备离线或 App Server 崩溃时，Web 收到明确错误并保持最后确认状态；重连只做只读历史发现，不自动恢复或重放未确认 mutation。
7. 不使用占位 prompt、按标题／时间猜 thread、扫描 Codex 数据库、接管 Desktop App Tools pipe、调用未验证私有 create 方法，或把 transport receipt 当作业务受理证据。

## Compatibility gate

### 2026-10-13 实测结果

- 环境：Codex Desktop bundle `0.160.0`，隔离临时 cwd，测试产物不进入仓库。
- 结果：**通过**。同一 App Server 的 `thread/start → turn/start` 返回了可核对的 exact thread／turn／client message identity，并收到了 `thread/started`、`turn/started`、agent message delta 与 `turn/completed`。
- exact interrupt：通过。对隔离的长时间命令调用 `turn/interrupt` 后，同一 turn 以 `interrupted` 终止。
- approval：通过。隔离命令触发一次 server request，明确回复 `decline`；未执行真实副作用。
- `request_user_input`：通过。在 App Server 返回的 Plan collaboration mode 下收到一次问题并提交一次答案；未硬编码 model，而是使用 `model/list` 返回的当前默认 model。
- 持久化与 owner 迁移：通过。终态后，同一 owner 和独立 App Server 均读回 exact thread／cwd／turn／client message；关闭 bootstrap owner 后，Codex Desktop deep link 能接管同一 thread，Ariel owner probe 观测到 snapshot 且无遗留 interaction。
- 隔离 fixture 已全部归档。试验日志只保存在 gitignored `.local/`，不包含用户真实会话。

在写生产实现前，先以隔离临时 cwd 和无副作用 prompt 验证当前 Desktop bundle `0.160.0`：

1. 同一 App Server 完成 `thread/start → turn/start`，取得 exact thread／turn／client message identity，并收到完整 streaming 与 `turn/completed`。
2. 首条 turn in-progress 时可以通过 exact `turn/interrupt` 停止，且没有停止其他 turn。
3. 隔离 approval fixture 能收到 server request、返回明确 deny，并以一致的 interaction identity 在最终 thread 中可核对；不得执行真实命令或写文件。
4. `request_user_input` fixture 能收到问题并提交一次答案；未知／重复回答被拒绝。
5. turn 终态后关闭创建 App Server，独立 App Server `thread/read` 可读回同一 ID／cwd／turn／client message；随后 Desktop follower 可接管同一 thread。
6. 任一项失败即将本规格状态改为 `Compatibility blocked`，保留试验报告，不进入 Web／Relay／Agent 产品实现。

## TDD 与验收

1. Compatibility gate 通过后，先把状态改为 Approved，再按 red → green → refactor 实施。
2. Red：Web 创建按钮只建立 draft，断言零 `thread.create`／`thread.bootstrap` 请求、输入框立即可用；含内容 draft 的替换必须显式确认。
3. Red：第一次发送只发一个 `thread.bootstrap`；accepted 后原子切换 native identity、清空已确认 composer 并订阅；rejected／unknown 保留 draft 且不重试。
4. Red：Relay schema、白名单、timeout、route fencing 与断线 unknown 测试；正文不进入 Relay 状态或日志。
5. Red：读写 App Server transport 覆盖 response／notification／server request 并发、oversize、未知 method、断线、慢消费者和 Close；现有只读 Session 行为保持不变。
6. Red：App Server normalizer 覆盖 streaming message、tool activity、file change、failed／interrupted turn、interaction、client message identity、未知 item 与非法状态迁移。
7. Red：Service 覆盖 create/start 顺序、单次调用、controller 安装先于 receipt、bootstrap subscribe、精确 interrupt／respond、终态持久化核对、owner 原子迁移和失败 stale 门禁。
8. Red：真实 Chromium 手机 viewport 覆盖“创建立即输入 → 首发 → streaming → 终态 → owner 迁移”，正文不白屏、composer 不折叠、输入焦点和后续草稿不丢失。
9. 完成 Go 全包、`go vet`、相关 `-race`、Web 全量单测、production build、完整 Playwright 与隔离真实 Codex compatibility fixture；物理 iPhone／Safari 由用户在部署后复验。

## 发布与回退

1. 发布前记录本机 Codex bundle 版本与 compatibility gate 结果；版本变化时重新运行 gate，不能仅凭 method 存在判断支持。
2. 先部署兼容新旧 Agent 的 Relay／Web，再更新本机 Desktop Agent；公网 Docker 与本机 Agent 都验证后才开放 capability。
3. 回退时先让 Web 隐藏 `threadBootstrap` 入口，再回退 Agent；已持久化的 Codex threads 不删除、不迁移正文。
4. 旧 `thread.create` 的失败实现不作为回退路径。若 bootstrap 不稳定，产品行为是暂时隐藏新建入口，并继续支持已有会话。
