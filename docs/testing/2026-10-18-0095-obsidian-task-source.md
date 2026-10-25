# 2026-10-18：0095 Obsidian Task Source POC

## 结果

0095 的隔离 M=E POC 通过：同一受权 Ariel Desktop Agent 同时担任 source 和 executor，经本地 Relay 完成了 Obsidian Markdown 发现、native Codex claim-only turn、CAS 领取、同 thread work turn 和 `DONE` 回写。

真实会话经 Codex App 会话索引重新读取，确认：

- 首 turn 只有 claim intent，没有执行任务；
- 回写 `WIP` 后，第二 turn 才收到真正任务；
- 两个 turn 属于同一 native thread；
- 任务最终为 `DONE`，claim comment ID 只出现一次；
- App Server bootstrap 结束后完成 Desktop follower 接管，deep link 打开成功。

测试使用临时 vault 和无副作用任务，未修改 `brain-spark` 正式任务，未重启现有 Desktop Agent 或公网 Relay。真实 thread ID、临时路径和凭据不写入文档。

## TDD 覆盖

- parser：标准 frontmatter、状态枚举、非法文件独立跳过；
- discovery：仅选 `TODO + agent_host + unclaimed`，空 `task_id` 稳定填充；
- safety：symlink 逸出拒绝、revision 变更拒绝、原子替换；
- claim gate：accepted receipt 不会提前生成 work，只有绑定后的 claim fact 才写 `WIP`；
- intent：拒绝 task/revision 不匹配、Markdown fence、多余字段或附加正文；
- outcome：同 thread `done` 回写 `DONE`，后续扫描不再投递；
- protocol：`claim/work` 必须有 64 位 task revision，task fact 只上传受限 action/revision，不上传 assistant 正文。

## 验证命令

```sh
go test ./... -count=1
go test -race ./... -count=1
go vet ./internal/obsidiantasks ./internal/desktopagent ./protocol ./cmd/desktop-agent
go build ./cmd/...
ARIEL_OBSIDIAN_NATIVE_POC=1 go test -v ./internal/desktopagent \
  -run '^TestObsidianTaskSourceRealNativePOC$' -count=1
```

结果：Go 全量与全量 race 通过；改动 package 的 vet 和全部 cmd build 通过。`go vet ./...` 仍只报既有 `internal/wsheartbeat/heartbeat_test.go:49` 在非测试 goroutine 调用 `t.Fatal`，该文件本轮未修改。

## 保留项

- 两台独立物理主机的 M→E 实测尚未完成；协议路由与错主机拒绝已由隔离测试覆盖。
- 本轮未部署、未替换 launchd 进程、未更新公网 Relay。
