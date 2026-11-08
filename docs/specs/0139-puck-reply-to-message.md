# 0139：Puck 引用某条消息回复

- 状态：Approved（2026-11-08；用户在 Turn 51 明确授权 spec-first 后直接实现）
- 日期：2026-11-08
- 依赖：0130 Puck 对话、0136 结构化卡片、0137 消息幂等。实现分支基于尚未合入 `main` 的 turn 50 分支，并继承 turn 48／49。

## 1. 目标与范围

用户可从 Puck 连续对话中的用户消息、助理消息、任务卡和决定卡发起回复。新消息保留被引用消息的稳定 ID；界面展示引用关系，Brain 得到有界引用上下文；引用任务卡时优先续投已有任务，不创建重复任务。

本规格不改变 Codex 会话引用能力，不引入多租户，不部署线上，也不把消息正文复制到第二张表。

## 2. 数据与 wire contract

### 2.1 输入

`puck.message.send.params` 新增可选字段：

```json
{"replyTo":"message-id"}
```

- `replyTo` 为空或缺失表示普通消息。
- 非空值必须是已有 Puck 消息 ID，长度不超过 128 字节。
- Web 的 optimistic message 同样携带 `replyTo`。

### 2.2 持久化与输出

`conversation_messages` 新增 `reply_to TEXT NOT NULL DEFAULT ''`。`Message` JSON 新增可选 `replyTo`。迁移必须保留旧数据库；旧行读为无引用；旧客户端忽略未知字段。

新增只读方法 `puck.message.get(assistantId, messageId)`，返回同一助理中的单条消息，用于引用跳转时按需加载；它不改变历史、不增加未读数。

## 3. 服务端行为与不变量

1. Relay 已有 Web session 认证仍是 owner 边界。Puck 当前是单 owner 模型；模块还必须确认 `replyTo` 指向同一 `assistantId` 的消息。跨 Bot、未知消息、已删除 Bot 或过长 ID 返回 `INVALID_REPLY_TARGET`，且不得写入用户消息或启动 Brain turn。
2. 用户消息与其 `replyTo` 原子地写入同一消息行；重启后历史仍返回该字段。
3. Brain 上下文包含有界引用内容：作者、消息类型、最多 800 个 Unicode code point 的正文；结构化卡片额外给出可验证的 `taskId`（若存在）。不得把未校验的客户端摘要传给 Brain。
4. Brain 指令明确：当前用户是在回复指定消息；若引用上下文含 `taskId`，应优先用 `send_turn(taskId, ...)` 跟进，除非用户明确要求创建独立任务；不得为同一跟进调用 `start_task`。
5. `replyTo` 不继承到 Puck 自动生成的 processing／ack／结果消息；引用关系只属于用户发送的那条消息。
6. `puck.message.get` 同样按 `assistantId` 查询，不得通过 ID 读取其他 Bot 的消息。

## 4. Web 交互

1. 桌面：每条用户消息、助理消息、任务卡、决定卡在 hover／keyboard focus 时出现“回复”按钮。
2. 移动端：在消息上向左滑至少 56 px 进入回复状态；现有长按 reaction 保持不变。滑动只选择目标，不发送消息。
3. composer 上方出现毛玻璃引用预览条，显示作者与一行摘要；`×` 可取消。切换 Bot 时必须清空引用目标。
4. 已发送消息在正文或卡片上方显示引用块。点击后：
   - 原消息已加载：滚动到原消息并高亮约 1.5 秒；
   - 原消息未加载：调用 `puck.message.get`，成功后把原消息并入当前历史再滚动；
   - 原消息不存在／不可读：显示“原消息暂不可用”，不崩溃。
5. 引用摘要对纯文本取首个非空行；任务／决定卡优先使用卡片标题或消息正文；展示最多一行并省略溢出。
6. 回复按钮具备可访问名称与 focus-visible 状态；引用预览和引用块不遮挡 composer、时间与 reaction。

## 5. 错误处理与兼容性

- 客户端收到 `INVALID_REPLY_TARGET` 时保留失败消息的重试内容，但清除失效引用并提示用户重新选择。
- 旧 Web 发送时不带 `replyTo`，行为不变；旧数据库自动迁移；旧客户端反序列化时忽略 `replyTo`。
- 单条读取返回 `PUCK_MESSAGE_NOT_FOUND`，不得泄露目标是否存在于其他 Bot。
- Brain 引用上下文构建失败时拒绝本次发送，不以无引用请求静默降级。

## 6. 验收与测试映射

### Go

- store schema v5 迁移、append／upsert／history round-trip `replyTo`；旧 schema 升级后旧消息可读。
- 同 Bot 引用通过；跨 Bot和未知 ID 拒绝且不写消息；`puck.message.get` 不跨 Bot。
- Brain turn 文本包含截断后的引用作者、类型、正文；任务卡包含 `taskId` 与 `send_turn` 优先指令。
- 模拟 Brain 对引用任务卡调用 `send_turn`，断言续投原 task/thread，且无新的 `thread.bootstrap`／任务记录。

### Web / Vitest

- 桌面回复按钮、引用预览、取消、发送 `replyTo`、消息引用块、点击滚动与高亮。
- 未加载目标触发 `puck.message.get`；失败提示不崩溃。
- 切换 Bot 清空引用；旧消息无 `replyTo` 正常渲染。

### Playwright

- 桌面 hover 回复流程。
- 390 px touch viewport 左滑回复、引用预览、发送后引用块、点击跳转和高亮；保存截图基线。

### 完整验证

运行 `go test ./...`、`go test -race ./...`、`go vet ./...`、Go build、`npm test`、`npm run test:ui`、Web production build。隔离本地端到端分别验证引用助理消息回复，以及引用任务卡后 `send_turn` 续投且不新建任务。
