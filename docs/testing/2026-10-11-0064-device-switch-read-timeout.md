# 0064 设备切换只读 timeout 验收记录

- 日期：2026-10-11
- 范围：Relay 只读 route budget、Web request deadline／设备切换 generation、Desktop Agent deadline 分类。
- 结论：跨层 timeout 倒挂与错误文案已修复并通过自动化；目标物理 Mac 当次请求的准确耗时仍需同期日志才能确认。

## TDD 证据

实现前先加入 Relay、Desktop Agent 和 Web 回归测试：

```text
GO111MODULE=on go test ./internal/relay ./internal/desktopagent
cd web && npm test -- --run src/client.test.ts src/App.test.tsx
```

Red 结果：

- Relay 测试因 `Config.ReadTimeout` 尚不存在而编译失败；
- Desktop deadline 被分类成 `RESYNC_REQUIRED`，而不是 `TIMEOUT`；
- Web 普通 `thread.list` 仍在 12 秒超时；
- 设备 B 的只读 `OUTCOME_UNKNOWN` 仍显示“不要直接重发”的 mutation 文案。

最小实现后，同一组测试转绿。Relay 测试用缩短的时间尺度证明普通列表可以越过旧 request budget，并在 read budget 内取得成功结果。

## 真实浏览器验收

`web/e2e/mobile-device-switch-timeout.spec.ts` 使用隔离 WebSocket fixture 和真实 Chromium 手机 viewport：

1. 同时提供设备 A／B；
2. A 的列表仍在途时切换到 B；
3. 用浏览器虚拟时钟推进 13 秒，跨过旧 Web 12 秒 deadline；
4. 断言 B 仍显示 loading；
5. 注入 A 的迟到 `OUTCOME_UNKNOWN`，断言不污染 B；
6. 注入 B 的成功列表，断言目标会话正常显示。

该用例不访问真实 Codex 会话，也不把 Chromium 冒充物理 iPhone／Safari。

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
- Relay、Desktop Agent 与启动入口通过 race detector；
- Web 22 个测试文件、173 项测试全部通过；
- TypeScript 与 Vite production build 通过；
- Playwright 20 项真实 Chromium 用例全部通过；
- Vite 仍报告既有的单 chunk 超过 500 kB 警告。
