# MVP 后续验证（2026-10-05）

## A05：owner 主动更新至 Web

扩展 `TestRealAgentRoutesHistoryAndSnapshotThroughRelay`：Web 通过真实 loopback WebSocket 收到原会话的 `thread.snapshot` 后，不向 Web 发送任何 mutation；模拟原 owner 的 follower 将状态改为 `inProgress` 并发出更新。测试核对 Agent → Relay → Web 最终送达同一订阅的 `thread.update`，`baseSeq=1`、`seq=2` 且 runtime 为 `inProgress`。

- 目标测试 `go test -count=20 ./internal/desktopagent -run '^TestRealAgentRoutesHistoryAndSnapshotThroughRelay$'`：通过。
- 全仓 `go test -race -count=1 ./...` 与 `go vet ./...`：通过。
- Web `npm test -- --run`：9 个测试文件、52 项通过；`npm run build`：通过。

这证明当前 Agent／Relay 的下行订阅链路在 owner 主动变化时可工作，但 owner 在该测试中是隔离 fake。真实 Codex Desktop UI 发消息时 Web 同步的 A05 端到端验收仍未完成；不能把此测试算作物理手机或 Desktop UI 实测。

## 当前网页的只读手机尺寸检查

本机 8082 入口提供最新构建。在 in-app browser 的临时标签以 390×844 视口打开未登录页，两个 Ariel 品牌图形的计算颜色均为 `rgb(180, 192, 207)`，截图目测为灰色；页面仍显示未连接状态。临时视口已恢复、标签已关闭。未输入 PIN 或操作真实会话，因此这项检查不能证明物理手机 Safari 缓存已更新，也不能覆盖已连接会话页。
