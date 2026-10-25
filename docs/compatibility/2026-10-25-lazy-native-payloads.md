# 2026-10-25：Owner 大字段流式裁剪验证

## 环境与边界

- Desktop / Codex 版本沿用当天已记录的 compatibility profile。
- 对既有大历史会话执行只读 owner 探针；不发送消息、不修改会话、不记录正文、thread ID 或 item ID。
- 普通内存 frame 上限仍为 64 MiB；兼容读取的 wire hard cap 为 256 MiB。

## 实测结果

1. 原始 owner snapshot frame 声明长度为 `144,459,205` bytes，超过旧 64 MiB 上限。
2. 字段级统计确认主要体积来自 `mcpToolCall.result.content`；同一会话中存在多项约 0.5–1 MiB 的结果正文。
3. streaming sanitizer 在 Desktop Adapter 边界移除已验证的 `mcpToolCall.result`、`arguments`、`output`、`diff` 与内嵌 `data:image/*`，保留 item 身份、类型、状态和顺序。
4. 裁剪后的 owner state 为 `1,337,185` bytes；owner 探针成功建立 snapshot，revision 连续，`requests` 状态可判定。
5. 合成的 32 MiB payload 回归测试证明裁剪期间的新增分配低于 4 MiB，不随被移除正文线性增长。未知大字段仍返回 `ErrFrameTooLarge`。

## 结论

当前兼容方案不能减少 Codex App 已经写入 Unix socket 的字节数，但能避免 Desktop Agent 为 144 MiB frame 分配等量内存，也不会把 tool detail／图片正文送入 Relay cache。历史和已完成 tool 的详情改由 `thread.activity.read` 在用户点击后分页读取；Relay 只转发，不缓存正文。

## 部署后回归：cache admission 必须发生在 Desktop Agent

公网首次部署后，真实多 watcher 场景暴露出一个实现偏差：Relay 虽然把单 entry 限制为 8 MiB，Desktop Agent 却会先尝试发送超限的规范化 snapshot。现场三个 hot thread 因此反复建立 follower；Agent 出现持续高 CPU，并发生 WebSocket heartbeat 写锁超时，手机端最终显示 `TIMEOUT`。

修复后，Desktop Agent 在公网传输前执行同一 8 MiB admission：

- `thread.cache.snapshot` 超限时只发送 `thread.cache.status`，reason 为 `CACHE_OVERSIZED`，不发送 thread 正文；
- progressive `thread.read` 超限时返回 `HISTORY_TOO_LARGE`，不把大响应写入 Agent／Relay WebSocket；
- 两种拒绝均保持 Agent 连接存活，Web 进入明确的 terminal fallback，不再无限显示“正在同步会话”。

该回归不改变 0100 的产品语义，而是补齐其“单 entry 最大 8 MiB、超限不得发送半个 payload”的既有边界。
