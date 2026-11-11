# 0138 Puck 持久授权规则与 ask 派发可靠性

- **Status**: Approved（2026-11-08；用户在 Turn 50 授权 spec 后直接实现）
- **Scope**: Puck Brain 派发契约、Relay 本地持久规则、授权卡、Bot 设置、隔离 E2E
- **Non-goals**: 部署、合并、PR、跨 Bot／跨主机授权、云端授权数据库

## 1. 背景与前置缺陷

Turn 49 的真实 Brain ask smoke 多次得到 `0 task / 0 authorization`，持久化记录只有普通文本；
确定性 Module 测试则能得到 `0 task / 1 authorization`。因此请求没有到达授权 handler。

代码对比提供以下边界证据：

1. `bbe8581`、`82df9b8` 与当前分支都在 Brain thread 上声明同一组 `dynamicTools`；ask 不会隐藏
   `start_task`。
2. 2026-11-08 调查时 BrainHost 使用 `approvalPolicy=never`、read-only sandbox。2026-11-11 起 brain 改为与 Full Access 执行任务相同的 `never` + `danger-full-access`（0130 §6.1.3）。Bot 的 `permissionMode` 仍不进入 brain 的 turn 参数，只在 Brain 已调用工具后由 Relay policy 和 Desktop execution policy 使用。
3. `82df9b8` 之后的 Brain instruction 一面要求直接调用 Puck dynamic tools，一面禁止“通过 code
   mode 间接调用工具”；但 BrainHost 明确启用 `features.code_mode_host=true`，Puck dynamic tools
   正由该 transport 承载。`bbe8581` 没有这条冲突指令。真实 Full E2E 同样得到 `0 task / 0
   authorization`，进一步排除 ask policy 拒绝，定位为工具调用前的 instruction/transport 冲突。
4. `82df9b8` 后的 toolset 轮换改变 schema/description，但 full 与 ask 使用相同版本。现有 0137
   测试记录还证明，把提示改成“第一动作必须 tool”会让 full 同样退化为 0/0。

结论：不能把问题归因于 ask policy 拦截，也不能靠反复修改自然语言提示碰运气。实现必须保留
Brain 自主规划，同时让“已明确指定目标设备与绝对目录的执行请求”具有可观测、可回归的直接
`start_task` 契约；真实 E2E 必须分别证明 full 和 ask 均实际进入 handler。

## 2. 用户行为

### 2.1 授权卡

pending 卡按顺序显示三个按钮：

1. `拒绝` → `reject`
2. `仅这一次允许` → `approve_once`
3. `以后都允许` → `approve_always`

按钮容器允许换行，每个按钮可收缩但文字不拆分；320 px viewport 不产生横向溢出。

终态文案：

- `approve_once`：`已允许一次`
- `approve_always`：`已允许（以后都允许）`
- 旧卡没有 decision 字段时继续按旧 `cardState` 显示。

### 2.2 自动执行

Bot 为 `ask` 时，待授权动作若命中规则，不生成 authorization row/card，直接执行，并在该 Bot
会话追加一条可见记录：`已按“以后都允许”自动执行：<动作>`。记录只包含动作、主机和规则根
目录，不保存 prompt 正文。

Bot 为 `full` 时仍直接执行，不读取规则、不追加上述记录。

### 2.3 Bot 设置

设置面板列出当前 Bot 的规则：动作类型、主机、目录根和创建时间。owner 可逐条撤销；成功后
立即从列表移除，下一次同作用域动作恢复授权卡。删除 Bot 时同一事务删除其规则。

## 3. 规则模型与匹配

Relay SQLite 新增 `authorization_rules`：

- `id`
- `assistant_id`
- `action_type`
- `device_id`
- `cwd_root`
- `created_at`

唯一键为 `(assistant_id, action_type, device_id, cwd_root)`。规则只存在于 owner 控制的 Relay
状态库，不进入 Desktop Agent、Web localStorage 或对话正文。

### 3.1 服务端规范化

规则作用域只能从 authorization row 中已冻结的服务端动作生成，决定请求只提交
`assistantId + authorizationId + decision`，不得提交或覆盖 action/device/cwd。

- `start_task`：`action_type=start_task`；device/cwd 取严格解码后的有效参数。
- `send_turn`：先以该 Bot 的 task locator 在服务端解析真实 device/cwd，再冻结。
- `request_authorization`：`action_type=request_authorization:<action>`；action 经 trim、长度限制和
  稳定大小写规范化；device/cwd 必须存在且有效。
- 其他受 policy 管理的具体工具：`action_type=<tool name>`，目标同样由 handler/已拥有 task
  在服务端解析。

