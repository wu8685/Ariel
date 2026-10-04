# MVP 后续验证（2026-10-05）

## A05：owner 主动更新至 Web

扩展 `TestRealAgentRoutesHistoryAndSnapshotThroughRelay`：Web 通过真实 loopback WebSocket 收到原会话的 `thread.snapshot` 后，不向 Web 发送任何 mutation；模拟原 owner 的 follower 将状态改为 `inProgress` 并发出更新。测试核对 Agent → Relay → Web 最终送达同一订阅的 `thread.update`，`baseSeq=1`、`seq=2` 且 runtime 为 `inProgress`。

- 目标测试 `go test -count=20 ./internal/desktopagent -run '^TestRealAgentRoutesHistoryAndSnapshotThroughRelay$'`：通过。
- 全仓 `go test -race -count=1 ./...` 与 `go vet ./...`：通过。
- Web `npm test -- --run`：9 个测试文件、52 项通过；`npm run build`：通过。

这证明当前 Agent／Relay 的下行订阅链路在 owner 主动变化时可工作，但 owner 在该测试中是隔离 fake。真实 Codex Desktop UI 发消息时 Web 同步的 A05 端到端验收仍未完成；不能把此测试算作物理手机或 Desktop UI 实测。

## 当前网页的只读手机尺寸检查

本机 8082 入口提供最新构建。在 in-app browser 的临时标签以 390×844 视口打开未登录页，两个 Ariel 品牌图形的计算颜色均为 `rgb(180, 192, 207)`，截图目测为灰色；页面仍显示未连接状态。临时视口已恢复、标签已关闭。未输入 PIN 或操作真实会话，因此这项检查不能证明物理手机 Safari 缓存已更新，也不能覆盖已连接会话页。

另在仅监听 `127.0.0.1:8094` 的临时 Relay 和 Mock Agent 上，用测试 PIN 连接 390×844 页面：设备与两条 Mock 会话出现；点选会话后侧栏自动收起；只向 Mock 发送一条无敏感数据的测试消息，页面显示运行中到完成和完整回复；重新打开侧栏再点遮罩可收起；同一标签页刷新后无需重输 PIN 即恢复连接，重新点选会话可看到原 Mock 消息。已连接侧栏中的 logo 同样计算为 `rgb(180, 192, 207)`。临时标签、视口覆盖与两进程均已关闭，8094 不再监听。截图仅在本机忽略版本控制的 `.local/` 中。这些结果不代表真实 Desktop 或物理手机验收。
