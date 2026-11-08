# 0141：Puck 生产工具参数兼容与二进制一致性

- 状态：Implemented（2026-11-09；完整门禁与 Codex 0.160.0 五项隔离预发通过）
- 日期：2026-11-09
- 关联：0136（结构化卡契约）、0140（Brain 稳定性与预发冒烟）

## 1. 问题与范围

生产 Desktop Agent 实际选择 ChatGPT bundle 内的 Codex 0.160.0，而前一轮本地预发使用独立
Codex 0.145.0-alpha.27。生产 `ask_user` 已到达工具链，但返回 `toolSuccess=false`，现有日志没有
参数结构、字段路径或 decoder 原因，Bot 会话也没有可见诊断，最终等待四分钟后回滚。

本轮只在隔离本地 Relay、Agent、Brain 与 Puck DB 中复现和验证；生产仅允许读取脱敏日志或快照。
不部署、不创建 PR、不修改 Codex 配置、其他 Codex thread 或未跟踪的 0106 spec。

## 2. 输入、输出与不变量

### 2.1 拒绝诊断

三个结构化工具解码失败时生成同一份脱敏诊断：

- 工具名；
- 被拒字段路径、稳定 reason 与 expected；
- 参数的递归结构摘要，仅含 object key、array 长度和 JSON 类型；
- 不含 string／number／boolean 的值、用户文本、完整 assistant／thread id 或凭据。

诊断写入 Desktop Agent／Relay 的结构化日志，并通过 Puck Brain status 事件进入 Bot 可见事件流。
日志与事件使用有界大小；畸形或超限 JSON 只能产生固定错误码和顶层类型摘要。

### 2.2 安全归一化

`ask_user`、`share_result`、`request_authorization` 在 canonical strict validation 前只归一化本轮
0.145.0 与 0.160.0 实际观察到的等价形状：

- 字段 alias 映射到唯一 canonical 字段；canonical 与 alias 同时出现且值不等价时拒绝；
- `ask_user` 接受单题／多题 alias、单选／多选的已知字符串或 boolean 表示、字符串 options 与
  对象 options；缺省的 `value`、`id`、选择类型和允许自定义字段仅在无歧义时推断；
- `share_result` 接受已知 summary／items 及 item 定位字段 alias，并把安全可推断的单项形状转为
  canonical items；kind 或定位不明确时拒绝；
- `request_authorization` 接受已知 action／reason／summary／deviceId／cwd alias；不得允许 Brain
  注入 authorization id、hash、规则、状态或过期时间。

归一化结果继续满足 0136 的长度、枚举、绝对路径、唯一值、URL 与定位约束。未知字段、冲突 alias、
不明确类型和不可识别形状仍 fail closed，不能因“宽容”跳过安全校验。

### 2.3 重试与友好降级

第一次不可归一化时，工具结果向 Brain 返回可执行的脱敏错误说明：指出字段路径、原因、期望的
canonical 类型，并要求只重试该结构化工具一次。相同 Brain turn 内同一工具第二次仍无法解码时：

1. 工具结果仍为失败，不伪造成功；
2. Relay 持久化对应 kind 的友好降级卡，说明助手暂时无法生成该交互，请用户用普通消息继续；
3. 卡片不得显示参数 JSON、原始值或内部 id；
4. 不进入四分钟工具超时等待，后续迟到结果按现有 fencing 处理。

一次成功调用或新 Brain turn 会清除该 turn／tool 的失败计数。授权失败降级卡不构成授权，不改变
mutation owner；结果失败降级卡不声称产物存在。

### 2.4 Codex binary 选择与预发一致性

- Desktop Agent 启动时解析一次最终 Codex executable，取得 `--version`，记录所选绝对路径和规范化
  版本；不记录环境变量或完整 argv。路径不可执行或版本不可识别时 fail closed。
- 选择顺序必须可测试，并明确区分显式配置与 ChatGPT bundle 探测；Brain 与普通 Adapter 使用同一
  已解析路径，不能再次通过 PATH 漂移。
- 预发冒烟接收“预期生产 binary”与“实际 smoke binary”，启动前分别解析真实路径和版本；不同则
  默认拒绝运行。只有显式的诊断 override 才可继续并输出醒目告警，正式门禁不得使用 override。
- 冒烟 summary 记录路径的安全 basename／版本与五项耗时，不含凭据、完整 thread id 或提示文本。

## 3. TDD 与验收

1. Red：为 0.145.0 与 0.160.0 实际捕获的参数 shape 建 fixture；先证明现有 decoder、诊断、重试
   降级、启动日志与 version gate 不满足本 spec。
2. Green：实现最小归一化、结构摘要、失败计数／降级卡和 binary identity gate；再重构共享逻辑。
3. 单测必须断言诊断中不存在 fixture 的 string／number／boolean 值、完整 id 与原始 JSON。
4. 完整门禁：`go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`、Web unit、
   Playwright UI、Web production build、`git diff --check`。
5. 门禁后，使用与生产 Desktop Agent 同一个 Codex 0.160.0 executable，运行隔离五项预发冒烟：
   Full Access、每次询问（以后都允许／自动允许／撤销后再询问）、`ask_user`、删除、引用回复；逐项
   记录耗时。任何失败都不得部署或接触生产写路径。

## 4. 交付

提交并 push `fix/puck-ask-user-prod-compat`，不 merge `main`、不建 PR、不部署。最终报告实际被拒
shape 与根因、修复、完整门禁、0.160.0 五项结果和耗时、部署建议与仍需用户决定的事项；不包含
密钥、完整 thread id 或用户原始提示。
