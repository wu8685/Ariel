# Ariel 项目协作约定

## 语言与代码

- 文档和沟通以中文为主，英文技术术语保留原文。
- Relay、Desktop Agent 与兼容性探针优先使用 Go；Web 使用 TypeScript。
- 不硬编码用户名、Codex thread ID、owner ID、PID、socket 路径或凭据。

## SDD

1. 每个组件或能力必须先在 `docs/specs/` 写明输入、输出、行为、不变量、边界条件、错误处理与验收标准。
2. spec 状态为 `Draft` 时不得开始实现；用户明确确认后改为 `Approved`。
3. 改变产品范围、通信语义、状态归属或安全边界时，先更新 spec 并重新确认。
4. `docs/specs/baseline/` 保存最初设计基线；后续规范以 `docs/specs/` 的已批准文档为准。

## TDD

1. 已批准 spec 的每条可执行行为要映射到测试。
2. 严格按 red → green → refactor：先写并实际运行会失败的测试，再写最小实现，最后重构。
3. Mock 测试不能替代真实 Codex Desktop 兼容性验证；所有写操作只允许作用于隔离 fixture。
4. 未验证能力必须报告为 `unverified` 或明确错误，不能根据方法名或字段猜测为 supported。

## 安全与数据边界

- Codex 是会话和执行状态的 SSOT；Ariel 不建立第二套会话数据库、离线队列或正文日志。
- Relay 与 Desktop Agent 均使用有界内存；断线后重建当前视图，不自动重放变更请求。
- Desktop 私有 IPC 只能存在于版本化 Adapter 内；Relay 和 Web 不得接触私有方法名。
- 默认只读探针；发送、停止、审批和回答必须由显式开关启用，并限制到隔离 fixture。
- 不提交 token、Codex 凭据、真实会话内容、未脱敏 IPC 快照或本机绝对路径。
- 历史会话自动加载是用户确认的 MVP 必需能力；Web 点选后不应再要求用户回电脑操作。隔离 fixture 已完成自动加载实测，后续版本升级仍须回归。
- 当前版本补充回答 IPC 对不存在的请求也会返回 `ok: true`。不得将 transport receipt 当作已受理证据；实现交互前先读 `docs/compatibility/2026-10-03-m0.md`。

## Git

- 仓库：`github.com/wu8685/Ariel`。
- 提交身份：`wuke <optimuswu8685@gmail.com>`。
- Ariel 的后续本地 commit 在验证通过后同步 push 到对应远端分支；push 失败时保留本地 commit，报告原因，不使用 force push 覆盖远端历史。
- 此约定只适用于 Ariel，不改变其他仓库（尤其私人知识库）的远端策略。
