# 0078 会话菜单刷新超时预算验收

- 日期：2026-10-13
- 规格：[`0078-sidebar-refresh-timeout-budget.md`](../specs/0078-sidebar-refresh-timeout-budget.md)
- 结论：Desktop Agent 的 `thread/list` 单次 deadline 已从 3 秒调整为 5 秒；重试次数、Relay／Web 总预算与写操作语义均未改变。

## TDD 证据

Red 阶段先新增生产配置测试：

```text
--- FAIL: TestProductionThreadListAttemptTimeout
thread/list attempt timeout = 3s, want 5s
```

实现只修改生产 deadline 后，同一测试转绿：

```text
ok github.com/wu8685/Ariel/internal/desktopagent
```

## 自动验证

以下命令通过：

```sh
go test ./internal/desktopagent ./internal/codex/appserver ./internal/relay
go test -race ./internal/desktopagent ./internal/codex/appserver ./internal/relay
go test ./...
go build -trimpath -o .local/bin/ariel-desktop-agent.next ./cmd/desktop-agent
```

验证范围包括：

- 生产单次 deadline 固定为 5 秒；
- 首次 `thread/list` 超时后只重建一次 endpoint；
- 第二次超时后停止，不发生第三次尝试；
- Desktop Agent、App Server 与 Relay 的 race 回归；
- 全仓 Go 测试与真实入口构建。

## 本机部署验证

新二进制已原位替换并通过 launchd 重启 `chat.kan.ariel.desktop-agent`：

```text
state = running
last exit code = 0
TCP <local>:<ephemeral> -> 47.79.252.104:443 (ESTABLISHED)
```

当前实例是连接公网 Relay 的 agent-only 模式。按本地部署约定执行 `GO111MODULE=on ./scripts/ariel.sh show-pin` 后，脚本明确返回 `show-pin is only available in local mode`；因此本次不存在可展示的 LAN 地址或 6 位连接码。

## 保留的人工复验

- 物理手机在慢速或冷启动场景下刷新会话菜单，确认原先 3 秒附近出现的提前 `TIMEOUT` 不再复现。
