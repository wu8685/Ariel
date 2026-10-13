# 0080 会话菜单刷新总预算验收

- 日期：2026-10-13
- 规格：[`0080-thread-list-overall-timeout-budget.md`](../specs/0080-thread-list-overall-timeout-budget.md)
- 结论：自动验证与本机公网 Agent 部署通过；用户侧慢请求复验待完成。

## 根因复核

本机运行状态确认当前只有连接公网 Relay 的 Desktop Agent，没有 local Relay。原生产值为单次 5 秒且最多两个 attempt，因此首个 recent list 连续无响应时会在约 `5s + 5s = 10s` 返回 `TIMEOUT`。这与用户在本机浏览器观察到的约 10 秒一致，不是浏览器缓存或旧 local Relay 导致。

## TDD 证据

Red 阶段先将生产期望改为单次 15 秒、总预算 30 秒，并要求只有普通菜单刷新获得该父 deadline。旧实现编译失败，缺少 `threadListRequestTimeout` 与 `threadListRequestContext`：

```text
undefined: threadListRequestTimeout
undefined: threadListRequestContext
```

Green 阶段增加共享父 context 并调整单次 deadline 后，定向测试通过：

```text
ok github.com/wu8685/Ariel/internal/desktopagent
```

测试同时约束：

- 单次 attempt 为 15 秒；
- 整次菜单刷新为 30 秒；
- 两个 attempt 的上限恰好等于总预算；
- 带搜索词的 `thread.list` 不套用菜单刷新专用 deadline。

## 自动验证

以下命令通过：

```sh
go test ./internal/desktopagent ./internal/codex/appserver ./internal/relay
go test -race ./internal/desktopagent ./internal/codex/appserver ./internal/relay
go test ./...
go build -trimpath -o .local/bin/ariel-desktop-agent.next ./cmd/desktop-agent
```

## 本机部署验证

新二进制已原位替换并通过 launchd 重启 `chat.kan.ariel.desktop-agent`：

```text
state = running
last exit code = 0
TCP <local>:<ephemeral> -> 47.79.252.104:443 (ESTABLISHED)
```

当前实例为公网 agent-only 模式。执行 `GO111MODULE=on ./scripts/ariel.sh show-pin` 后，脚本按设计返回 `show-pin is only available in local mode`，不存在可展示的 LAN 地址或 6 位连接码。

## 保留复验

- 在用户实际出现慢加载的本机浏览器中刷新会话菜单，确认不再在约 10 秒提前显示 `TIMEOUT`。
- 如果完整 30 秒仍失败，应把问题归为 App Server 连续不可用，而不是继续无上限调大 Web／Relay 等待窗口。
