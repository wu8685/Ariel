# 0134 Puck `start_task` / `send_turn` 契约修复

状态：Approved（2026-11-08，用户在第 37 轮明确确认）

## 1. 问题

Puck 的只读工具可用，但 `start_task` 与 `send_turn` 在到达执行主机前返回
`INVALID_ARGUMENT`。当前 dynamic tool registry 只暴露任意 object；handler 又以匿名 Go
结构和无字段详情的错误独立解释参数，造成调用方、Relay 与 Desktop Agent 的契约漂移。

此外，`start_task` 当前转发 `thread.create` 后再发 `turn.start`，而 Desktop Agent 已不支持
空 thread 创建；现行能力是带首条消息的 `thread.bootstrap`。

## 2. 目标

1. `start_task` 与 `send_turn` 使用同一份版本化 canonical contract。
2. dynamic tool registry 暴露字段、required、类型、限制、互斥定位方式、
   `additionalProperties: false` 和合法示例。
3. handler 严格校验，并在 `INVALID_ARGUMENT.details[]` 返回 `field`、`reason`、
   `expected`，不回显输入值。
4. `start_task` 经 `thread.bootstrap` 原子创建执行 thread 和首个 turn；
   `send_turn` 支持 `taskId` 或 `deviceId + threadId` 二选一。
5. policy、幂等、capability 与 toolset version 保持一致；只读工具不回归。

## 3. Canonical 输入

### `start_task`

- required：`deviceId`、`cwd`、`goal`、`prompt`、`riskTags`、`clientTaskId`
- `cwd` 必须是绝对路径；`prompt` 非空且不超过 65536 字符。
- `clientTaskId` 是 UUID，同时作为 bootstrap 的 `clientMessageId`。
- `riskTags` 只能取已知高风险标签，最多 8 个且不得重复。

### `send_turn`

- required：`text`、`riskTags`、`clientMessageId`
- 定位方式严格二选一：仅 `taskId`，或同时提供 `deviceId` 与 `threadId`。
- `clientMessageId` 是 UUID；`text` 非空且不超过 65536 字符。
- direct thread 定位按 B2 进入授权；task 定位复用任务归属。

两者均拒绝未知字段。

## 4. 语义与安全边界

- `start_task` 只在目标设备在线、`threadBootstrap=true` 且
  `executionPolicy=true`（或消费精确用户授权）时执行。
- `send_turn` 仍由 Desktop Agent 的 recent-ID gate 防重复；unknown 不自动重放。
- 项目禁止建立离线 mutation queue，因此设备离线时返回 `DEVICE_OFFLINE`，不得在恢复后
  自动重放。原 Draft 的“离线排队及恢复”验收项由此安全边界替代。
- Puck toolset version 升级；旧 toolset 不会被误报为新契约，下一次用户驱动时轮换 brain
  thread。旧 transport envelope 保持可读。

## 5. TDD 与验收

先提交失败测试，覆盖：

1. contract：schema 可被标准 validator 编译，required/enum/oneOf/额外字段策略正确；
2. validation：空对象、缺字段、错误类型、未知 enum、多余字段、非法 locator、超长文本，
   且每例返回字段详情；
3. adapter：registry 使用 canonical schema，bootstrap/turn 映射无损，policy/capability 一致；
4. E2E：Relay 允许 Puck bootstrap 并保留可信 origin，创建后续 turn、幂等与只读回归。

实现完成后运行相关测试、race、`go vet ./...` 与全量 build。只 push 修复分支；本轮不
merge、不部署、不创建 PR。

