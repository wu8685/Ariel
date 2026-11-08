# Puck brain 首条消息与 Full Access 回归

日期：2026-11-08

基线：`d58da08722578e1ad506df793e12ae7fc486debe`

范围：本地隔离 Relay、Desktop Agent、Puck state、brain workspace 与新建 Codex 线程；线上只读取失败实例日志和 state 证据，不部署、不变更线上状态。

## 结论

失败由两条连续缺陷造成，并非 Full Access 审批卡住：

1. 纯文本消息被 Relay 组装成含 `images: null` 的 `puck.brain.turn.start`。协议只接受省略 `images` 或图片数组，所以请求在 Relay outbound schema gate 被拒绝，未到达 Desktop Agent 和 brain。
2. 该内部发送错误使用 `map[string]string`，Puck response decoder 只识别 `map[string]any`。错误因此丢失，旧 Module 又允许缺少权威 `turnId` 的空回执继续写入运行态消息，表现为停在 acknowledged、没有任何 tool call。

修通传输后还确认了一个独立的兼容性缺陷：bundled Codex app-server 的 `DynamicToolSpec` 要求顶层 `type: "function"`，原 tool registry 未提供该字段。缺字段时 brain 无法可靠注册 `start_task` 等动态工具。

失败实例中的一次冷启动约两分钟，说明 90–150 秒的单一 UI 等待窗口偏紧；但延长超时不能修复上述确定性缺陷。

## TDD 证据

- Red：纯文本首条消息的真实 Relay→Agent 路径被 outbound protocol schema 拒绝；单元测试固定“不得输出 `images: null`”。
- Red：app-server wire-contract 测试确认动态工具缺少 `type: "function"`。
- Red：内部 timeout response 经 decoder 后丢失 `OUTCOME_UNKNOWN`；缺 `turnId` 的 accepted response 被误呈现为运行中。
- Green：纯文本请求省略空图片字段；decoder 同时保留内部与 JSON 解码后的错误对象；accepted brain turn 必须带权威 `turnId`，否则 fail closed。
- Green：工具 registry 输出 app-server function wire contract；brain 指令要求直接调用 Puck tool，禁止经 `exec` 间接调用。

## 真实端到端冒烟

环境使用 ChatGPT Desktop bundled Codex 0.160；每个场景创建隔离 state、Bot、brain workspace 与 Codex 线程。

| 模式 | 预期 | 结果 | 耗时 |
|---|---|---|---:|
| `full` | 首条消息调用 `start_task`；创建 task；无授权卡 | PASS | 多次通过，约 54–179 秒 |
| `ask` | 首条消息调用 `start_task`；只创建授权卡；批准前无 task | PASS | 独立运行约 168 秒 |

另以真实 BrainHost 连续执行两个 turn，均观测到已注册动态工具调用，证明新 schema 可被 bundled app-server 接受，且不是只在首轮成立。

执行 policy 不从 `thread.read.permissions` 反推：该投影描述线程默认设置，不能证明 per-turn override。`full` 的 owner request 由 production IPC 测试精确断言为 `approvalPolicy=never`、`sandboxPolicy.type=dangerFullAccess`，且不携带 user reviewer；真实 E2E 则负责证明该模式不生成授权卡并实际创建任务。

## 发布门禁建议

1. 先等待 `puckBrainReady`、`threadBootstrap`、`executionPolicy` 与 `codexReady` 同时为 true。
2. 新 Bot 先完成 greeting 预热；assistant 文本出现后保留约 10 秒稳定期，并再次连续确认 device ready。
3. mutation transport timeout 建议至少 3 分钟；完整行为门禁建议 6 分钟。任何 `unknown`、缺 `turnId` 或 schema rejection 都应立即失败，不能记作 acknowledged。
4. full 与 ask 必须分别使用隔离 fixture 验证 task/authorization 的互斥结果；不要让 full 创建的真实执行 turn 污染随后的 ask 门禁。只看最终文本不构成通过。
