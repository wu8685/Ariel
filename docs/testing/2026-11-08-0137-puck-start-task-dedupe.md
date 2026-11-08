# 0137 Puck `start_task` 用户消息级幂等测试记录

日期：2026-11-08。公网保持回滚版本，未部署、未 merge、未创建 PR。

## 根因证据

- Turn 47 保留的 Relay SQLite 经只读快照和 `PRAGMA integrity_check` 校验为 `ok`。
- 三条 task 属于同一 assistant，但 task id 与 native thread 均各不相同；创建间隔约为
  107 秒和 52 秒。这证明 Brain 发出了三次彼此独立、使用不同 `clientTaskId` 的
  `start_task`，不是单次 handler/SQL 调用重复执行。
- 0134 只按 Brain 提供的 `clientTaskId` 去重，因此无法吸收同一用户消息下的新 UUID 重投。
- Brain 指令此前没有把成功后的跟进限定为 `get_task_status` / `send_turn`；BrainHost dynamic
  tool 默认 timeout 也只有 60 秒，短于 release smoke 允许的三分钟 mutation 窗口。
- 本地 `main@82df9b8` 真实基线 smoke 延长观察后未稳定复现线上 1→3，而是六分钟内保持
  0 task / 0 authorization。该模型路径非确定性；不能把这次结果冒充复现成功。线上持久层
  与确定性双调用测试用于确认重复执行层次。

## TDD

Red：

- 同一 source user message 使用两个不同 Brain UUID 时，旧 `Call`/`FrozenAction` 不存在
  source message 幂等字段；测试编译失败。
- BrainHost 三分钟慢 mutation 测试观测默认 timeout 为一分钟。
- 模块重复事件在旧逻辑会创建两个 task 或两条 authorization。

Green：

- Relay 以 assistant + source user message 派生稳定 UUID；task 同时持久化
  `source_message_id` 并有唯一索引。
- ask pending authorization 按同一 source message 返回原 authorization，不新增 row/card。
- BrainHost tool timeout 为四分钟；tool-result 回传失败写入无参数正文的 audit，不自动重放。
- Brain 指令要求确认后在同一 turn 继续调用工具，成功后只用
  `get_task_status` / `send_turn` 跟进；更强的“第一动作必须 tool”诊断版本会让 Full 也稳定
  退化为 0/0，已依据实测回退，不把 prompt 当成幂等保障。
- v1/v2 → v3 migration、重复 Full/ask 事件、失败 tool-result audit、toolset version 轮换测试通过。

## 真实隔离 E2E

每次运行均使用新的临时 Relay state、Puck Bot、Brain workspace、执行目录与 thread；不恢复或
修改已有 Codex 会话。

| 模式 | 结果 | 完整行为窗口最终计数 |
|---|---|---:|
| Full | 通过；首次 task 后继续观察三分钟，总耗时约 226 秒 | 1 task / 0 authorization |
| ask | 未通过；三次正式／诊断运行均未到达 handler，脱敏 assistant kind 为纯文本序列 | 0 task / 0 authorization |

ask 的确定性 Module 测试为 0 task / 1 authorization，证明 policy 与授权幂等本身有效；但真实
Codex Brain 未调用 dynamic tool，因此不能把模块测试替代为真实 ask 通过。发布门禁保持关闭。
