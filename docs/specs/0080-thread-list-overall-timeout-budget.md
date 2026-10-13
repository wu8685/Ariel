# 0080：会话菜单刷新总预算与单次读取预算

- 状态：Implemented（2026-10-13；TDD、Go 全包、race、构建与本机公网 Agent 重启验收通过；用户侧慢请求复验待完成）。
- 来源：0078 将 `thread/list` 单次 App Server deadline 从 3 秒调到 5 秒，但 recent list 的两个 attempt 会在约 `5 + 5 = 10` 秒耗尽，仍早于 Relay 35 秒与 Web 40 秒总窗口。用户在本机浏览器实测约 10 秒即显示超时，与该路径精确吻合。
- 范围：修正 Desktop Agent 的会话菜单读取预算模型；不调整 Relay／Web 总窗口、不增加重试次数、不改变写操作或图片规格 0079。

## 已确认原因

1. 当前本机没有 local Relay 进程；Desktop Agent 直接连接 `wss://www.kan.chat/ws`，因此并非旧本地 Relay 的 10 秒默认值截断请求。
2. 生产 `threadListAttemptTimeout` 为 5 秒，`RestartingRPC` 对 `thread/list` 最多执行两个 attempt。
3. 如果首个 recent list endpoint 一直无响应，第一次 5 秒后重建，第二次 5 秒后返回 `context.DeadlineExceeded`；Web 因而在约 10 秒收到 Agent 的 `TIMEOUT`。
4. 0078 所称“最坏约 30 秒”只覆盖 recent、pinned filter、pinned fallback 都依次接近各自上限的累计估算，不能让任何单个关键读取等待 30 秒。该预算模型不足以解决慢 recent list。

## 输入与输出

- 输入：Web 发起的不带搜索词的 `thread.list` 会话菜单刷新。
- 成功输出：沿用现有会话索引、置顶合并、分页 cursor 与 Web 缓存更新结构。
- 失败输出：整个读取预算耗尽后返回确定性的 `rejected/TIMEOUT`。

## 行为与不变量

1. Desktop Agent 为一次不带搜索词的会话菜单刷新建立 30 秒总 context deadline。
2. `thread/list` 单次 App Server attempt deadline 从 5 秒调到 15 秒；首次超时仍只重建 endpoint 并重试一次。
3. recent list、现代 pinned filter 与 Codex 0.160.0 pinned section fallback 必须共用同一个 30 秒总 context，不得分别获得 30 秒。
4. 如果 recent list 第一次在 15 秒超时，第二次最多使用剩余约 15 秒；如果 recent 较早成功，置顶读取只使用总预算的剩余时间。
5. Relay 只读 route 保持 35 秒，Web 只读等待保持 40 秒，继续满足 `Agent 30s < Relay 35s < Web 40s`。
6. `thread/search`、历史读取、图片读取及写操作不受本规格影响；写操作不得自动重放。
7. Web 已确认的旧会话列表在刷新超时后继续保留，不因失败清空。

## 边界条件与错误处理

- 调用方提前取消时立即停止，并返回调用方 context 错误。
- 单次 attempt 受 15 秒 deadline 与 30 秒父 context 中更早到期者约束。
- 第二次 attempt 仍超时，或 pinned 阶段耗尽剩余总预算时，停止后续读取并映射为 `TIMEOUT`。
- 不把读取超时报告为 `OUTCOME_UNKNOWN`，不增加第三次 endpoint 启动。

## TDD 与验收

1. Red：生产配置测试从当前 5 秒期望改为 15 秒，先确认失败。
2. Red：新增总预算测试，证明当前 `Service.List` 没有 30 秒父 context；采用注入的毫秒级预算验证 recent 与 pinned 共享同一个 deadline，而不是累加。
3. Green：为菜单 refresh 增加 30 秒父 context，并把单次 attempt 改为 15 秒。
4. 保留并通过 endpoint 首次超时后只重建一次、第二次超时停止、mutation 不重放的既有测试。
5. 运行 Go 全包测试、race 和 Desktop Agent 构建；重启本机公网 Agent，确认到 Relay 的连接恢复。
6. 真实浏览器计时回归：构造首个 endpoint 超过 10 秒但在 15 秒内返回的隔离读取，确认不会在约 10 秒提前显示 `TIMEOUT`；物理手机保留用户复验。

## 非目标

- 不把 Relay 或 Web 超时继续调大。
- 不用无限等待掩盖 App Server 卡死。
- 不增加轮询或逐会话请求。
- 不改动 0079 图片归一化方案。

## 实施结果

- Desktop Agent 的生产 `thread/list` 单次 attempt 已从 5 秒调整为 15 秒，仍只允许首次超时后重建并重试一次。
- 不带搜索词的菜单刷新现在使用共享的 30 秒父 context；recent、pinned filter 与 pinned section fallback 不再分别累加完整预算。
- `thread/search` 沿用原 context，Relay 35 秒与 Web 40 秒保持不变。
- 新增生产预算与 context 作用域回归测试，明确约束 `2 × 15s = 30s`，防止再次出现 10 秒过早失败或串行预算失控。
- 本机公网 Desktop Agent 已替换为新二进制并通过 launchd 重启，确认到 `www.kan.chat:443` 的连接处于 `ESTABLISHED`。
- 验收证据见 [`docs/testing/2026-10-13-0080-thread-list-overall-timeout-budget.md`](../testing/2026-10-13-0080-thread-list-overall-timeout-budget.md)。
