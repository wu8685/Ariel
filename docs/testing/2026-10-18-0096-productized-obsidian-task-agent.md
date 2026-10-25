# 2026-10-18：0096 产品化 Obsidian Task Agent

## 结果

0096 已完成本机与公网部署，真实链路通过：用户从 Obsidian Bases 的 `TODO` 列原生新建卡片，Ariel 自动补齐模板与稳定主机 ID，经公网 Relay 创建一个 Codex native thread，并在同一 thread 内依次完成 claim、blocked work 与人工评论后的 done work。

最终任务文件为 `DONE`，保留四类 append-only 评论：领取、Agent 阻塞、人工反馈、完成。Codex App 可读取该 thread，三个 turn 均为 `completed`，后两个 work turn 没有创建第二个负责人。

## TDD 覆盖

- Obsidian Bases 最小卡片规范化与文件重命名；
- startup 持久配置、默认 host/manager/root 与不安全路径拒绝；
- accepted receipt 的 journal 恢复只走 tracking；
- 新 revision 淘汰同 task 的旧投递与 receipt；
- blocked 写回不自触发，人工编辑后续投同一 thread；
- journal 损坏、symlink、stale revision 与 workspace 越界 fail closed；
- failed native fact 清理恢复状态并允许重试。

## 真实链路证据

1. Obsidian 原生新卡被补齐 `task_id`、`agent_host`、正文与评论区。
2. claim turn 返回受 revision 绑定的 exact intent；随后文件写入 `WIP`、native `threadId` 和领取评论。
3. 首个 work turn 返回 `blocked`，文件保持 `WIP`，且 Ariel 自写评论没有触发循环。
4. 人工追加“继续验收”评论后，同一 thread 收到第二个 work turn 并返回 `done`。
5. 文件更新为 `DONE`；journal 不再保留该任务的 pending delivery。
6. 公网 Relay `/healthz` 正常，本机 launchd Desktop Agent 使用 0096 deployment 持续运行。

真实 task/thread ID、token、用户目录与 Relay credential 不写入本文。

## 验证命令

```sh
go test ./... -count=1
go test -race ./... -count=1
go vet ./internal/obsidiantasks ./internal/desktopagent ./internal/startup ./protocol ./cmd/desktop-agent
go build ./cmd/...
npm test -- --run
npm run build
ARIEL_OBSIDIAN_NATIVE_POC=1 go test -v ./internal/desktopagent \
  -run '^TestObsidianTaskSourceRealNativePOC$' -count=1
```

全量 `go vet ./...` 仍输出一条既有诊断：`internal/wsheartbeat/heartbeat_test.go:49` 在测试 goroutine 内调用 `t.Fatal`；该文件不属于 0096 改动。本轮涉及 package 的 vet 与构建均通过。

## 部署与回滚

- 本机：launchd Desktop Agent 已切换至 0096 binary，任务配置由保存的 runtime config 恢复。
- 公网：Relay 使用 0096 image，task credential 以只读 data mount 提供。
- 上一版本的本机 plist 与上一版公网容器均保留，可按运行手册回滚；brain-spark 未 push 到任何远端。
