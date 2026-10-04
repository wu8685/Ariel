# 0013 分页 API 只读复核（2026-10-05）

## 范围

- 使用当前 Desktop 内置 Codex CLI `0.160.0` 的独立 App Server，仅对 Ariel 隔离 fixture 调用 `thread/read` 与 `thread/turns/list`；`initialize.capabilities.experimentalApi=true`。
- 输出只保留结构、计数和大小，不打印或保存 thread ID、工作目录、消息正文和 cursor。未发送消息，也未触碰业务会话。

## 观察

1. 普通双 turn fixture 中，`itemsView: summary` 与 `full` 都返回完整的简单 user／agent 消息结构；此单一样本不足以证明 summary 适合历史展示。
2. 文件变更 fixture 的同一个 turn：`summary` 返回 2 个 item（userMessage、最终 agentMessage），`full` 返回 6 个 item（包括两条 reasoning、一条中间 agentMessage 和 fileChange）。因此 summary 会静默省略 Web 应能呈现的原始内容，不能用于 [0013](../specs/0013-paged-history-and-large-owner.md) 的历史页。
3. 对同一 fixture 请求 `thread/turns/list(itemsView: full, sortDirection: desc, limit: 1)` 得到一个 turn 和非空 `nextCursor`；用该 cursor 取到另一个不同 turn，第二页 cursor 为空。`thread/read(includeTurns: false)` 返回身份信息且 turns 为空。

据此把 0013 Draft 改为 `itemsView: full` 并加入“不得用 summary 冒充完整 turn”的测试门禁。以上只证明当前版本的两个隔离样本；大于 8 MiB 的单 turn、跨页并发新增 turn 和升级后兼容性仍待 TDD 与只读实测。
