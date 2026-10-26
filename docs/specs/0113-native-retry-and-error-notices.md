# 0113：原生重试与错误提示

- 状态：Implemented（2026-10-26；用户确认后按 red → green 实现，部署验收另记）。
- Issue：#19。
- 范围：仅修复原生会话 item 的只读投影与显示；不改变网络重试、认证、turn 状态或 mutation 授权。

## 输入与输出

输入为 native turn 的 status、按序 items，以及 `type=error` item 的 `id`、`message`、`willRetry`。已安装 Codex bundle 明确用 `willRetry` 区分可恢复错误与停止重试的错误；不能仅凭 `type=error` 判定 turn 失败。

输出沿用 v1 Item 的 `itemId/role/text`，不新增未协商字段，避免破坏长期打开的旧 Web（0109）。Adapter 输出三个固定、可读的 system text 前缀：`Codex 正在重试：`、`Codex 错误：`、`Codex 状态未确认：`。新 Web 仅将这些前缀用于视觉分类，绝不用于权限、认证或运行状态判断；旧 Web 仍能直接阅读原文。普通 user/assistant 内容和未知 item 的兼容占位不受影响。

## 行为与不变量

1. 同一 turn 的多个 `willRetry=true` 错误仅保留最新一个。只在 `inProgress` 且该错误之后没有确定的原生执行进展或其他错误时显示；恢复进展或 turn terminal 后不显示过期重试提示。
2. 确定进展指非空 agentMessage、已有工具／文件／图片执行 item。reasoning 空占位、sleep、用户输入本身不是恢复证据。只能依据当前 native snapshot，不能用本地 timer 猜测恢复。
3. `willRetry=false` 的错误保留可读原文，不因 turn completed 或后续回复而删除。缺失、null 或非 boolean 的标记归为“状态未确认”，不得擅自当作已恢复或最终失败。
4. `message` 缺失／空白时显示明确占位“原生未提供错误说明”；文本先移除 inline image data，再按 UTF-8 安全截到 4 KiB，不能把 additionalDetails、堆栈或整个 native 对象转发给 Relay。
5. 重试用安静的灰色状态行，最终错误用红色 alert，未知用 warning；保留换行与 literal text，不解析 Markdown／HTML。新 Web 不再同时展示重复“思考中”行。
6. 不增加 follower、轮询、RPC 或正文日志；不新增 Web wire 字段。规范化 item 的结构增删仍走现有即时 cache 发布路径，文本更新仍遵守 5 秒合并窗口。
7. UI 的状态清除只处理显示；无论重试／恢复／失败，都保留用户草稿，不触发自动发送或登录跳转。

## 边界与错误处理

- 不把所有 error 都称为网络问题；原文可表示其他 retryable 原因。
- 独立 turn 不互相合并；真实错误与未知类型不能静默丢失。
- 分段历史只能按当前页里的事实投影；后续完整 cache snapshot 仍为当前展示的权威来源，不建立第二套事件状态机。
- 老 Agent 的 `[error 项目]` 无足够信息，新 Web 不猜测其含义；完整双端更新后由 native 重新投影。

## TDD 与验收

1. Go red：重连提示不再输出 `[error 项目]`；多个 retry 合并；进展／terminal 清除 retry；最终错误、未知标记和未知类型保留；长文本和 inline 图片有界。
2. Web red：三种前缀的视觉语义与可访问角色；普通消息不误分类，原文不作为 HTML；草稿保留。
3. 手机 Chromium：通过隔离 WebSocket fixture 连续发布重试→恢复→最终错误；无需刷新，原文可读、只有一条状态、草稿不丢、无横向溢出。截图与行为断言一起验收。
4. Go 全量／vet／关键包 race、Web unit、production build、全量 Chromium；公网只读 smoke，不向真实业务会话发送测试内容。
5. README 更新后，打版本 tag、push，更新公网 Docker Relay/Web 与本机 launchd Agent；保留回滚容器／二进制、认证凭据和关闭的看板状态。
