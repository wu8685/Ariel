# 0136 Puck 结构化问题与结果卡契约

状态：Implemented（2026-11-08）

## 1. 问题与根因

Puck Brain 的 `ask_user`、`share_result`、`request_authorization` 仍使用任意 object schema。
Brain 因而可能按 Codex `request_user_input` 的习惯发送 `questions[]`。Relay 当前只读取顶层
`text`／`question`，缺失时又把完整 payload 当正文；Web 只读取顶层 `options`。结果是问题卡
显示裸 JSON，且没有可操作选项。相同兜底也会让结果类结构化消息暴露原始 JSON。

## 2. 目标与范围

1. 为三个工具提供 canonical JSON Schema、中文描述和严格服务端解码。
2. `ask_user` 原生采用单题输入，同时兼容既有／错误生成的 `questions[]` 多题输入。
3. 多题归一化为**一张卡中的多个问题**，一次提交整组回答；每个答案保留 question id。
4. 新消息、历史裸 JSON 消息、回答后状态均能稳定渲染，任何失败路径都不显示原始 JSON。
5. 保持 Puck 深色毛玻璃风格，并在 320／390px Chromium 手机 viewport 中无横向溢出。
6. 不改变授权 owner、Codex SSOT、安全策略、Puck 开关与部署边界。

本轮不合并、不部署、不创建 PR，也不修改用户 Codex 配置或其他未跟踪 spec。

## 3. Canonical tool contract

三个 schema 均为 object、拒绝未知字段，并限制字符串、数组数量与总 payload 64 KiB。字段错误
使用现有 `INVALID_ARGUMENT.details[]`（`field`／`reason`／`expected`），不回显输入内容。

### 3.1 `ask_user`

Canonical 单题输入：

```json
{
  "question": "你通常从哪里让 Codex 操作浏览器？",
  "options": [
    {"label": "Codex Desktop", "description": "桌面应用", "value": "desktop"}
  ],
  "allowCustom": true,
  "multiSelect": false
}
```

- `question`：必填非空字符串。
- `options`：必填非空数组；元素含必填 `label`、可选 `description`／`value`。省略 `value`
  时服务端取 `label` 作为稳定提交值。label 可重复时 value 必须不同。
- `allowCustom`、`multiSelect`：可选 boolean，默认 false。
- 为兼容已经生成的格式，顶层也可改为 `questions` 非空数组；每项字段同上，并额外接受
  `id`。单题与 `questions` 严格二选一。
- `questions[]` 缺 id 时由 Relay 生成不透明 id；重复 id、空问题、空 options、非法类型与
  超限输入均拒绝。`allowCustom=true` 时允许 options 为空，以便纯自由输入。

工具描述明确告诉 Brain：一般只问一题；确需用户同时决定多个相关事项时才用 `questions[]`；
不得调用 Codex `request_user_input` 代替本工具。

### 3.2 `share_result`

```json
{
  "summary": "已生成结果",
  "items": [
    {
      "kind": "diff",
      "title": "查看变更",
      "description": "可选简述",
      "deviceId": "device",
      "threadId": "thread",
      "path": "relative/or/absolute/path",
      "url": "https://example.invalid"
    }
  ]
}
```

- `summary` 必填；`items` 为非空数组。
- `kind` 取 `diff | file | pr | thread | url`；`title` 必填；其余定位字段按 kind 校验，
  不允许用不完整定位生成不可点击结果。
- 文件与 diff 仍只保存引用，读取与作用域门禁沿用 0130，不在卡片中持久化原文。

### 3.3 `request_authorization`

```json
{
  "action": "deploy",
  "reason": "需要发布已验证的版本",
  "deviceId": "device",
  "cwd": "/workspace",
  "summary": "部署到测试环境"
}
```

- `action`、`reason` 必填；`deviceId`、`cwd`、`summary` 可选。
- `cwd` 若提供必须为绝对路径；字段用于授权卡显示，不改变策略判断或授权 owner。
- 服务端生成的 authorization id、action hash、规则、过期时间与状态仍由 Relay 持有，Brain
  不能提供或覆盖。

## 4. 归一化与持久形状

### 4.1 问题卡

Relay 将单题与多题统一持久化为：

```json
{
  "questionId": "card-id",
  "questions": [
    {
      "id": "codex_entry",
      "question": "你通常从哪里让 Codex 操作浏览器？",
      "options": [{"label": "Codex Desktop", "value": "Codex Desktop"}],
      "allowCustom": false,
      "multiSelect": false
    }
  ],
  "cardState": "pending"
}
```

`questionId` 是卡片级不透明 id，沿用现有 message id `question-card:<questionId>`。单题也使用
`questions[]`，避免服务端和 Web 出现两套业务逻辑。为滚动升级兼容，新 Relay 在单题卡上同时
镜像顶层 `question`／`options`／`allowCustom`／`multiSelect`，使上一版 Web 仍可显示按钮；新 Web
同时接受规范形状、旧顶层单题形状和历史 `message.text` 中的 JSON。

多题只允许一张 pending 卡；用户一次提交全部必答项。已回答形状增加：

```json
{
  "cardState": "answered",
  "answers": [
    {"questionId": "codex_entry", "values": ["Codex Desktop"], "custom": ""}
  ]
}
```

