# 0074：新建空会话在创建端关闭前完成 Desktop 接管

- 状态：Compatibility blocked（2026-10-13；TDD 真机验收否定方案，未进入发布）。
- 来源：用户在公网 Web 每次新建会话后都看到“未命名会话”和正确项目目录，但页面停留在“正在同步会话”，最终提示“操作超时，请重试”。
- 关联：[0033 在指定项目中新建 Codex 会话](0033-create-thread-in-project.md)、[0065 新建会话的 bootstrap 订阅](0065-new-thread-bootstrap-subscription.md)。
- 范围：Desktop Agent 创建空 thread 时的一次性 App Server 生命周期、Desktop owner 接管确认和首个订阅；不改变 Relay、Web 协议，不自动重发 `thread.create`，不创建第二套会话存储。

## 已确认根因

1. 截图中的 thread header 证明 `thread.create` 已取得合法创建回执；失败发生在随后的 owner 接管／首次订阅。
2. Ariel 当前通过一次性 App Server 调用 `thread/start`，验证 `threadId + cwd` 后立即关闭该 App Server；随后 Web 的 `thread.subscribe` 才触发 `codex://threads/<id>`，要求 Codex Desktop 加载并成为 owner。
3. 2026-10-13 在当前 Codex `0.160.0` 上用隔离临时目录实测：
   - `thread/start` 在约 300 ms 内返回合法 ID、canonical cwd 与 idle 状态；
   - 创建端关闭后，独立 App Server 对该 ID 的 `thread/read` 返回 remote rejection；
   - `thread/delete` 同样被拒绝，说明该空 thread 尚未进入独立进程可见的持久化索引；
   - 这与 Web 的两次 bounded subscribe 最终超时一致。
4. OpenAI 官方 App Server 文档要求客户端保存 `thread/start` 返回的 ID，并在进程重启后通过 `thread/resume` 继续；文档没有承诺“尚无 turn 的 thread 在创建端关闭后会立刻被 Desktop 的 `thread/read`／deep link 接管”。Ariel 不能再把合法 start receipt 等同于 Desktop owner 已就绪。
5. 因此主要矛盾不是 timeout 数值太短，而是生命周期顺序错误。单纯延长 Web／Relay timeout 只会让同一失败更晚出现。

## 目标与非目标

### 目标

1. `thread.start` 返回后，在创建端 App Server 仍存活时主动打开 Desktop deep link，并确认原 thread 已由 Desktop owner 接管。
2. 只有在 Desktop owner 的 threadId／cwd 验证通过后，`thread.create` 才返回 `accepted`；此时首次 subscribe 可以直接连接 owner。
3. 创建、接管和首次订阅继续使用有界超时；任何不确定结果都禁止自动重发 create。
4. 保持空会话语义：创建时不伪造用户消息、不启动 turn、不执行模型或工具。

### 非目标

1. 不通过发送占位消息来“强迫持久化”；这会污染会话正文和可能产生模型费用。
2. 不让 Relay 缓存 thread、不增加远端数据库或离线 mutation 队列。
3. 不依赖 Web 针对某个 Codex 版本无限重试或轮询。
4. 不把 private Desktop IPC 方法暴露给 Relay／Web；owner discovery 和 follower verify 仍只存在于版本化 Desktop Adapter。

## 创建与接管事务

1. `ThreadCreator` 打开一次性 App Server，调用稳定的 `thread/start`，参数继续为 canonical cwd、`ephemeral:false` 和 Ariel `serviceName`。
2. 收到结果后先验证 thread ID、cwd 和结构；验证失败返回 `unknown/OUTCOME_UNKNOWN`，不得尝试 deep link。
3. 创建端保持打开，并调用由 Desktop Agent 注入的 `ConfirmOwner` callback：
   - 对该 ID 执行现有 `OpenFollower`；
   - 若 owner 尚不存在，沿现有 `EnsureOwner` 路径调用 `open codex://threads/<id>`；
   - discovery 成功后必须用 follower 验证 threadId 和 expected cwd；
   - 验证成功即关闭临时 follower，但创建端只在 callback 返回后关闭。
4. `ConfirmOwner` 成功后，`Service.CreateThread` 才建立现有 bounded bootstrap hint 并向 Web 返回 accepted receipt。首次 subscribe 继续复用 0065 的 hint，避免等待历史索引传播。
5. `ConfirmOwner` 失败、context deadline、Desktop IPC 断开或身份不匹配时：
   - 创建端关闭；
   - create 返回 `unknown/OUTCOME_UNKNOWN`，因为 native start 是否留下可恢复状态无法安全断言；
   - Web 保留项目路径并提示用户先检查会话列表，不自动重发 create；
   - 不建立 bootstrap hint，不把未验证的 ID 当作 owner。
