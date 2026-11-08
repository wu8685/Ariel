# 0137 Puck `start_task` 用户消息级幂等

- **Status**: Implemented（2026-11-08；Full 真实 E2E 已通过，ask 真实 Brain 未派发，暂不可部署）
- **Extends**: 0134、0135

## 1. 问题与证据

0134 只以 Brain 提供的 `clientTaskId` 去重。Brain 在同一用户消息对应的长 turn 中再次调用
`start_task` 时可以生成新的 UUID，Relay 因而把每次调用视为新任务。Turn 47 的隔离冒烟留下三条
task：它们属于同一个 assistant，分别使用三个 task id 和三个 native thread，创建时间相隔约
107 秒与 52 秒。这证明重复创建来自三次独立 `start_task` 调用，而不是单次 handler 或 SQL 写入
重复执行；当前服务端幂等键不足以吸收 Brain 重投。

此外，Desktop BrainHost 的 dynamic tool 默认等待时间短于 release smoke 允许的 mutation 窗口。
慢 `thread.bootstrap` 可能已经成功创建 task，但 tool result 到达时原 call 已超时；Brain 收到失败或
无法确认结果后可能再次调用。Module 当前也没有检查 `puck.brain.toolResult` 的回传结果。

## 2. 目标

1. 同一 assistant、同一用户消息最多创建一个 task，不依赖 Brain 是否复用 UUID。
2. 重复 `start_task` 返回已经存在的 task；不得再次调用 `thread.bootstrap`。
3. `ask` 模式对同一用户消息最多创建一条 pending authorization；批准后仍只创建一个 task。
4. tool result 的等待窗口覆盖 Relay 支持的完整 mutation 窗口；已确认成功的 task 不因本地
   tool call 提前超时而被 Brain 误判。
5. Brain 在 `start_task` 成功后使用 `get_task_status` 查询、使用 `send_turn` 续投，不为同一请求
   再次调用 `start_task`。

## 3. 稳定幂等键

### 3.1 来源

Relay 从可信 Puck 状态取得当前 Brain turn 对应的用户 `clientMessageId`。Brain tool arguments
不能声明或覆盖该值。正常运行优先使用内存中的 pending turn 归属；Relay/Brain 重启后的工具事件
使用该 assistant 最新的持久化用户消息恢复归属。

### 3.2 派生与持久化

- 服务端以 `assistantId + userMessageId` 派生稳定 UUID，作为 effective `clientTaskId`。
- 原始 Brain `clientTaskId` 仍须满足 0134 的 UUID 契约，但只在无法取得可信用户消息归属的兼容
  路径中作为幂等键。
- task 持久化 `source_message_id`，并以 `(assistant_id, source_message_id)` 建立唯一约束；旧 task
  的该字段为空，不参与消息级唯一约束。
- task 查找顺序为 source message，再为 effective task id。命中时返回完整已有 task。
- `thread.bootstrap.clientMessageId` 使用 effective task id，使 Desktop Agent 的 mutation 幂等与
  Relay task 幂等一致。

## 4. `ask` 授权幂等

- `start_task` authorization 持久化同一 source message id。
- 同一 assistant、同一 source message 已有未过期 pending authorization 时，重复调用返回原
  authorization id，不新增 row/card。
- 用户批准时，冻结动作携带 source message id；即使 Brain 原始 UUID 不同，最终执行仍使用相同
  effective task id。
- rejected、expired authorization 不阻止用户通过新的用户消息重新发起任务。

## 5. Tool result 与重试语义

- BrainHost dynamic tool 默认 timeout 必须大于 Relay 最大受支持 mutation 窗口；本版本使用
  4 分钟，覆盖 release smoke 的 3 分钟预算。
- `puck.brain.toolResult` 返回 accepted 才算结果已交付；失败必须留下有界、无参数正文的诊断记录，
  不得自动重放 `start_task`。
- `OUTCOME_UNKNOWN` 仍不得自动重放 mutation。后续相同用户消息的显式 Brain 重投只允许命中服务端
  幂等记录；没有已确认 task 时返回 unknown，由 Brain 通过状态工具核对。

## 6. Brain 指令

Brain developer instructions 必须明确：

1. 每条用户消息对同一执行目标只调用一次 `start_task`；
2. 成功后保存返回的 `taskId`，用 `get_task_status` 跟进；需要追加指令时用 `send_turn`；
3. timeout、`OUTCOME_UNKNOWN` 或 tool result 失败时先核对状态，不得生成新 UUID 再次 `start_task`。

指令变更提升 toolset version，使既有 Bot 在下一次用户驱动时轮换到新 Brain thread。

## 7. 冒烟与验收

1. Red：同一 source message 使用两个不同 Brain `clientTaskId`，断言旧实现创建两个 task；修复后
   只转发一次 bootstrap，两次均返回同一 task。
2. Red：`ask` 模式重复调用只产生一个 pending authorization；批准后只有一个 task。
3. 数据库 v2 → v3 migration 保留旧记录并建立 source message 唯一约束。
4. BrainHost timeout 覆盖三分钟慢 mutation，且成功 tool result 可在窗口内回传。
5. Full Access 真实隔离 E2E 在首次出现 task 后继续观察完整行为窗口，最终仍为
   **1 task / 0 authorization**。
6. ask 真实隔离 E2E 单独运行，完整窗口后为 **0 task / 1 authorization**。
7. 两个模式不得在同一子测试中串行复用 Bot、Brain thread 或 task 状态。
8. `go test ./...`、相关 race、`go vet ./...`、Go build、`npm test`、`test:ui` 与生产 build 通过。

## 8. 发布边界

本轮只提交并 push 修复分支，不 merge、不部署、不开 PR。公网保持回滚版本不变。
