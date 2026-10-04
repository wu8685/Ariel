# 0013 分页 API 只读复核（2026-10-05）

## 范围

- 使用当前 Desktop 内置 Codex CLI `0.160.0` 的独立 App Server；先对 Ariel 隔离 fixture 调用 `thread/read` 与 `thread/turns/list`，后对用户反馈的目标大会话做只读容量扫描；`initialize.capabilities.experimentalApi=true`。
- 输出只保留结构、计数和大小，不打印或保存 thread ID、工作目录、消息正文、item 内容和 cursor。未向任何会话发送消息，也未通过 Desktop IPC 变更 owner。

## 观察

1. 普通双 turn fixture 中，`itemsView: summary` 与 `full` 都返回完整的简单 user／agent 消息结构；此单一样本不足以证明 summary 适合历史展示。
2. 文件变更 fixture 的同一个 turn：`summary` 返回 2 个 item（userMessage、最终 agentMessage），`full` 返回 6 个 item（包括两条 reasoning、一条中间 agentMessage 和 fileChange）。现有 Web 不显示 reasoning，但中间消息与 fileChange 是 UI 相关内容；因此不能用 summary 冒充 [0013](../specs/0013-paged-history-and-large-owner.md) 的完整历史页。
3. 对同一 fixture 请求 `thread/turns/list(itemsView: full, sortDirection: desc, limit: 1)` 得到一个 turn 和非空 `nextCursor`；用该 cursor 取到另一个不同 turn，第二页 cursor 为空。`thread/read(includeTurns: false)` 返回身份信息且 turns 为空。
4. 目标大会话共 47 个 turn。按每页 10 turn 读取 `full` 时，5 页累计约 45.4 MB，最大一页约 18.2 MB，两页超过 8 MiB；其中一个旧 turn 自身约 14.3 MB，包含 1,628 个 item。最新 10 个 turn 分别按单 turn 读取均低于 8 MiB。因此“页超限就把 limit 降到 1”的方案仍会卡在这个旧 turn。
5. 目标大会话最大单 item 约 1.08 MB，没有单 item 超过 7 MiB。对该巨型 turn 调用 `thread/items/list(limit: 100, sortDirection: asc)`，17 页共返回 1,628 个 item，按 ID／内容／顺序与 `full` turn 完全一致；最大 item 页约 1.93 MB。倒序前两页共 200 个 item 也精确对应原 turn 最新的 200 个 item。`thread/turns/list(itemsView: notLoaded)` 对 47 个 turn 返回身份／状态而不携带 items，可作为巨型 turn 的有界元数据入口。

据此把 0013 Draft 改为“本机 64 MiB 容量 + 手机默认最近 10 turn + `itemsView: full` 优先 + 巨型 turn 的 `notLoaded` 元数据／`thread/items/list` 子分页”，并加入“不得用 summary 冒充完整 turn”的测试门禁。以上只证明当前版本的隔离样本与目标大会话的只读结构；跨页并发新增 turn、单 item 超限和升级后兼容性仍待 TDD 与实测。
