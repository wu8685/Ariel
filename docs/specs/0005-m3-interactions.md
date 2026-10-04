# 0005：M3 原会话交互响应

- 状态：实施中。沿用户已确认的 MVP 范围和 0002 审批语义推进；用户已授权连续开发，实施中遇到无法确认的原生行为必须 fail closed。
- 依据：[0002 审批拒绝语义](0002-approval-denial-semantics.md)、[M0 兼容性报告](../compatibility/2026-10-03-m0.md)、[协议基线](baseline/2026-10-03-codex-remote-protocol.md)。

## 输入、输出和边界

- 输入：Web `interaction.respond` 的 threadId、interactionId、decision、可选 answers；Desktop owner 当前 live snapshot。
- 输出：仅在同一 owner 对同一待处理请求的结果得到足够确认时为 `accepted`；本地过期或变更为 `rejected`；原生回执含糊、断线或竞态为 `unknown`。不自动重试。
- Web 卡片展示原生请求的准确上下文、可用决定和提问约束。自由输入、选项、多问题均可回答；不自动选择。未知类型展示不支持，不提供响应按钮。
- 原生问题若标记 `isSecret`，首版不经手机 Web 采集；只提示回 Desktop 处理，不能显示普通回答按钮。
- 决定只按原生 `availableDecisions` 映射：`decline` → `deny`，`cancel` → `deny_and_stop`。`accept_once` 仅在原生 `accept` 经隔离实测为单次允许后启用；结构化或永久授权不映射。
- 回答前重新获取 owner 状态，核对 thread/cwd/turn/request ID、完整请求内容、决定集合以及 turn 仍在运行。提交后再次读取 owner：user_input 必须精确回显本次 answers；审批至少核对原 request 消失及预期 turn 终态。仅收到 `ok` 不表示成功。
- 多端竞争或请求已被电脑处理时，不以 request 消失推断本次成功。响应中途断线/超时为结果未知，旧卡片失效。
- 原生历史中的 `userInputResponse` item 只有在 `completed=true` 时可显示“已回答补充问题”；等待中的占位或缺少完成证据时，不能提前声称已回答。待处理卡片仍以当前 owner 的 `requests` 为准。
- 命令、文件变更、权限请求按类型分别验证。缺少原生样例或上下文时不能提供允许按钮；不为通过测试而扩大 sandbox 或执行非 fixture 操作。

## 权限请求细化

- `item/permissions/requestApproval` 只展示原生 `reason`、`cwd` 和完整 `permissions` 请求。可用响应限定为拒绝（`permissions: {}`、`scope: turn`）与“仅本轮按原请求授权”（逐字段复制已验证的请求 profile、`scope: turn`）；不开放 session 授权或自行扩大的权限。
- 缺失权限 profile、出现未知字段/形态、请求目录与原会话不符时不提供允许按钮。拒绝仍须绑定完整请求 ID，提交前重新读取 owner；已经过期时不发送。
- 原生 IPC 的 `{ok: true}` 不是完成证据。必须见同一请求从新 owner snapshot 消失，并保留不确定结果提示。真实 fixture 未触发该类型前，只能称协议/单元测试覆盖，不能称权限审批端到端验收完成。

## TDD / 验收

1. 测试 interaction card 的原生方法、ID、decision、选项和自由文本约束；错误/过期/变更拒绝。
2. 测试生产 Follower 响应：再次取快照、严格绑定、native 调用、确认/未知判定，以及并发双客户端。
3. 测试 Web 表单多题与自由输入、禁用未答必填、过期提示；Relay 端到端路由。
4. 隔离 fixture 真机：用户问题选项及自由回答，命令单次允许/拒绝并停止；文件和权限请求若能安全构造，再分别实测。最终以原 owner 和独立持久化核对，不以 UI 自报代替。
