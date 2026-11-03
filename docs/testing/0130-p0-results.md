# 0130 Puck P0 实施验证（2026-11-03）

## 隔离 smoke 结论

- 真实 Codex／Desktop 部分复用第 0 步的一次性 Q23 fixture 证据，详见 `0130-probe-results.md` P6：临时 cwd、专用 thread、显式逐 turn policy，最终因 Desktop IPC profile 不兼容而无法读回，thread 与目录均已删除，探针进程均已退出。
- 因 Q23 为 `unverified`，P0 按 §4.4 第 4 条 fail closed：执行主机未声明可证明的 `executionPolicy` 时，Relay 在发送 `thread.create` 前返回 `EXECUTION_POLICY_UNENFORCEABLE`。本轮没有再次创建真实 Codex thread，也没有连接既有 thread。
- mock brain → module tool handler → brain `toolResult` 往返通过；brain app-server 退出后的 `turn.lost`、退避重启与 `thread/resume` 通过。
- 可重复的无业务数据 smoke：`./tests/puck-p0-smoke.sh`。

## E1 时延证据

- Web 本地 optimistic “正在处理”在点击发送的同一 React update 中产生，组件测试覆盖，不等待 Brain。
- 真实热 app-server 的首条 agent delta 既有样本为 1545–2543 ms；含 dynamic tool 的 turn 约 12 s。该数据来自第 0 步隔离 fixture，不代表稳定 SLA。
- module 返回 `templateAfterMs: 3000`。当前 P0 Web 在 3 秒前显示本地 processing bubble；真实 Brain 文本事件的完整 streaming 映射仍是后续增强项。

## 明确降级

- Desktop follower policy 未经兼容读回证明，所以自动执行始终走 B10 拒绝分支；不能把此版本描述为“无需确认自动派发”。
- 物理 iPhone／Safari、Codex Desktop 侧边栏可见性复核及实际部署均未在本任务执行。
