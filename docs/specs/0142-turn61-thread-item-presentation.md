# 0142：Codex 0.160 会话条目兼容与 greeting 参数核验

- 状态：Approved（2026-11-09，Turn 61 用户明确授权修复并发布）
- 影响范围：Desktop Agent 历史／live normalize、Web 普通会话展示、Puck Brain greeting 参数核验
- 不影响范围：数据库 schema、Codex 原始历史、Puck 消息卡协议、Chilling、用户现有线程写操作
- 关联：[0015 工具活动折叠](0015-compact-tool-activity-in-conversation.md)、[0098 用户输入卡](0098-conversation-user-input-cards.md)、[0109 Web wire 向后兼容](0109-web-wire-backward-compatibility.md)、[0140 Brain 稳定性](0140-turn55-brain-stability-and-preflight-smoke.md)

## 1. 问题与证据

Codex Desktop 0.160 的私有 follower 投影出现公开 app-server `ThreadItem` schema 之外的条目。Ariel Desktop Agent 当前对未知类型统一生成 `[type 项目]`，Relay 原样透传，Web 再把它作为普通 system message 展示；因此历史线程和 live thread 都会暴露占位文本。

本机 0.160 静态取证确认：

- `automaticApprovalReview` 含 `status`、`action`、`rationale` 等字段；`action.type` 已观察到 `command`、`execve`、`writeStdin`、`applyPatch`、`networkAccess`、`mcpToolCall`、`requestPermissions`。
- `steered` 只携带稳定 `id`，用于标记输入已插入 active turn；实际用户内容位于相邻 `userMessage` 或 `steeringUserMessage`。
- `request_user_input` 的用户回答可被编码为单个 text input：`<send_user_message_question_reply>` 包裹单个对象或数组，每项字段为 `questionItemId`、`question`、`answer`。
- 公开 app-server schema 还包含 Ariel 未显式处理的 `hookPrompt`、`functionCallOutput`、`dynamicToolCall`、`enteredReviewMode`、`exitedReviewMode`、`contextCompaction`。Desktop 私有投影另有 `todo-list`、`planImplementation`、`autoReviewInterruptionWarning`、`strictReviewNotice`、`remoteTaskCreated`、`personalityChanged`、`forkedFromConversation`、`modelChanged`、`modelRerouted`、`mcpServerElicitation`、`permissionRequest`、`worktreeInit` 等。

官方 OpenAI 文档只公开 app-server 的 initialize、thread/start、turn/start 与 item event 生命周期，没有承诺 Desktop 私有 follower 条目；私有类型按精确本机版本证据处理，未观察字段不得猜测。

## 2. normalize 契约

### 2.1 问答回传

1. 仅当 text 去除首尾空白后完整匹配 opening／closing tag 时解析；支持单对象和非空数组。
2. 每项必须有非空 `questionItemId`、`question`、`answer`；数量、单字段和总输出有界。
3. 成功时仍投影为 user item，但 `text` 改为安全 Markdown：每题以引用／小字问题开头，下一段显示“你的回答”；不得保留 XML、JSON、内部 request ID。
4. 检测到 opening 或 closing tag 但解析失败时，投影为 user item 的友好说明“已提交补充回答，但当前格式暂不支持显示”；不得回退原文。
5. 普通 user text 不受影响。历史与 live 使用同一实现。

### 2.2 自动审批

1. `automaticApprovalReview` 只提取可验证的 `status` 与 `action.type`，不转发原始 action、command、路径、rationale 或其他 payload。
2. `approved` 显示“已自动审批”；其他状态显示中性的“自动审批检查”，不得把 denied／timedOut 误写为已批准。
3. 连续 `automaticApprovalReview` 合并为一个 system item。单项标题带安全类别；多项标题为“已自动审批 N 项”或“自动审批检查 N 项”。展开摘要逐项只显示类别与状态。
4. 类别映射：command／execve／writeStdin → 命令，applyPatch → 文件，networkAccess → 网络，mcpToolCall → 工具，requestPermissions → 权限，其他 → 请求。

