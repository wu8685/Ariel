# 0023：会话截图的发送与展示

- 状态：Implemented（用户于 2026-10-06 要求写 spec 后直接实施；自动／隔离 Desktop 已验，浏览器视觉与物理手机待验）。
- 范围：真实 Desktop 会话中的用户截图输入、用户与 Codex 历史截图展示；手机和桌面 Web 共用实现。
- 依据：本机 Codex 0.160.0 的 `UserInput` 包含 `image`／`localImage`，`ThreadItem` 包含 `imageGeneration`；Codex 回复也可能以 Markdown 图片引用本地截图。

## 输入、输出与行为

1. 输入框旁提供附件按钮和原生文件选择器；允许 PNG/JPEG 截图，最多 3 张、单张和总量均不超过 4 MiB。可仅发截图，亦可附文字；发送前展示可删除的缩略图。空文字、空附件不可发送。手机回车仍只换行，发送必须点击蓝色按钮。发送成功清空本次附件；未提交或结果未知时保留草稿和附件，不自动重试。
2. `turn.start` 可选 `images`（有界 data URI 数组）；Agent 在调用 Desktop 私有 Adapter 前复核 MIME、文件签名、图片尺寸和容量。Adapter 把文字与图片作为同一原生 `UserInput[]` 交给 owner，沿用现有 client message ID 和不确定结果语义。若当前 Codex 版本无法证明原生图片已进入该 turn，应报告 `unknown`，不能假报发送成功。
3. 历史／实时 item 仅携带图片引用元数据，不把图片二进制塞进 thread snapshot、旧页或 Relay 存储。识别原生 `userMessage` 的 `image`／`localImage`、`imageGeneration.savedPath`，及 `agentMessage` 正文中的本地 Markdown 图片引用；远程图片 URL、`fileId` 和未识别格式不自动读取。文本继续采用 0019 的安全 Markdown 规则。
4. Web 对进入视野的原生图片按需请求 `thread.image`（thread/turn/item/index）；Codex 回复里的本地 Markdown 图片须用户点占位后才请求，避免模型文本触发隐式本机文件读取。Agent 只从 Codex 原生 item 核对该引用，再读本机文件或原生 data URI。浏览器不可提交路径。仅返回经过签名与尺寸检查的 PNG/JPEG，单图至多 4 MiB；不访问网络、不跟随 Markdown 远程资源。加载失败时保留可辨识占位和重试按钮，不影响消息、历史分页或其他图片。
5. 图片按所在气泡展示，宽度不超出气泡与屏幕，保持纵横比；点击可放大查看，关闭后回到原阅读位置。预览区域要有可访问名称、加载状态和错误反馈。

## 不变量与错误

- Relay 单帧仍不超过 8 MiB，单次图片响应只含一张；原会话 64 MiB 和默认最近 10 turn 的规则不变。
- 不建图片数据库、不写上传副本、不把图片或绝对路径记入日志；图片来源始终是 Codex 原会话或原文件。切换会话后丢弃旧图片响应。
- 不解析 SVG/HTML/HEIC/GIF，不执行其内容。路径仅能来自核实后的原生 item，拒绝非普通文件、超限和失效文件；远程引用只显示占位。
- 兼容旧客户端／旧消息：`images` 为可选字段；未出现图片时行为保持不变。

## TDD 与验收

1. 先做协议与 Agent 测试：合法／非法图片、容量、原生输入形状、回执核验、从历史与实时原生 item 读取、错误隔离。确认 red 后实现。
2. 再做 Web 测试：文件筛选与预览删除、仅图发送、手机 Enter 不发送、图片加载／失败／放大、Markdown 远程图片仍不自动请求。确认 red 后实现。
3. 运行 Go／Web 全量测试与构建；使用隔离 Desktop fixture 发送一张截图并确认原生 turn 与独立 App Server 历史均可读。真实用户会话不做写测试，物理手机交互留给用户最终验收。

测试记录见 [0023 截图能力验收](../testing/2026-10-06-0023-screenshots.md)。
