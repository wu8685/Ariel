# 0064 Agent 只读请求自愈与设备会话列表缓存验收

- 日期：2026-10-11
- 规格：[`0064-agent-read-timeout-and-thread-list-cache.md`](../specs/0064-agent-read-timeout-and-thread-list-cache.md)
- 跟踪：[GitHub Issue #1](https://github.com/wu8685/Ariel/issues/1)

## 原因复现与临时恢复

截图中的“执行结果不确定”来自 `thread.list` 超时路径。Relay 和 Web 都有超时保护，但原 `RestartingRPC` 只在 App Server 子进程退出时重建 endpoint；子进程存活而 RPC 卡死时，请求会持续悬挂并被 Relay／Web 先判为 `OUTCOME_UNKNOWN`。

实施前使用 `launchctl kickstart -k gui/$UID/chat.kan.ariel.agent` 重启本机 Agent，强制创建新的 App Server endpoint。`./scripts/ariel.sh status` 随后确认“Agent 已与 Relay 握手，Codex 就绪”。这只用于即时恢复，长期修复由下面的代码与测试保证。

## 自动测试

### Go

```text
GO111MODULE=on go test ./...
PASS

GO111MODULE=on go test -race ./internal/codex/appserver
PASS
GO111MODULE=on go test -race ./internal/desktopagent
PASS
GO111MODULE=on go test -race ./internal/relay
PASS
```

专项测试验证：

- `thread/list` 首次 endpoint 存活但卡死时在 deadline 后被关闭，第二 endpoint 成功完成请求；
- 两个 endpoint 都卡死时只启动两次，并在有限时间内返回 `context.DeadlineExceeded`；
- queue mutation 不因新机制自动重放；
- Relay 只读超时返回 `rejected/TIMEOUT`，mutation 超时仍为 `unknown/OUTCOME_UNKNOWN`；
- Desktop Agent 把 deadline 映射为协议 `TIMEOUT`。

### Web 与 production build

```text
npm test
23 files, 176 tests passed

npm run build
TypeScript + Vite production build passed
```

专项测试验证：

- Web transport 的 `thread.list` 本地计时器使用 `TIMEOUT`；
- thread-list cache 按设备、Agent epoch 与规范化查询隔离；
- 60 秒后条目变为 stale 但仍可先展示；
- LRU 上限与按设备清理生效；
- A → B → A 切换时 A 的 fresh cache 立即恢复，且不产生第三次 `thread.list`。

## 真实浏览器与截图

```text
npx playwright test
20 passed
```

新增 Chromium mobile fixture 在 390×844、touch、dark mode 下执行 A → B → A：切回 A 后项目和会话在同一帧恢复，250ms 后 `thread.list` 总请求数仍为 2。

截图基线：

- `web/e2e/mobile-thread-list-cache.spec.ts-snapshots/mobile-device-thread-list-restored-darwin.png`

人工检查截图确认：设备选择器已回到 Mac A；项目 `device-a` 与“设备 A 的缓存会话”可见；未出现空列表、错误提示或布局跳变。

## 边界

- 自动测试用可控的 hung endpoint 证明自愈逻辑；公网链路无法在不影响当前使用的前提下强制制造同样卡死。部署后应以实际手机切换设备继续观察日志。
- 缓存只存在于当前页面内，不跨刷新持久化；会话正文仍由 Codex／Desktop Agent 提供，Relay 不新增数据库。