### 2.3 steering 与其他条目

1. `steered` 投影为一行“已插入引导”，不显示原始类型名或 payload；相邻多条允许 Web 合并展示。
2. `steeringUserMessage` 继续按已接受的 user message 展示；pending 不进入历史正文。
3. 已知但不需要正文的 lifecycle 条目可显示安全的一行提示或静默省略，但不得显示 `[type 项目]`。
4. 任何真正未知类型投影为 system item：标题“暂不支持显示的内容”，可展开只查看有界、转义后的类型名；原始 payload 永不进入公共 item。
5. 不新增 v1 item 字段。旧 Web 最多显示友好纯文本；新版 Web 根据 Adapter 自有的固定前缀渲染折叠提示，保持 0109 的严格兼容边界。

## 3. Web 展示

1. 自动审批使用低对比度单行 `<details>`；多项只占一行，展开后显示安全摘要。
2. `steered` 使用低对比度一行提示，不使用普通引用卡样式。
3. 问答回传沿用 user bubble；问题使用 blockquote／小字样式，回答保持正文层级。
4. 未知类型使用折叠的一行“暂不支持显示的内容”，展开后只显示“类型：…”。
5. 页面不得出现 `[xxx 项目]`、`send_user_message_question_reply`、原始 JSON 或内部 question item ID。
6. 0098 的 pending `request_user_input` 卡片、选项、自由文本、多题一次提交和 owner exact echo 语义保持不变。

## 4. greeting 核验

1. 0.160 生成 schema 中 `turn/start.effort` 仍为正式字段；Ariel 必须证明 `puck.create → puck.brain.turn.start → Desktop Agent → app-server turn/start` 全链路保留 `low`。
2. 测试同时记录 Brain model、已确认 thread settings、greeting prompt 的有界长度与模型生成阶段耗时；不输出 prompt 正文或线程 ID。
3. 若发现字段丢失、被 collaboration mode 覆盖或 prompt 非预期增长，做最小修复并增加回归。
4. 若参数已正确生效且慢点完全位于 provider/model generation，不修改模型或产品语义；报告证据、波动范围和后续建议。

## 5. TDD 与验收

### Red

1. Go tests：自动审批单项／连续合并／非 approved、steered、合法问答、非法问答、公开 schema 新类型、真正未知类型。
2. Web tests：自动审批折叠、steered 小提示、问答 blockquote、未知类型折叠，断言无旧占位和原始标记。
3. greeting tests：从 Relay 输入到 app-server `turn/start` 的 `effort=low` 精确传递。

### Green / Refactor

实现最小 normalize 与展示组件；复用历史／live 公共路径。把文本约定集中到单一 helper，避免散落字符串判断。

### 门禁

1. `go build ./...`、`go vet ./...`、`go test ./...`、`npm test`、Web production build、`git diff --check`。
2. Codex 0.160 隔离 Brain 五场景；普通会话隔离 fixture 覆盖自动审批、steered、问答与未知类型。
3. 生产 bundle 普通会话检查无 `[xxx 项目]` 与原始 XML／JSON；物理 iPhone/Safari 若未完成，明确保留用户复验。

## 6. 实证记录（2026-11-09）

- Codex 0.160 隔离 Brain：Full Access、ask authorization、ask_user、引用回复、删除临时 Bot 五场景通过；两次 greeting 分别为 12.022s、8.011s，`turn/start` 均记录 `effort=low`，prompt 为 325 bytes。
- 普通会话 production preview fixture 在 320／390／1280 viewport 通过；request_user_input 既有手机浏览器回归通过。
- 全量 Playwright 的 181 项通过；9 项既有 Puck 截图只在共同的标题文字上出现相同 14-pixel raster 差异，行为与几何断言通过，单 worker 重跑仍为同一差异。该差异不在本次普通会话 DOM／CSS 影响范围内，因此未改动既有 Puck 基线。
