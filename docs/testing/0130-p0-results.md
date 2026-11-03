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

## 回归与交付验证

| 范围 | 命令 | 结果 |
| --- | --- | --- |
| Go 全量 | `go test ./...` | 通过 |
| 竞态检查 | `go test -race ./internal/puck ./internal/puck/tools ./internal/desktopagent` | 通过 |
| P0 fail-closed smoke | `./tests/puck-p0-smoke.sh` | 通过 |
| Web 单测 | `cd web && npm test` | 45 files、374 tests 全部通过 |
| Web production build | `cd web && npm run build` | 通过 |
| Chromium 回归 | `cd web && npx playwright test` | Puck 320／390／1280px 与截图基线 3/3 通过；最终全量首轮 158/159，唯一失败为既有 file-workspace reload 超时，原命令精确复跑 1/1 通过（本分支较早一轮全量 159/159 通过） |
| Kubernetes | `kubectl kustomize deploy/kubernetes/overlays/passkey-puck` | 通过；渲染含 Puck 开关、`/data/puck`、896MiB／1152Mi，PIN overlay 相对 `main` 无变更 |
| ECS | `deploy-ecs.sh ... --mode passkey --enable-puck --dry-run` 及 PIN 反例 | Passkey 通过；PIN 组合按固定原因拒绝 |

## 尚未完成的环境验证

- 一次性 Docker 镜像构建连续两次在读取 Docker Hub 的 `golang:1.26-alpine`／`alpine:3.23` metadata 时返回 `DeadlineExceeded`；本机没有相应 base image cache，因此 §15 P0 验收 14 的“镜像开启／关闭两种配置启动”仍为 `unverified`。失败发生在拉取构建基础镜像之前，不是 Ariel 编译或启动失败；两次构建均未产出镜像或容器。
- 未运行 `scripts/ariel.sh up local` 的真实拒绝路径，因为本任务明确禁止触碰当前本地部署；其拒绝行为由启动／脚本契约测试覆盖。
