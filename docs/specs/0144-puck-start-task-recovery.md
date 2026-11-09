# 0144 Puck `start_task` 生产恢复

状态：Approved（2026-11-09；Turn 63 用户明确授权修复并发布）

## 1. 问题

生产 Codex 0.160.0 Brain 调用 `start_task` 时出现两类连续失败：

1. Brain 生成的 `clientTaskId` 不是 UUID，Relay 在 tool contract 层以
   `INVALID_ARGUMENT` 拒绝；
2. Brain 后续改用合法 UUID，但猜测了目标设备上不存在的 `cwd`。Desktop Agent 将目录不存在
   压缩为裸 `INVALID_ARGUMENT`，Brain 和用户都拿不到可行动信息。

两类失败均发生在 native `thread/start` 之前，生产 DB 没有创建 task 或 authorization。

## 2. 行为

1. `start_task` 的幂等 task id 由 Relay 根据可信 `assistantId + sourceMessageId` 生成；新版 Brain
   schema 不再要求模型生成 `clientTaskId`。
2. 为兼容已经加载旧 toolset 的 Brain，`clientTaskId` 仍作为可选 legacy 字段接收；只要存在可信
   `sourceMessageId`，其值不参与 task identity，并在 strict validation 前被服务端生成值覆盖。
3. 没有可信 `sourceMessageId` 的内部调用仍必须提供合法 UUID；未知字段、超限字段、非法类型与
   其他 contract 约束继续 fail closed。
4. 目标 `cwd` 不存在时，Desktop Agent 返回 `FILE_NOT_FOUND`，并提供不含原始路径的安全说明：
   Brain 应使用 `list_threads` / `read_thread` 返回的 cwd，或向用户确认后重试，不得猜测路径。
5. 非绝对路径、NUL、超长路径和“存在但不是目录”继续返回 `INVALID_ARGUMENT`。
6. 不改变 DB schema，不建立离线队列，不自动重放 mutation，不回显用户正文、完整 thread id 或
   本机绝对路径。
7. Codex `turn/completed` 若含失败状态或 error，不得记录为成功完成；Agent 只记录稳定错误码，并向
   Bot 流发送固定 `BRAIN_UNAVAILABLE` 终态，由 Relay 转为可读失败消息。原始 provider message
   不进入日志、协议或 DB。

## 3. 验收

1. Red：复现 Codex 0.160 的非 UUID `clientTaskId` 与不存在 cwd 的裸错误。
2. Green：可信 Brain 调用在缺失／畸形 legacy id 时使用稳定服务端 task id；同一用户消息重试只
   创建一个 task。
3. 不存在 cwd 返回 `FILE_NOT_FOUND` 与可行动说明；Bot 可据此纠正或询问用户。
4. Codex 0.160.0 隔离真实 Brain 冒烟覆盖 `start_task` 成功，随后 `send_turn` 与
   `get_task_status` 成功；Full Access 保持 1 task / 0 authorization，并覆盖 `ask_user`。
5. 完整门禁、Agent 先于 Relay 的生产发布与临时 `turn63` Bot / native thread 清理按 Turn 63
   用户指令执行。