单选恰好一个 value；多选至少一个 value；`allowCustom` 可提交非空 custom，且可与多选值并存。
服务端按卡内 snapshot 校验 id 与值，拒绝缺题、未知 id、未知 option、重复值和不允许的 custom。

### 4.2 回传 Brain

新 Web 调用：

```json
{
  "assistantId": "assistant",
  "questionId": "card-id",
  "answers": [{"questionId": "codex_entry", "values": ["desktop"]}]
}
```

Relay 以带 question id 的结构化 JSON 放入签名 brain envelope，不拼接含糊的自然语言。为兼容
上一版 Web，仍接受 `{assistantId, questionId, answer}`：仅当目标卡恰有一个问题时转为单值
`answers[]`；多题卡拒绝 legacy answer。transport receipt 仍不当作 Brain 已处理的业务证明。

### 4.3 解析失败与日志

- `ask_user` 在 handler 阶段严格解码失败时不落卡，向 Brain 返回脱敏字段错误。
- 对历史数据库中的 malformed cardJSON／正文 JSON，Web 尝试只读兼容解析；成功则规范渲染，
  失败显示“助手发来了一个无法识别的问题”，绝不显示原始 payload。
- Relay 若收到无法归一化的结构化事件，写一条结构化 WARN（assistant id、event kind、固定
  reason code；不含 payload、thread id 或用户原文），并持久化同一友好文案。
- authorization、share_result、task 的结构化渲染也不得回退为 `string(payload)`；缺字段时按
  kind 显示友好占位，并记录同类脱敏日志。普通文本消息行为不变。

## 5. Web 行为

1. 卡标题为“需要你决定”／“已回答”；每题显示问题标题。
2. option button 以 label 为主文字、description 为小字；长文本可换行，不撑破卡片。
3. `allowCustom` 显示带 label 的输入框；输入内容只在显式提交后发送。
4. 单选使用单选语义；多选使用可切换按钮／checkbox 语义并有独立“提交”按钮。多题始终统一
   “提交回答”，避免第一题点击后提前结束整卡。
5. 回答后禁用交互，逐题显示所选 label 与自定义答案；刷新／历史加载后状态一致。
6. authorization 展示 action、reason、deviceId、cwd；share_result 使用可读摘要和结果项；task
   缺失 title／summary 时显示固定友好文案。所有 URL／path 继续经过现有安全打开路径。
7. 320／390px 下 card、option、输入框、长 cwd 和长 description 不产生页面横向滚动；保留
   现有毛玻璃、深色 token、focus-visible 与最小触控尺寸。

## 6. 版本与兼容性

- Puck `toolcontract.Version` 从 2 升到 3。现有规则会在下一次用户驱动操作时轮换旧 Brain
  thread，使新的 dynamic tool schema 生效；不原地修改已运行 thread 的工具。
- 新 Relay 可读取旧单题 card、历史裸 `questions[]` card 和上一版 Web 的单题 answer。
- 新 Web 可读取旧 Relay 的顶层单题 card；新 Relay 的单题镜像字段保证上一版 Web 可操作。
- 新／旧 Desktop Agent 不新增 transport 字段或 capability gate；Agent 仍只中转版本化 toolset。
- 回滚到上一版 Relay 时，toolset version 3 会按现有 fail-closed 规则报告“Relay 较旧”，不会
  把新 Brain 当成旧契约继续执行。数据库 schema 不变。

## 7. TDD 与验收

严格按 red → green → refactor：

1. Go contract 测试：三个 schema 可编译；required、oneOf、enum、unknown field、边界与描述；
   version 2／3 的定义差异和工具轮换。
2. Go normalization 测试：单题、多题、缺 id、value 默认值、legacy answer、带 id 的多题回答、
   多选／custom 校验、malformed payload 友好兜底与脱敏日志。
3. Web unit：规范／旧顶层／历史正文 JSON、描述、custom、多选、多题、已回答、malformed；
   authorization、share_result、task 不泄露 JSON。
4. Playwright 行为与截图：单题、多题、带 description、已回答；320／390／1280px，至少 390px
   覆盖交互提交，断言无横向溢出。截图只能作为静态基线，提交结果另作行为断言。
5. 浏览器 fixture 使用隔离 WebSocket，不写真实 Puck／Codex 数据。Chromium 手机 viewport／touch
   仅称浏览器验收，物理 iPhone／Safari 留给用户复验。
6. 完整门禁：`go test ./...`、相关 Puck／Relay／Desktop Agent `-race`、`go vet ./...`、全部 Go
   build、`npm test`、`npm run test:ui`、Web production build。

完成后 commit 并 push `fix/puck-ask-user-card-format`；不 merge、不 deploy、不创建 PR。最终报告
根因、改动、兼容性、测试结果、compare URL 与部署建议，不包含密钥、thread id 或用户原始提示。

## 8. 实现与验证记录

- Go：`go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...` 全部通过。
- Web：Vitest 47 files／415 tests、Playwright 186 tests、production build 全部通过。
- Playwright 已覆盖单题、多题、description、已回答和 390px 深色移动 viewport 截图；同时保留
  320／390／1280px 既有回归。物理 iPhone／Safari 未在本轮执行，仍需发布前人工复验。
