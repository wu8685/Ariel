# 0089 turn 文件链接、选段引用与部署验收

- 日期：2026-10-17
- 规格：[`0089-turn-file-links-and-selection-context.md`](../specs/0089-turn-file-links-and-selection-context.md)
- 结论：Ariel Web／Agent／Relay 与本地 Chilling backend 已按前后端分离方案实现并部署；Go、Web、race、production build、真实移动与桌面 Chromium、真实 Codex 0.160.0 只读兼容性探针均通过。物理 iPhone／Safari 与用户真实会话验收待执行。

## 实现边界

- Web 只在 Relay runtime flag 与目标 Agent capability 同时开启时展示功能；仍复用一条现有多路复用 WebSocket。
- 最近 4 个已完成 turn 批量读取文件变更摘要；文件正文只在点击时读取。浏览器页面内存使用 10 项／8 MiB LRU，设备 epoch 改变或显式退出时清空；Relay 不保存正文。
- Chilling 通过私有 Unix socket 暴露 `file-context/v1`，只使用 Codex App Server 的 public `thread/read`、`thread/items/list`、`fs/getMetadata`、`fs/readFile`。
- 路径必须位于 thread 权威 cwd 内；拒绝 traversal、symlink traversal、目录、二进制、超限文件和 stale digest／mtime／选段。
- Chilling 只验证并生成 canonical context envelope；最终 `turn.start`／`queue.add` 仍由 Desktop Agent 单 owner 发出。

## TDD 与验证证据

- Chilling：`go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./cmd/chilling` 通过。
- 真实 Codex read-only probe：设置当前 thread ID 与 Codex 0.160.0 binary 后，`TestRealCodexFileContextReadOnly` 通过；确认 native absolute path 仅在权威 cwd 内规范化为相对路径。
- Ariel：`go test ./...` 与 `go test -race ./...` 通过。
- Ariel `go vet ./...` 仅被既有、与 0089 无关的 `internal/wsheartbeat/heartbeat_test.go:49` 阻塞：测试 goroutine 内调用 `testing.T.Fatal`。本次未修改该范围。
- Web：`npm test` 通过 28 个文件、225 个测试，包含浏览器 LRU 缓存边界。
- Web production build：`npm run build` 通过，仅保留既有 Vite chunk size warning。
- Playwright：`npm run test:ui` 通过 34 个真实 Chromium 场景；新增场景覆盖 390×844 手机全屏阅读器、选段引用、发送 payload，以及桌面侧栏几何关系并生成截图基线。

## 待复验

- 物理 iPhone／Safari 的 native selection、safe-area 与软键盘行为。
- 启用本机 Chilling 与目标部署 flags 后，在用户真实 `fileChange` turn 上完成一次只读打开、选段和发送验收。

## 部署记录

- 本机 Chilling 使用 launchd 持久运行，`file-context/v1` capability 返回 `ready: true`，build version 为 `e1f2df7`。
- 本机 Desktop Agent 已更新到 Ariel `32bb6ad` 并通过 Chilling handshake；其 ready marker 在新版公网 Relay 接受 hello 后生成。
- 公网 `www.kan.chat` 已运行镜像 `ariel-relay:32bb6ad`，Docker health 与公网 `/healthz` 均为 `ok`，HTML runtime meta 为 `ariel-chilling-file-context=true`。
- 旧镜像容器 `ariel-relay:ad44f9c` 已停止并保留，供人工回滚；未删除历史容器和持久认证数据。