6. 若测试或非 Desktop 调用方没有注入 `ConfirmOwner`，`ThreadCreator` 保留原始 standalone 行为，便于 App Server 单元测试；Ariel production wiring 必须注入 callback，能力探针也要报告这一接管验证结果。

## 超时与资源边界

1. 创建端、Desktop deep link、owner discovery 和 follower verify 共用原 `thread.create` context，不新增无界 goroutine 或后台重试。
2. Web 50 秒、Relay 45 秒和 App Server 单次 30 秒的现有 mutation budget 暂不放宽；实现应在更窄的 Desktop 接管 budget 内 fail closed。若真实验收证明接管正常但稳定超过预算，再另行调整各层倒挂关系。
3. 同一 create 只启动一个 App Server、调用一次 `thread/start`、打开至多一次 deep link；超时后不得由 Agent 或 Web重发 `thread/start`。
4. callback 返回后必须关闭一次性 App Server 和临时 follower；失败路径也必须释放 pipe、socket 与 controller lease。

## TDD 与验收

1. Red：App Server 单元测试在 `ConfirmOwner` callback 内断言创建 endpoint 尚未关闭；callback 返回后 endpoint 恰好关闭一次。
2. Red：callback 收到与 `thread/start` 完全一致的 thread ID／cwd；callback 失败时 Create 返回 `ErrOutcomeUnknown`，且不重复 start。
3. Red：Desktop Agent wiring 测试断言 production creator 使用 `OpenFollower` 完成 owner/cwd 验证并关闭临时 follower；未确认 owner 不建立 bootstrap hint。
4. Red：隔离真机测试在临时 cwd 创建空 thread，保持创建端直至 Desktop owner 接管；随后关闭创建端，再由独立 App Server `thread/read` 且由 Desktop follower 读取均成功，最后删除 fixture。
5. 保留 Web 现有“create 一次、subscribe 最多重试一次”测试，确认本修复不增加 mutation 重放。
6. 完成 Go 全包、`go vet`、race、Web 全量、production build 和真实 Chromium fixture；公网部署后由物理 iPhone／Safari 复验。

## 发布与回退

1. 协议未变化，公网 Relay／Web 可以保持当前版本；只需升级本机 Desktop Agent。
2. 若当前 Codex 版本无法在创建端存活期间通过 deep link 完成 Desktop owner 接管，则停止实现并把 capability 降为 unsupported，不通过占位 turn 或私有未验证 create 方法绕过。
3. 回退只需恢复旧 Agent binary；Relay、Web、Passkey 数据与既有会话不受影响。

## 2026-10-13 TDD 实施结论

1. Red 测试先固定了“创建 endpoint 必须在 Desktop owner 确认之后关闭”的顺序；最小实现随后通过 App Server 与 Desktop Agent 单元测试。
2. 隔离真机验收仍失败：创建 endpoint 保持存活的完整 45 秒内，`OpenFollower` 无法发现 Desktop owner，最终返回 `OUTCOME_UNKNOWN`。因此失败不是 endpoint 提前关闭造成的竞态。
3. 本机 Desktop bundle 只接受 `codex://threads/<已持久化 UUID>` 作为既有会话 deep link；`codex://threads/new` 可以打开 Desktop 自己的本地 draft，但没有返回 thread ID、指定 cwd 或供独立 Ariel Agent 安全关联该 draft 的接口。
4. Desktop 自身也用 `client-new-thread:*` 表示尚未发送首条消息的本地 draft。这与真机结果一致：当前 Codex `0.160.0` 不把无 turn 的 `thread/start` receipt 作为可由另一 App Server／Desktop owner 接管的持久化会话。
5. 按本规格“无法接管即停止”的发布条件，试验性 `ConfirmOwner` 实现与测试已撤回；没有提交、部署或改变线上行为。继续实现必须另开规格，重新选择以下之一：
   - Codex 暴露可返回 ID 且可指定 cwd 的 Desktop draft/create API 后直接接入；
   - Ariel 把新建页改为本地 draft，并为首条真实消息建立一条可安全处理 streaming、approval、interrupt 与 owner 迁移的 App Server 执行链路。
6. 不采用按标题／时间猜测 `codex://threads/new` 新增项、占位消息、抢占 Desktop App Tools pipe 或扫描本地数据库等不具备确定性或越过进程所有权的方案。
