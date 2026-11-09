# Turn 63：Puck `start_task` 生产故障取证与未发布修复

- 日期：2026-11-09
- 基线：`93a1f27`
- 分支：`fix/turn63-puck-start-task`
- 发布状态：未发布；Codex 0.160.0 真实 Brain 门禁被外部 workspace routing 故障阻塞

## 生产取证

14:18–14:25（Asia/Shanghai），“工作助理”连续五次调用 `start_task`。Desktop Agent 的脱敏阶段
日志均显示 tool call 已到达、tool result 已返回，但 `toolSuccess=false`。

Brain rollout 的脱敏结构证据分为两段：

1. 首次调用的 `clientTaskId` 为 40 bytes、不是 UUID；Relay 返回
   `INVALID_ARGUMENT`，字段诊断为 `clientTaskId / value has invalid format / UUID`。
2. 后四次调用已使用合法 UUID，但 cwd 均为目标设备上不存在的猜测路径。Desktop Agent 在
   `canonicalProjectDirectory` 阶段把目录不存在压缩为裸 `INVALID_ARGUMENT`；没有进入 Codex
   `thread/start`。

生产 `puck.db` 只读核对：对应窗口没有新增 task 或 authorization；DB v5 integrity ok。取证未记录
用户消息正文、参数值、完整 thread id 或凭据。

## 修复

1. Puck toolset 升至 v5；`start_task` schema 不再要求 Brain 生成 `clientTaskId`。
2. Relay 使用可信 `assistantId + sourceMessageId` 生成稳定 task id；旧 Brain 传入的有界 string
   legacy id 被覆盖。无可信 source、未知字段、错误类型和超限值继续拒绝。
3. 不存在 cwd 返回 `FILE_NOT_FOUND` 与不含路径的行动说明，要求使用 `list_threads` / `read_thread`
   返回的 cwd 或询问用户，不再只返回裸错误码。
4. Codex `turn/completed` 含 failed/error 时不再记作成功；Agent 只记录
   `BRAIN_UNAVAILABLE`，并生成 Relay 可转为中文错误气泡的固定终态。provider 原始 message 不进入
   日志、协议或 DB。
5. 真实冒烟增加 task 创建后 `get_task_status` → `send_turn` 场景，并在结束时删除临时 Bot、归档
   Brain 与执行 thread。

## 自动门禁

| 门禁 | 结果 |
|---|---|
| `go test ./...` | 通过 |
| 相关并发包 `go test -race` | 通过 |
| `go vet ./...` | 通过 |
| `go build ./...` | 通过 |
| Web unit | 48 files / 428 tests 通过 |
| Web production build | 通过；仅既有 chunk-size warning |
| `git diff --check` | 通过 |

## Codex 0.160.0 真实 Brain 门禁

production/smoke binary parity gate 均确认 `codex-cli 0.160.0`。四次隔离 Puck E2E 和两次独立最小
app-server probe 均在首个 Brain turn 返回同一失败：

```text
workspace routing discovery failed
```

实际 app-server `turn/completed` 结构为 `status=failed` 且 `error` 非空；没有 assistant message，
因此无法进入本轮要求的 `start_task`、`send_turn`、`get_task_status` 与 `ask_user` 场景。新增失败终态
兼容后，第三次 E2E 在约 50–60 秒内明确记录 `ok=false / BRAIN_UNAVAILABLE`，不再等待四分钟或误报
成功。

所有 probe / E2E 创建的 native thread 均发送 `thread/archive` receipt；临时测试 DB 随隔离目录
回收。生产未创建 `turn63` Bot，用户现有 8 个 Bot 未改动。

## 发布结论

真实 Brain 是硬门禁，故本轮没有 fast-forward `main`、push、构建生产镜像或部署。生产继续运行
Relay `93a1f27` 与 Desktop Agent `0150-b72a806`，Relay healthy，DB v5 integrity ok；没有发生发布，
因此无需回滚。
