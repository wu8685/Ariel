# 0081 弱网 WebSocket heartbeat 容错验收

- 日期：2026-10-13
- 规格：[`0081-loss-tolerant-websocket-heartbeat.md`](../specs/0081-loss-tolerant-websocket-heartbeat.md)
- 结论：自动验证、本机 Agent 与公网 Docker Relay 部署通过；用户侧会话菜单复验待完成。

## 根因证据

现场测量本机到 `www.kan.chat` 的链路：

```text
20 packets transmitted, 8 packets received, 60.0% packet loss
round-trip min/avg/max/stddev = 366.869/367.428/367.822/0.302 ms
asset status=200 total=29.916970s size=631449 speed=21106
```

同期 Desktop Agent 日志持续出现 `failed to wait for pong: context deadline exceeded`、TLS handshake timeout 与 WebSocket reconnect。服务器负载约 0、Relay／Caddy CPU 均约 0.02%，内存仍有约 1 GiB available，排除服务器资源耗尽。

公网 Web bundle 已确认 `thread.list` 本地预算为 40 秒，公网 Relay 旧镜像已确认长读取 route timeout 为 35 秒，本机 Agent 已运行 0080 的 30 秒总预算。因此约 10 秒失败来自 heartbeat／链路提前拆除，而不是仍存在的业务 10 秒计时器。

## TDD 证据

Red 阶段先增加共享 heartbeat 状态机测试，旧代码因以下符号不存在而失败：

```text
undefined: runWithTicks
undefined: DefaultInterval
undefined: DefaultTimeout
undefined: DefaultMaxConsecutiveFailures
undefined: DefaultWriteTimeout
```

Green 阶段覆盖并通过：

- 第一次 Ping 失败不退出；
- 成功 Ping 清零连续失败计数；
- 连续第二次失败才返回错误；
- 父 context 取消后立即正常结束；
- 生产默认值固定为 `30s / 15s / 2 failures / 15s write`。

Docker 首次构建真实发现 Dockerfile 未复制新 package 并失败；补充 `COPY internal/wsheartbeat/` 后公网镜像构建通过。旧容器在整个失败阶段保持运行。

## 自动验证

以下命令通过：

```sh
go test ./internal/wsheartbeat ./internal/desktopagent ./internal/relay -count=1
go test -race ./internal/wsheartbeat ./internal/desktopagent ./internal/relay
go test ./...
go build -trimpath -o .local/bin/ariel-desktop-agent.next ./cmd/desktop-agent
go build -trimpath -o .local/bin/ariel-relay.next ./cmd/relay
cd web && npm test -- --run && npm run build
```

Web 结果为 24 个 test file、195 个 test 全部通过；Vite 生产构建通过。

## 部署验证

本机 Desktop Agent 已替换并通过 launchd 重启：

```text
state = running
last exit code = 0
TCP <local>:<ephemeral> -> 47.79.252.104:443 (ESTABLISHED)
```

当前是公网 agent-only 模式；`GO111MODULE=on ./scripts/ariel.sh show-pin` 按设计返回 `show-pin is only available in local mode`，没有 LAN 地址或 6 位连接码。

公网采用可回滚替换：

```text
NEW ariel-relay:c226db2 running ecs
BACKUP ariel-relay:5b2fff9 exited
ariel-relay:c226db2 status=running health=healthy
```

公网 `/healthz` 返回 200。Relay 切换导致一次预期 EOF 后，连续观察约 107 秒（超过两个 30 秒 heartbeat 周期），本机 Agent 保持同一条 ESTABLISHED 连接，日志没有新增 heartbeat 断线。

## 保留复验

- 用户在原来复现问题的浏览器和网络中刷新会话菜单，确认不再约 10 秒提前失败。
- 若仍在 30～40 秒耗尽，则继续检查 Codex App Server 响应时间或持续网络不可达；不得再次把它归因于前端 10 秒 timeout。
