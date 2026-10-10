# 0063 Agent 重连状态 fencing 与会话恢复验收记录

- 日期：2026-10-11
- 范围：Relay device registry 广播、Web epoch／subscription recovery、Desktop Agent heartbeat 诊断与 reconnect backoff。
- 结论：代码竞态与恢复遗漏已修复并通过自动化；公网首次断链的底层网络／代理原因仍需部署后依靠新增诊断继续观察。

## TDD 证据

实现前先加入 Relay、Desktop Agent 和 Web 回归测试：

```text
go test ./internal/relay ./internal/desktopagent ./cmd/desktop-agent
cd web && npm test -- src/App.test.tsx
```

Red 结果：

- Relay 缺少当前 registry 状态生成逻辑，测试因 `currentDeviceStatus` 不存在而编译失败；
- reconnect backoff 缺少成功握手后的 reset，测试因 `newReconnectBackoff` 不存在而编译失败；
- heartbeat 测试仅取得 `failed to get reader: use of closed network connection`，无法识别 heartbeat failure；
- Web 的 offline late-event 与 recovery generation 两项测试失败；新增 epoch 在旧 schema 中还会被视为非法字段并关闭 WebSocket。

最小实现后，同一组测试转绿。覆盖行为包括：

- 旧 peer 清理时 registry 已有 replacement，生成的状态仍为 replacement epoch online；
- 当前 offline 原子废弃旧 subscription，迟到 update 不触发 seq-gap resync；
- 恢复 subscribe 进行中出现更新 online epoch，首次失败后只执行一次最新 generation 恢复；
- 成功 hello 后 backoff 回到 1 秒，连续失败仍按 1、2、4、8、15 秒增长并封顶；
- heartbeat failure 在返回错误中保留明确原因。

## 真实浏览器验收

`web/e2e/mobile-agent-reconnect.spec.ts` 使用隔离 WebSocket fixture，在真实 Chromium 手机 viewport 中执行：

1. 当前会话收到旧 epoch offline；
2. 新 epoch online；
3. 旧 subscription update 与旧 epoch offline 迟到；
4. fresh owner snapshot 恢复。

断言只发生一次恢复订阅、用户草稿保留、fresh snapshot 恢复，且不再出现第二次 seq-gap notice。该测试不向真实 Codex 会话发送消息，也不把 Chromium 冒充物理 iPhone／Safari。

## 全量回归

通过：

```text
GO111MODULE=on go test ./...
GO111MODULE=on go vet ./...
GO111MODULE=on go test -race ./internal/relay ./internal/desktopagent ./cmd/desktop-agent
cd web && npm test
cd web && npm run build
cd web && npm run test:ui
git diff --check
```

结果：

- Go 全包测试和 `go vet` 通过；
- 涉及并发状态的三个 Go package 通过 race detector；
- Web 22 个测试文件、172 项测试全部通过；
- TypeScript 与 Vite production build 通过；
- Playwright 19 项真实 Chromium 用例全部通过，其中同时覆盖 8 秒只读重连窗口与新 Agent epoch 的迟到事件隔离；
- Vite 仍报告既有的单 chunk 超过 500 kB 警告。