deviceId 必须为当前已知设备；cwd 必须是 clean 的绝对路径，且不得是 filesystem root。缺失或
不合法时 `approve_always` 返回 `AUTHORIZATION_SCOPE_INVALID`，authorization 保持 pending，
仍可拒绝或仅允许一次。

### 3.2 匹配不变量

规则命中当且仅当：

- Bot id 完全相等；
- action type 完全相等；
- deviceId 完全相等；
- 请求 cwd 等于 `cwd_root` 或位于其真实路径分段下（`/work/a` 不匹配 `/work/ab`）。

不同 Bot、设备、兄弟目录、父目录均不匹配。路径只做词法 clean/分段判断，不跟随符号链接；
实际 workspace 的 canonical/安全检查仍由现有 Desktop Agent 执行，规则不扩大文件能力。

## 4. 决定、撤销与并发

`approve_always` 在同一数据库事务中：校验 pending/未过期/action hash → 解析冻结 scope →
幂等插入规则 → 将 authorization 标记 approved。随后沿用现有一次性 consume 执行冻结动作。
卡片写入 `decision=approve_always` 后显示新终态。

规则列表与撤销使用新方法：

- `puck.authorization.rule.list {assistantId}`
- `puck.authorization.rule.revoke {assistantId, ruleId}`

只接受已通过 Passkey/PIN owner Web 会话进入的 Relay-local Puck 请求；Agent/Brain 不能调用。
撤销 SQL 同时约束 assistant id 和 rule id，跨 Bot id 返回 not found。现有 Passkey 与本人设备
门禁不变。

## 5. ask 派发可靠性

### 5.1 服务端确定性 dispatch

Relay 收到用户消息时，若请求明确同时包含：已知 deviceId、该设备可执行能力、绝对 cwd，以及
需要目标 workspace 实际执行的语义，则在服务端构造规范 `start_task` 参数，直接调用同一 Puck
tool handler，不把首个 dispatch 留给 Brain 自主规划。Full 立即执行；ask 由同一 policy 生成
授权卡；命中持久规则时自动执行。`SourceMessageID` 仍使用用户消息 ID，复用 Turn 49 的幂等键。
设备、目录和动作类型均由服务端解析并在 handler / policy 层重新校验，不能由前端传入冻结 action。

Brain instruction 只禁止本地 shell/exec；必须明确说明已声明的 Puck dynamic tools 由
`code_mode_host` 承载、属于允许且唯一的行动通道，不得再笼统禁止 code mode transport。

不满足确定条件或含图片的请求保持原自主规划路径，不用字符串猜测执行危险动作。Brain 工具集对
`start_task` 明确声明“调用本工具不代表绕过授权；Relay 会在 ask 模式显示卡片”。

### 5.2 证据与诊断

真实 E2E 记录每个 Bot 的 tool-call count、task count、authorization count 和脱敏 assistant
kind；失败时能区分“未调用工具”和“进入 handler 后失败”。测试 Bot 名称不得包含 `ask/full`
等策略提示词，两个模式使用相同用户请求、独立 Relay/Bot/Brain workspace/thread。

bundled Codex 的真实兼容性探针必须另外证明 dynamic tool 确已注册并可调用；明确目标的首个
dispatch 则以真实 Full / ask E2E 验证确定性 handler 路径，不能用 Module mock 冒充通过。

## 6. 协议与兼容

- `puck.authorization.decide.decision` 扩为 `reject | approve_once | approve_always`。
- 新增两个 rule 方法及严格 params schema。
- 旧 Relay 不认识新 decision 时 Web 展示原错误；旧卡和旧 authorization row 可照常读取、决定。
- v3 数据库迁移到 v4；旧数据无规则，行为等价于从未选择“以后都允许”。

## 7. TDD 与验收

### 7.1 Red

1. Store migration、同 Bot/动作/设备/子目录命中与所有负例、幂等创建、跨 Bot 撤销、Bot 删除。
2. `approve_always` 从 frozen action 创建规则；前端伪造字段无效；无效 scope 保持 pending。
3. Relay method/schema 与 owner-only 路由。
4. Web 三按钮、终态、规则列表/撤销、旧卡兼容、320/390 px 无溢出。
5. ask 首次卡 → approve_always → 同作用域第二次无卡且有自动执行记录 → revoke → 再次有卡。

### 7.2 Green / release gate

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- 全部 Go binaries build
- `npm test`
- `npm run test:ui`
- `npm run build`
- 真实隔离 Brain E2E：
  - full：最终 `1 task / 0 authorization`
  - ask：首次 `0 task / 1 authorization`；批准后 `1 task / 1 authorization`
  - always allow 后同作用域下一动作不新增卡；撤销后新增卡

任何真实 E2E 未通过时不得建议部署。
