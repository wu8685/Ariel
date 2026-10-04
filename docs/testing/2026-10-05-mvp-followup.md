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

## 0014 后的当前状态复核

- 本仓最新代码执行 `go test -race -count=1 ./...` 和 `go vet ./...` 均通过；这是自动回归证据，不等于所有未验原生场景已通过。
- 对既有 A05 隔离 fixture 做三次只读 Desktop follower 刷新，均在当前原 owner 中得到可归一化的 idle 状态及两个 completed turn；未向 fixture 或业务会话发送消息。该 fixture 当前可用于下一步 Desktop UI 主动发送后的实时订阅验收。
- [验收矩阵](2026-10-04-mvp-acceptance-audit.md)已补入 0013 真实 47-turn 大会话、0014 隔离浏览器输入区的证据，保留物理手机、真实 Desktop UI→Web 推送、原生权限请求和真实 Desktop 退出／重启等缺口，不将局部通过外推为 MVP 完成。
- 运行中的 0013 Relay 与 Desktop Agent 进程仍存在；本次 `lsof` 只读抽样统计二者可写普通文件 FD 为 0。它支持 A23 的**当前进程**结论，不替代源代码审查，也不保证其他启动方式或未来版本。

## A19：单个历史 item 超页的边界

新增 `TestOversizedSingleHistoryItemFailsOnlyThatPage`：模拟一个超过 Web 历史页预算的 7 MiB `agentMessage` item。`thread.history.items` 的页面限制从 100 逐步缩至 1，最终明确返回 `HISTORY_TOO_LARGE`；随后同一 Agent Service 仍能读取普通历史页。这是单 item 超限隔离的自动回归，不是当前 Desktop 原生大 diff 或物理手机的端到端验收。
