# 0134 Puck mutation tool contract 验证记录

日期：2026-11-08

基线：`3e5790257e9b815fcb9cdbe7c30a62f50d6b0e73`

分支：`fix/puck-start-task-send-turn-contract`

## 根因

1. `internal/desktopagent/brainhost.go` 把全部 Puck dynamic tools 注册成
   `type=object, additionalProperties=true`，Brain 看不到字段、required、互斥 locator、enum
   和示例，因此会在进入远端执行前生成缺字段或错字段参数。
2. `internal/puck/tools/handler.go` 用匿名结构独立解析参数，只返回无字段详情的
   `INVALID_ARGUMENT`；`send_turn` 只接受 `taskId`，与 0130 的
   `taskId | (deviceId,threadId)` 不一致，且 `riskTags` 未进入该工具的 policy 路径。
3. `start_task` 的旧 adapter 映射是 `thread.create` 后 `turn.start`，但 Desktop Agent 对
   `thread.create` 明确返回 method unavailable；已实现且声明的真实能力是原子
   `thread.bootstrap`。
4. Relay 的 Puck 方法白名单未包含 `thread.bootstrap`，也没有 toolset version
   compatibility gate。只改 registry 仍无法完成真实调用。

因此根因不是整体连接，也不只是 schema 展示；它是 registry、handler、adapter、Relay
白名单四层漂移。

## Canonical contract 与三层映射

canonical source 位于 `internal/puck/toolcontract/contract.go`：

- `StartTaskRequest` / `SendTurnRequest`：真实 typed request；
- `DefinitionFor` / `DynamicTools`：Desktop Agent 注册用 schema；
- `DecodeStartTask` / `DecodeSendTurn`：Relay 内 Puck handler 严格校验；
- `Version=2`：brain toolset compatibility gate。

| 层 | 修复前 | 修复后 |
| --- | --- | --- |
| Puck registry / Desktop brain host | 任意 object；无字段信息 | 直接消费 canonical `DynamicTools(version)`；两个 mutation tool 为严格 schema |
| Relay 内 Puck handler / adapter | 匿名结构；无 details；`start_task` 两步错误映射；`send_turn` 只支持 task | typed decode；字段级 details；原子 bootstrap；send 双 locator + policy |
| Relay ↔ Desktop Agent | `thread.bootstrap` 不在 Puck 白名单；无 toolset version | whitelist/write quota/policy gate 均覆盖 bootstrap；既有 `adapterVersion` 携带 toolset version，新 Relay 将其投影到 device status；future/newer contract fail-closed |
| Desktop Agent 消费端 | `thread.create` unavailable；`thread.bootstrap` 与 `turn.start` 有真实消费路径 | start 走 `BootstrapThreadWithExecutionPolicy`；send 走 `StartWithExecutionPolicy`；未验证 `executionPolicy` capability 时仍要求用户授权 |

严格字段：

- `start_task` required：`deviceId:string(1..128)`、`cwd:absolute path(1..4096)`、
  `goal:string(1..512)`、`prompt:string(1..65536)`、`riskTags:enum[](max 8, unique)`、
  `clientTaskId:UUID`；
- `send_turn` required：`text:string(1..65536)`、`riskTags:enum[](max 8, unique)`、
  `clientMessageId:UUID`；locator 必须严格二选一：`taskId` 或
  `deviceId + threadId`；
- 两者均为 `additionalProperties:false`。当前版本不接受 attachments；传入时以字段级错误
  拒绝，不能静默丢弃。

## TDD 证据

Red commit：`8671fb9 test: define Puck mutation tool contract`

首次运行的预期失败包括：canonical API 未定义、`ToolResult.details` 不存在、Desktop registry
仍为旧签名、Relay bootstrap 超时（方法未获准转发）。Green 阶段覆盖：

- contract：schema 可由 Draft 2020-12 validator 编译；required、enum、oneOf、example、
  `additionalProperties`；
- validation：空对象、每个 required 缺失、错误类型、未知/重复 risk tag、额外字段、非法
  UUID、相对 cwd、超长文本、locator 缺半/混用、attachments；
- adapter：bootstrap 参数无损映射、direct/task send、风险授权、capability fail-closed、
  toolset version 宣告与新旧版本拒绝；
- flow：新建 task、幂等重查、第二个 turn、direct existing thread、status、只读工具；设备
  离线时不转发、不排队、不自动重放。

验证结果：

- `go test ./...`：通过；
- `go test -race ./internal/puck/... ./internal/relay ./internal/desktopagent`：通过；
- `go vet ./...`：通过；
- `go build ./cmd/...`：通过；
- Web `vitest`：47 files / 412 tests 通过；
- Web production build：通过，仅有既存 chunk size warning。

## 兼容与边界

- toolset v2 只在新建/轮换 brain thread 上声明严格 schema；旧 v1 thread 不伪装成 v2。
- 已存在的旧 assistant 在下一次用户输入或问题回答前先轮换 brain thread，成功后才继续；旧
  thread 保留只读并 retire。
- 新 Agent 使用旧 Relay 已接受的 `adapterVersion` 字符串承载 toolset version，不向旧严格
  hello schema增加未知字段，因此可安全先升级 Agent。
- 新 Relay 请求 v2、旧 Agent 未声明对应 version 时返回 `PROTOCOL_UNSUPPORTED`。
- 新 Agent 可接受 v1 resume；只读工具与旧 transport envelope 不变。
- 按项目安全边界不实现离线 mutation queue：离线返回 `DEVICE_OFFLINE`，unknown 不重放。

## 发布与回滚建议

受影响组件是 Relay 与 Desktop Agent；Web 业务代码未改，但共享生成类型随协议更新，Relay
镜像构建仍会带上对应 Web asset。

发布顺序：

1. 先发布 Desktop Agent，确认新 PID、ready marker、Relay 握手、`puckToolsetVersion=2`、
   `puckBrainReady` 与 `threadBootstrap`；
2. 再替换 Relay/Web 容器，保留上一容器与 image；验证 health、`/healthz`、Web asset、连接数；
3. 在隔离 fixture 从真实 Puck 会话执行：新建最小 task、第二 turn、已有 thread direct send、
   status、非法字段 details、只读工具回归；不得触碰用户 thread；
4. 对未声明 `executionPolicy` 的目标，预期出现一次授权而不是自动执行。

失败时先回滚 Relay 容器，再回滚 Desktop Agent binary 并重启 launchd；不清数据库、不删除
thread、不自动重放 unknown mutation。回滚会恢复本次修复前的 mutation 缺陷，因此 Puck
`start_task` / `send_turn` 应暂时停用并优先 roll forward；已创建或轮换的 thread 不丢失，
但不得声称旧 Relay 能继续驱动 v2 tool contract。
