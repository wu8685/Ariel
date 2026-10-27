# Ariel 项目协作约定

## 语言与代码

- 文档和沟通以中文为主，英文技术术语保留原文。
- Relay、Desktop Agent 与兼容性探针优先使用 Go；Web 使用 TypeScript。
- 不硬编码用户名、Codex thread ID、owner ID、PID、socket 路径或凭据。

## SDD

1. 每个组件或能力必须先在 `docs/specs/` 写明输入、输出、行为、不变量、边界条件、错误处理与验收标准。
2. spec 状态为 `Draft` 时不得开始实现；用户明确确认后改为 `Approved`。
3. 改变产品范围、通信语义、状态归属或安全边界时，先更新 spec 并重新确认。
4. `docs/specs/baseline/` 保存最初设计基线；后续规范以 `docs/specs/` 的已批准文档为准。

## TDD

1. 已批准 spec 的每条可执行行为要映射到测试。
2. 严格按 red → green → refactor：先写并实际运行会失败的测试，再写最小实现，最后重构。
3. Mock 测试不能替代真实 Codex Desktop 兼容性验证；所有写操作只允许作用于隔离 fixture。
4. 未验证能力必须报告为 `unverified` 或明确错误，不能根据方法名或字段猜测为 supported。

## UI 验收

- 涉及手机布局、遮挡、层叠、尺寸、滚动、streaming 稳定性或 pointer／touch 交互的变更，必须在 jsdom 之外补真实浏览器回归。
- 高风险静态状态使用固定手机 viewport 的截图基线；行为问题同时断言交互结果和关键几何关系，截图不能代替行为断言。
- 浏览器测试默认使用隔离 WebSocket fixture，不向真实 Codex 会话发送、删除、重排或审批内容；真实环境只做不改变业务数据的 smoke check。
- Chromium 手机模拟只能称为真实浏览器 viewport／touch 验收，不能冒充物理 iPhone/Safari。无法完成的物理设备项必须明确保留为待用户复验。

## 本地部署交付

- 每次本地部署或重启成功后，运行 `GO111MODULE=on ./scripts/ariel.sh show-pin`，在最终回复中同时展示当前 LAN 地址和 6 位连接码，不再只提示用户自行查询。
- 连接码只在当次对话回复中展示，不写入仓库文档、测试 fixture、commit message 或其他持久化日志。

## 公网部署

- Ariel 的公网实例位于 `www.kan.chat`，Relay 与 Web 使用 Docker 容器方式部署在该机器上。
- 公网升级应先通过 SSH 登录该机器，再沿用现有 Docker 部署目录和容器配置；不要把公网环境误按本机 `ariel.sh up local` 流程处理。
- 项目配置只记录部署方式和操作边界，不记录 SSH 私钥、口令、token 或其他凭据。当前本机默认 ED25519 key 尚未获得该机器的 SSH 授权；继续部署前需要取得正确的 SSH 用户与私钥路径，或由管理员把本机公钥加入 `authorized_keys`。

## 安全与数据边界

- 0119 已于 2026-10-27 获批：Codex 会话与文件代理链路由 Ariel 自包含；文件原文／Diff／目录／消息引用／选区校验及图片读取归 Desktop Agent 内置 `internal/filecontext`，经既有版本化 Codex Adapter 获取来源证明，复用现有只读 endpoint 与唯一 mutation owner。不得为文件能力重新依赖 Chilling socket／进程，或在 Relay/Web 读取本机路径。0089／0094／0105 中旧 Chilling 后端归属由 0119 取代，其用户行为与安全约束继续有效。
- 文件、workspace browser、document assets 默认关闭，分别显式授权；旧 `chillingFileContext` 等 v1 capability 和旧文件开关仅作兼容别名，不能因改名破坏旧页面。独立任务后端不随文件迁移，不自动启用看板、不删除或重启 Chilling 及其数据。
- 任务看板的 Web／云端代码统一归 Ariel，本地 chilling 承担 MySQL／scheduler／任务持久数据；管理与执行主机分开选择，负责人使用所选执行主机的本地 Codex。0091／chilling 0001 已于 2026-10-17 获准实现，严格按 TDD 与 compatibility gate 推进。
- chilling 是看板任务的唯一 SSOT，不仅是数据库插件；Ariel 只连接／呈现任务和绑定主机 Agent，不复制任务状态机、调度或验收规则。native 事实回流 chilling 后更新任务，native turn 成功不等于任务完成；后端离线不接管任务写入。
- Relay 的主机接入对象是 Ariel Desktop Agent，不是 Codex Agent；本机 Ariel Desktop Agent 再经 Codex Adapter 转发给 Codex。`role=agent`、`Agent(M/E)` 和 `agentEpoch` 均属 Ariel Desktop Agent，不得与任务负责人 Codex 或 ahsir Agent 混用。
- 任务接入不引入 Ariel MySQL／云端 scheduler／持久任务队列。允许必要任务数据有界转发，不持久化正文或记录内容日志；native 写操作仍走执行机既有 owner，后台跨 Agent 路由必须新增授权与核对，不能视为现有 file-context 能力。
- Codex 是会话和执行状态的 SSOT；Ariel 不建立第二套会话数据库、离线队列或正文日志。经 0100 明确批准，Relay 可以在进程内存中保存每个 hot 会话最近两个 turn 的有界只读 cache，用于 Web 通过 Relay-local read／watch 立即展示；Web watcher 不得创建 per-Web Agent follower。cache 不得写入磁盘、数据库、Docker volume、访问日志或 crash dump，也不能作为 mutation 的 owner receipt；所有 mutation 仍由 Desktop Agent 基于当前 hot controller 现场校验。
- Relay 与 Desktop Agent 均使用有界内存；Relay 的双 turn cache 不设固定会话数上限，以 watcher 引用和实际字节预算管理：默认 cache soft budget 512 MiB、`GOMEMLIMIT=768MiB`、容器 hard limit 1 GiB。容量压力必须先淘汰无 watcher LRU 或返回 capacity error，不能把 OOM 当正常控制机制。cache 在 Agent 断线后只能标记 stale、在容器重启后丢失，并由 Agent 重建。任何断线恢复都不得自动重放变更请求。
- Desktop 私有 IPC 只能存在于版本化 Adapter 内；Relay 和 Web 不得接触私有方法名。
- 默认只读探针；发送、停止、审批和回答必须由显式开关启用，并限制到隔离 fixture。
- 不提交 token、Codex 凭据、真实会话内容、未脱敏 IPC 快照或本机绝对路径。
- 历史会话自动加载是用户确认的 MVP 必需能力；Web 点选后不应再要求用户回电脑操作。隔离 fixture 已完成自动加载实测，后续版本升级仍须回归。
- 当前版本补充回答 IPC 对不存在的请求也会返回 `ok: true`。不得将 transport receipt 当作已受理证据；实现交互前先读 `docs/compatibility/2026-10-03-m0.md`。

## Git

- 仓库：`github.com/wu8685/Ariel`。
- 提交身份：`wuke <optimuswu8685@gmail.com>`。
- Ariel 的后续本地 commit 在验证通过后同步 push 到对应远端分支；push 失败时保留本地 commit，报告原因，不使用 force push 覆盖远端历史。
- 此约定只适用于 Ariel，不改变其他仓库（尤其私人知识库）的远端策略。
