# 0081：弱网下 WebSocket heartbeat 容错

- 状态：Draft（等待用户确认）。
- 来源：0080 已把会话菜单的 Desktop Agent 总预算调整为 30 秒，但用户复验仍在约 10 秒看到超时。现场测量显示本机到公网 Relay 存在 60% ICMP 丢包、约 367 ms RTT，631 KB HTTPS 资源下载约 30 秒；同期 Desktop Agent 日志持续出现 heartbeat pong deadline、TLS handshake timeout 和 WebSocket 重连。服务器 CPU、内存与容器健康状态正常。
- 范围：提高 Relay、Desktop Agent 的 WebSocket heartbeat 与单帧发送在弱网下的容错；部署本机 Agent 和公网 Docker Relay／Web。不会改变业务请求幂等语义，也不会无限延长会话菜单等待。

## 已确认原因

1. 公网 Web bundle 已包含 `thread.list = 40s` 的本地等待窗口。
2. 公网 Relay 镜像 `5b2fff9` 已包含 `thread.list = 35s` 的 route timeout，不会在 10 秒主动截断长读取。
3. 本机 Desktop Agent 已运行 0080 的 `15s attempt / 30s total` 新二进制。
4. Relay 与 Desktop Agent 当前默认每 20 秒发起一次 Ping，并在单次 5 秒内没有收到 Pong 时立即关闭连接。
5. 连接关闭会使 Web 的 pending 请求提前结束；会话列表把该链路失败呈现为加载超时，因此业务请求仍可能早于 30 秒终止。
6. `coder/websocket` 的 `Ping` 超时只移除当前 active ping；同一连接允许后续再次 Ping，因此可以在不破坏连接状态的前提下采用连续失败阈值。

## 输入与输出

- 输入：Relay 与 Web／Desktop Agent 之间已经建立的 WebSocket 连接，以及周期 heartbeat 与业务响应帧。
- 成功输出：短暂丢包或单次 Pong 超时不再立即拆除连接；业务请求仍由各自既有的 30／35／40 秒预算决定结果。
- 失败输出：连续 heartbeat 失败达到阈值，或业务帧在扩大后的有界发送窗口内仍无法写出时，关闭连接并沿用现有重连／超时语义。

## 行为与不变量

1. Relay 与 Desktop Agent 的默认 heartbeat interval 调整为 30 秒，单次 Pong timeout 调整为 15 秒。
2. 单次 heartbeat 失败只增加连续失败计数，不立即关闭连接；连续失败达到 2 次才判定连接失活。
3. 任意一次 heartbeat 成功必须把连续失败计数清零。
4. 调用方 context 取消时立即退出 heartbeat，不计作失败。
5. Relay 与 Desktop Agent 的单帧发送 deadline 从 5 秒调整为 15 秒；超时后仍按现有逻辑关闭连接或丢弃本次回执，不建立离线队列。
6. 业务请求预算保持 0080 的 `Agent 30s < Relay 35s < Web 40s`；heartbeat 不得重置或延长业务请求 deadline。
7. 写操作断线后仍不得自动重放；其不确定结果继续表达为 `OUTCOME_UNKNOWN`。
8. 所有 heartbeat、发送等待和重连仍有界；不得以无限等待掩盖永久断线。

## 边界条件与错误处理

- 第一次 Ping 在 15 秒超时、下一次成功：连接继续使用，失败计数归零。
- 连续两次 Ping 失败：主动关闭连接，让现有 reconnect 恢复。
- Ping 返回永久连接错误：仍计入连续失败阈值；Reader 已先发现连接关闭时可立即退出，不必等待阈值。
- 发送业务帧超过 15 秒：Relay 关闭对应慢 consumer；Desktop Agent 返回发送失败并由连接生命周期处理，不积压无界消息。
- 公网链路持续不可用超过业务预算：页面仍显示有界错误，不承诺弱网下一定成功。

## TDD 与验收

1. Red：抽出可注入 Ping 函数的 heartbeat loop，先写失败测试，覆盖“第一次失败不退出、第二次失败退出、成功清零、context cancel 立即结束”。
2. Green：Relay 与 Desktop Agent 复用同一连续失败语义，并调整生产默认 interval、timeout 与发送 deadline。
3. 运行 Go 全包、相关 race test 与构建。
4. 本机重启 Desktop Agent，公网沿用现有 Docker 状态目录构建并替换 Relay／Web 镜像；不得重建 Passkey 凭据或 token。
5. 检查公网 health、容器状态与 Agent 连接。
6. 弱网回归：确认单次 heartbeat timeout 不再触发断线；会话菜单不再在约 10 秒因 heartbeat 被提前终止。
7. 物理手机／实际网络条件保留用户复验；若 30～40 秒业务预算仍耗尽，另行定位 Codex App Server 或持续网络故障。

## 非目标

- 不继续放大 `thread.list` 的 30／35／40 秒预算。
- 不引入消息持久化、离线队列或自动 mutation replay。
- 不把网络持续不可达伪装成业务成功。
- 不修改会话缓存、unread、图片发送或新建会话语义。
