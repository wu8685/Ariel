# 0139 Puck 引用消息回复验证记录

- 日期：2026-11-08
- 分支：`feat/puck-reply-to-message`
- 基线：`origin/feat/puck-authorization-always-allow`
- 规范：`docs/specs/0139-puck-reply-to-message.md`

## 依赖

Turn 48、49、50 尚未进入 `origin/main`，因此本分支基于 Turn 50；Turn 50 已包含 Turn 49 与 Turn 48 的等价提交。合并本变更前应先处理这些前置分支。

## TDD 记录

### Red

- Go：新增 store、Module、protocol 用例后，因 `Message.ReplyTo`、`Store.Message` 与协议字段尚不存在而失败。
- Vitest：新增引用选择、发送、跳转、取消、切换 Bot 清理、移动端左滑与按需加载用例后失败。

### Green / regression

| 检查 | 结果 |
| --- | --- |
| `go test ./...` | 通过 |
| `go test -race ./...` | 通过 |
| `go vet ./...` | 通过 |
| `go build ./...` | 通过 |
| `npm test` | 通过，47 files / 418 tests |
| `npm run test:ui` | 通过，189 tests |
| `npm run build` | 通过；仅保留既有 chunk-size warning |
| `go test ./cmd/relay ./internal/puck ./protocol`（最终审计） | 通过 |
| `git diff --check` | 通过 |

## 浏览器验收

- Desktop Chromium：hover 回复入口、发送带 `replyTo` 的消息、点击引用块跳转及短暂高亮通过。
- 390px Chromium touch viewport：左滑进入回复、毛玻璃引用预览、取消与发送通过；无水平溢出。
- 全量 Chromium 回归 189/189 通过。
- 390px 截图：`web/e2e/puck-workspace.spec.ts-snapshots/puck-reply-preview-390-dark-darwin.png`。
- 因 focus-visible 回复入口改变，更新了 1280px light、dark 与 feature-parity 三张既有基线。
- 物理 iPhone / Safari 尚未验证，不能用 Chromium touch viewport 替代该结论。

## 本地真实端到端

真实 Codex Desktop 隔离 smoke 已运行两次。隔离 Desktop Agent 可上线并声明 Puck-ready，但 `puck.create` 在新建隔离 Brain thread 时收到 `RESYNC_REQUIRED`，因此测试在创建 Bot 阶段停止，尚未执行“引用助手消息”和“引用任务卡后沿用原任务”两条真实链路。

该结果记为 **unverified**，不视为实现通过或产品失败。确定性覆盖仍包括：

- Go integration 验证 `replyTo` 持久化、同 Bot/owner 校验、Brain 引用上下文、任务卡 `taskId` 与 `send_turn` 路由不新建任务；
- Vitest 验证 Web 请求、预览、加载、跳转和移动端手势；
- Playwright 验证真实 Chromium DOM、交互与几何关系。

后续在 Codex Desktop IPC compatibility 恢复后，重新运行 opt-in smoke，再进行物理 iPhone / Safari 左滑复验。
