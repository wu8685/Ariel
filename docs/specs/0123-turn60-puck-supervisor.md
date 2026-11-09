# 0123 Puck Supervisor 小步恢复

- 状态：Approved
- 日期：2026-11-09

## 规格

- `Supervisor.Go` 的 handler 正常返回时结束；panic 时恢复、记录 panic、写日志，并以 100ms 起步、最大 5s 的指数 backoff 重启。
- 达到既有 circuit threshold 后停止重启，保持 circuit open 语义。
- `Supervisor.Call` 为 handler 派生可取消 context。调用 context 超时/取消时主动 cancel handler context，并记录仍在运行的 handler 次数与日志。
- 不强杀 goroutine，不声称能约束忽略 context 的 handler；完整 worker queue 留待后续设计。
- 日志不含请求正文、assistant/thread ID 或参数。

## 验收

- 后台 handler 连续 panic 后按 backoff 重启，正常返回后停止。
- Call timeout 后 handler 能观察到 `ctx.Done()`，Supervisor 的 still-running counter 增加。
- 既有 circuit breaker 测试继续通过，并运行 race test。
