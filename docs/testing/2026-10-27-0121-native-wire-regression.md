# 0121 补充：生产限额下的原生大帧回归

Issue #41，补足 [0121](../specs/0121-native-state-encoding-reconnect.md)／[上一轮验收](2026-10-27-0121-native-state-reconnect.md) 的覆盖缺口。本次仅新增测试，不修改生产实现、协议或限额，不进行部署。

## 测试链路与断言

原有浏览器用例经过真实 Agent 与 Relay，但 native owner 是假 Live，HTML 编码故障使用缩小的 Reducer 限额复现。新增测试通过独立 Unix socket 模拟原生 owner，实际执行生产 `OpenFollower` 的普通 8 MiB → 兼容 64 MiB fallback、framing、streaming projection、Follower 与 Reducer；浏览器再走真实 Agent、Relay 和 Web。

合成帧在内存中生成，大小 **13,108,088 bytes**（约 12.5 MiB）：

- 普通未知字段中有 1.5 MiB `<>&`，必须逐字保留，不能当作 lazy 字段删除。
- 已验证 MCP tool result 中有约 10 MiB `<>&`，内部含合法 `type: 42`；图片为约 1 MiB data URI，均应被已有 projection 转为 lazy 占位。
- 初始正文、工具摘要、用户输入和原生状态结构保留，处理后的 resident state 为 **1,573,499 bytes**，不调高生产限额。

| 层次 | 新增用例／断言 |
| --- | --- |
| IPC → Agent | `TestNativeLargeFrameOpensAndPatchesAtProductionLimits`：真实生产限额、恰好两次 native 连接／load、未知字段逐字不变、tool 与 image 各自变为 lazy、Agent normalization 可用、正文连续消费两次原生 patch |
| Agent → Relay → Chromium | `native-wire-reconnect.spec.ts`：首次正文可见、增量与完成状态自动到达、草稿保留、发送可用、没有只读／重新连接提示、没有额外 attach／load、没有整页导航或横向溢出 |
| 安全 | 独立临时 Unix socket、loopback Relay；任何非白名单 native 请求拒绝并计数，测试断言计数为零，不连接真实 Codex、不发送真实消息 |

fixture 的恢复 guard 延迟为 30 秒，断言在此之前完成，验证的是 socket 主动 patch，而不是靠恢复轮询拉取完成状态。所有大内容仅在运行时生成，不提交真实快照或大文件。

## 实际 Red → Green

使用 `git archive v0.11.2` 建立隔离源码副本，只复制本次测试及 native fixture；不回退工作区或重启已部署进程。

| 隔离版本 | 实际结果 |
| --- | --- |
| v0.11.2 原实现 | 新 Go 用例首次打开失败：`production-limit native cold open failed: IPC connection closed`；同一浏览器用例等待正文 12 秒后失败，页面进入“历史只读／重新连接” |
| 只恢复旧 projection，保留新编码与错误传播 | resident state 为 **13,107,834 bytes**，新增 `<2 MiB` 断言失败 |
| 只恢复旧 HTML 编码，保留新 projection 与错误传播 | resident state 为 **9,437,819 bytes**，新增 `<2 MiB` 断言失败 |
| 当前实现 | 不改生产代码即通过；resident state 约 1.5 MiB，两个 patch 按序到达，最终完成状态展示，无需重连 |

两个独立回退也被拦住，避免只修复其中一处后让组合测试意外变绿。旧实现的通用 `IPC connection closed` 本身正是上一轮修复的 typed error 丢失问题，不能把该文案当成网络故障的证据。

## 验证结果

- Go 全量、`-race` 全量、`go vet ./...` 通过；新增 IPC 用例在 `-race -count=10` 下连续通过。
- Web 40 文件／318 单测、production build 通过，既有 chunk-size 警告不变。
- Chromium 全量 **100 项通过**，包含此前全部 99 项与新增原生大帧用例，既有截图没有变化。
- 新增浏览器用例再以 `--repeat-each=3` 连续通过；fixture 用 `go build -race` 构建，无 data race。

## 复跑

仓库根目录：

```sh
go test ./...
go test -race ./...
go vet ./...
go test -race ./internal/desktopagent -run '^TestNativeLargeFrameOpensAndPatchesAtProductionLimits$' -count=10
```

Web 目录（先构建 fixture 要提供的静态资源）：

```sh
npm run build
npm test -- --run
npx playwright test e2e/native-wire-reconnect.spec.ts --workers=1
npm run test:ui -- --workers=1
```

若要重现 Red，在隔离 v0.11.2 副本复制 `internal/codex/desktopipc/testfixture/`、`internal/desktopagent/native_wire_regression_test.go` 和 `tests/fixtures/cache-recovery/main.go`，先运行上述 Go 专项，再将该副本的 `./tests/fixtures/cache-recovery` 构建为独立 binary。当前 Web 用例可通过 `ARIEL_NATIVE_WIRE_FIXTURE_BINARY=/absolute/path/to/old-fixture` 指向它，浏览器断言与当前版本完全相同；此环境变量仅为测试入口，不会影响生产 Agent。

本次回归是 Chromium 的 390×844 viewport／touch 模拟和隔离 native peer，不冒充物理 iPhone／Safari 或新的真实 Codex 兼容性实测；上一轮真实只读验证的范围不扩大。
