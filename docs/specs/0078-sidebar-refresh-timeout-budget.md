# 0078：会话菜单刷新超时预算调整

- 状态：Implemented（2026-10-13；TDD、Go 全包、race、构建与本机公网 Agent 重启验收通过）。
- 来源：会话菜单刷新在 Codex App Server 响应较慢时，Desktop Agent 的单次 `thread/list` deadline 过短，容易提前返回 `TIMEOUT`。
- 范围：只调整 Desktop Agent 的 `thread/list` 单次等待预算；不改变 Relay／Web 总等待窗口、重试次数、写操作语义或缓存策略。

## 输入与输出

- 输入：Web 发起的 `thread.list` 读取请求，以及 Desktop Agent 向 Codex App Server 发起的一个或多个 `thread/list` RPC。
- 成功输出：沿用现有 `thread.list` 响应与会话菜单缓存更新行为。
- 失败输出：所有有限重试耗尽后，沿用确定性的 `rejected/TIMEOUT`。

## 行为与不变量

1. Desktop Agent 的 `thread/list` 单次 App Server deadline 从 3 秒调大到 5 秒。
2. 单次 RPC 超时后仍只允许重建 endpoint 并重试一次；不得增加重试次数或形成无界等待。
3. 首屏列表可能依次读取 recent、现代 pinned filter 与 Codex 0.160.0 pinned section fallback；按最坏 3 次 RPC、每次最多 2 个 attempt 计算，Agent 理论等待上限约为 30 秒。
4. Relay 只读 route 保持 35 秒，Web 只读等待保持 40 秒，继续满足 `Agent < Relay < Web` 的跨层预算顺序。
5. `thread/search`、历史读取和所有写操作不受本次调整影响；写操作仍不得自动重放。

## 边界条件与错误处理

- 调用方 context 更早取消时，应立即返回其取消原因，不等待满 5 秒。
- endpoint 在 deadline 前退出时，沿用现有重建与错误分类行为。
- 第二次 attempt 仍超时，必须返回 `context.DeadlineExceeded`，再由现有链路映射为 `TIMEOUT`。
- 不把读取超时误报为 `OUTCOME_UNKNOWN`，也不因超时清空 Web 已确认的缓存列表。

## TDD 与验收

1. Red：新增配置层测试，断言生产 Agent 使用 5 秒 `thread/list` 单次 deadline，而不是原来的 3 秒。
2. Green：仅修改 timeout 配置，使新测试通过；复用 0070 已覆盖的“首次超时后重建一次、第二次超时停止”测试。
3. 运行 Desktop Agent、App Server、Relay 相关 Go 测试，并运行 `go test -race ./internal/desktopagent ./internal/codex/appserver ./internal/relay`。
4. 构建并重启本机 Desktop Agent，确认 launchd 进程在线且能通过公网 Relay 刷新会话菜单。

## 非目标

- 不延长 Relay 或 Web 的总请求超时。
- 不改变会话列表分页大小、刷新频率或本地缓存 TTL。
- 不以额外轮询换取成功率。
- 不修改 Codex App Server。

## 实施结果

- Desktop Agent 的生产 `thread/list` 单次 deadline 已从 3 秒调整为 5 秒，endpoint 自愈仍只重试一次。
- 新增生产配置回归测试，防止后续修改意外恢复过短预算或越过已确认值。
- Relay 35 秒与 Web 40 秒总等待窗口均未变更，跨层预算仍为 `Agent < Relay < Web`。
- 本机公网 Desktop Agent 已用新二进制重启，并确认到 `www.kan.chat:443` 的连接处于 `ESTABLISHED`。
- 验收证据见 [`docs/testing/2026-10-13-0078-sidebar-refresh-timeout-budget.md`](../testing/2026-10-13-0078-sidebar-refresh-timeout-budget.md)。
