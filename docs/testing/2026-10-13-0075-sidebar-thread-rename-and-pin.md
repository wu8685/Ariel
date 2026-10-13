# 2026-10-13 侧栏会话重命名与原生置顶验收

## 结果

- Codex Desktop bundled App Server `0.160.0` 的隔离持久化 fixture 通过生产 `RestartingRPC` 路径完成 `thread/name/set`、`thread/section/move` 置顶与取消置顶；每项写入后均经 `thread/read` 和有界 `ListPinned` 复核，fixture 的 turn 数量未变化，测试结束后已删除。
- Desktop Agent 只允许当前有界已知集合中的 thread ID；名称执行 Unicode、UTF-8 byte 与 C0/C1 控制字符校验。同一 thread 的 metadata/archive mutation 串行，不同 thread 的 metadata mutation 全局最多并发 2 个。
- Web 仅在 Agent 声明 `threadRename`／`threadPin` 时显示对应菜单项。accepted 回执只 patch 标题与置顶 metadata；transcript、subscription、旧历史分页、输入草稿和截图草稿均不重建。
- 390×844 touch Chromium 已验证长按菜单、12px 菜单字体、重命名 dialog、置顶分组变化，以及操作期间正文和草稿保持可见。物理 iPhone/Safari 保留给公网部署后的用户复验。

## 自动验证

- `ARIEL_TEST_THREAD_METADATA=1 go test ./internal/desktopagent -run TestRealRenameAndPinIsolatedThread -count=1 -v`
- `go test ./... -count=1`
- `go vet ./...`
- `go test -race ./internal/codex/appserver ./internal/desktopagent ./internal/relay ./protocol -count=1`
- `npm test -- --run`
- `npm run build`
- `npm run test:ui`：28/28 通过。

## 边界

- Codex 是标题与 Pinned section 的 SSOT；Relay 不缓存 metadata，Web 只保留当前页面内的有界缓存。
- mutation 不自动重试；写入后无法验证时返回 unknown，并只触发安全的列表刷新。
- 未实现置顶拖拽排序、自定义 section、批量操作和标题历史。
