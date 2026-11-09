# 0120 Brain 事件过载交付

- 状态：Approved
- 日期：2026-11-09
- 授权：Turn 60 用户明确授权按 architecture review 的 P0 第一步落地

## 输入与输出

- 输入：Desktop Agent 发往 Relay 的 `puck.brain.*` event 与 device status。
- 输出：Puck Module 收到原事件；或在不可丢事件无法进入主队列时收到明确的 delivery failure。

## 行为与不变量

1. `puck.brain.toolCall` 与非 status 的 Brain event 视为不可丢事件。主队列满时最多等待 250ms。
2. 等待期内出现容量则正常交付；超时后累计 drop counter，并记录只含 assistant 摘要 ID、事件类型与累计值的日志。
3. 超时后通过独立、有界的 failure 通道通知 Module。Module 清理当前 pending turn，并写入用户可见的 error message；不得等待分钟级 mutation timeout。
4. `puck.brain.status` 与 device status 是状态类事件。队列满时替换待处理旧值，仅保留 latest-only。
5. 不记录 event payload、提示词、工具参数或完整 assistant/thread ID。
6. 不改变公开协议、Puck toolset schema 或数据库 schema。

## 边界与错误处理

- failure 通道同样有界；若它也持续满载，则记录无敏日志，不创建无界 goroutine 或内存队列。
- 未知 Brain thread 仍按既有行为忽略。

## 验收标准

- 测试证明不可丢事件会短暂等待并在容量释放后交付。
- 测试证明持续满载会产生 delivery failure 和 drop 计数。
- 测试证明状态类事件 latest-only。
- 测试证明 Module 将 delivery failure 转成用户可见 error message。
