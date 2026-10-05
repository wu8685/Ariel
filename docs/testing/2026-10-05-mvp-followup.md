# MVP 后续验证（2026-10-05）

## A05：owner 主动更新至 Web

扩展 `TestRealAgentRoutesHistoryAndSnapshotThroughRelay`：Web 通过真实 loopback WebSocket 收到原会话的 `thread.snapshot` 后，不向 Web 发送任何 mutation；模拟原 owner 的 follower 将状态改为 `inProgress` 并发出更新。测试核对 Agent → Relay → Web 最终送达同一订阅的 `thread.update`，`baseSeq=1`、`seq=2` 且 runtime 为 `inProgress`。

- 目标测试 `go test -count=20 ./internal/desktopagent -run '^TestRealAgentRoutesHistoryAndSnapshotThroughRelay$'`：通过。
- 全仓 `go test -race -count=1 ./...` 与 `go vet ./...`：通过。
- Web `npm test -- --run`：9 个测试文件、52 项通过；`npm run build`：通过。

这证明当前 Agent／Relay 的下行订阅链路在 owner 主动变化时可工作，但 owner 在该测试中是隔离 fake。真实 Codex Desktop UI 发消息时 Web 同步的 A05 端到端验收仍未完成；不能把此测试算作物理手机或 Desktop UI 实测。

### 真实 Desktop UI → 已订阅 Web（后续补验）

2026-10-05 在带 `.ariel-fixture` guard 的隔离 fixture 上，先用只读探针核对 Desktop 原 owner 可订阅、持久化历史为 2 个 completed turn／4 个 item。仅监听 `127.0.0.1:8090` 的测试 Relay／Agent 与 in-app browser 已连通，Web 已点选原 fixture 并显示同样的 2 个 turn；此时页面未发任何变更请求。

用户随后在 **Codex Desktop UI** 中对该 fixture 发送一条明确禁止工具的短消息。保持同一 Web 标签页和订阅、不刷新页面，Web 从 2 个 turn 更新为 3 个 completed turn，并显示该条消息及 Codex 的精确回复。再次独立运行只读 `probe history --thread`，原 Codex 持久化历史为 3 个 completed turn／6 个 item。测试前后的 Web 会话标题与工作目录后缀一致；未在 Web 重发。由此完成 A05 的真实 Desktop UI→Web 方向浏览器验收；物理手机的同方向可见性仍单列 A22，不由此推定。

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

## A22：物理手机续聊与输入区

用户在真实手机 Safari 上以原固定连接码登录 LAN Relay，隔离 fixture 的最新 3 个 turn 与 Desktop 实时回复可见，点选会话后侧栏自动收起。键盘 Return 只换行，九行草稿使输入框封顶并可内滚；聚焦时暴露的横向溢出经 16px 手机输入字号修复，用户刷新后确认按钮完整可见，详见 [0014 专项记录](2026-10-05-0014-mobile-composer.md)。

随后用户从手机向同一 fixture 点击发送一条禁止工具的短消息。保持另一 Web 页面订阅且不刷新，该页面由 3 个增长到 4 个 completed turn 并显示精确回复；独立原历史探针为 4 个 completed turn／8 个 item。由此手机→原 owner→另一 Web 的实时方向已通过；不能以发送通过替代原生审批、补充回答和 Desktop 重启的验收。

用户再次刷新手机页面后确认：空草稿的发送箭头为灰色且不可点击，输入文字后才变蓝。清空草稿、将 Safari 切到后台约 30 秒再返回，Relay 恢复连接，同一 fixture 的 4 个 turn 仍可见。该结果覆盖一次短暂后台恢复，不代表长时间休眠或弱网切换。

## A10：手机处理真实命令审批

另建受 `.ariel-fixture` guard 保护的专用 `command-accept` fixture。创建命令的 seed 探针超时并尝试中断；随后独立读取原历史，确认 seed 已正常完成、只含预期短回复，未有命令执行。网页点选该 fixture 后自动加载原 owner；探针确认当前无待处理请求，再触发一次只请求 `/usr/bin/true` 的真实命令审批。已订阅 Web 显示 `Read Only / on-request` 和命令 `/bin/zsh -lc /usr/bin/true` 的待审批卡片。

用户在物理手机上核对隔离目录和精确命令后点“仅本次允许”，反馈卡片消失并显示退出状态 0。另一已订阅 Web 同步清除卡片、会话回到待命；独立 Codex 历史确认第二个 turn completed，唯一 `commandExecution` 是上述命令且 `exitCode=0`，最终回复为 `0`。此结果覆盖手机的单次命令允许路径，不等于文件审批、拒绝或权限请求也已经过手机实测。

## A12：手机补充回答与输入聚焦

另建专用 `user-input` fixture，原历史核对 seed 只有预期回复。通过 Web 点选自动加载原 owner 后，受保护探针触发真实两题 `request_user_input`：第一题颜色选项，第二题允许自由文本。用户在物理手机上提交指定选项和自由文本，反馈卡片消失并看到精确结束标记；另一已订阅 Web 同步清除卡片并显示标记，独立 Codex 历史显示对应 turn completed。手机提交链路通过。

本轮同时暴露 iPhone Safari 聚焦问题输入框时页面自动放大：`.question` 原字号为 12px，输入框继承了该值。针对已批准的手机交互规格，先新增移动端问题输入框／下拉框最小 16px 测试并看到预期失败，再将控件字号设为 16px，同时约束最小和最大宽度。Web 67/67 测试、构建通过；390px 本机浏览器聚焦后页面没有横向溢出、两题输入框右缘为 351px。用户刷新手机后再次聚焦两题，确认不再放大；再次提交测试答案，另一 Web 卡片消失，独立 Codex 历史确认第 3 个 turn completed 并给出预期结束标记。

## Fixture seed 探针的 0013 分页回归

两次新 fixture 创建命令都曾误报 `seed did not finish`，但独立 Codex 历史显示 seed 实际已按要求完成。原因是 0013 之后普通 `thread/read` 不再返回 turns，而 `SeedFixture` 仍用普通读取轮询，直到超时。新增模拟默认历史分页的测试先红；将该仅用于隔离测试的校验改用显式 `ReadFull` 后定向测试通过。全仓 `go test -race -count=1 ./...`、`go vet ./...` 均通过。此修复不改变生产 Agent 的有界分页行为。
