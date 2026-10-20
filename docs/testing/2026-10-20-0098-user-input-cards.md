# 0098 会话内用户输入卡片验收

- 日期：2026-10-20
- 规格：[0098 会话内用户输入卡片](../specs/0098-conversation-user-input-cards.md)
- 结论：自动化验收通过；物理 iPhone／Safari 待用户复验

## TDD 证据

Red 阶段分别确认了三项旧行为：Desktop normalizer 丢失 `header` 与 option `description`；公共 schema 拒绝结构化 option；Web 仍渲染为 `datalist`，没有触摸式单选项与“其他”输入。

Green 后完成：

- `go test ./...`
- `cd web && npm test`：30 files、238 tests passed
- `cd web && npm run build`
- `cd web && npx playwright test e2e/mobile-user-input-card.spec.ts`

## 真实浏览器行为

Playwright 使用真实 Chromium：

- 390×844、touch viewport：结构化选项、description、自由文本与提交按钮布局正常，无横向溢出。
- 1280×900 desktop viewport：同一交互卡片正常展示。
- 会话 A 填写答案后切换到会话 B，再返回 A，当前页面内答案仍在。
- 两题完整后只发出一次 `interaction.respond`；提交期间按钮显示“正在提交…”且整张卡片禁用。
- response receipt 返回后仍不乐观移除卡片，继续等待 owner 的 live update；既有 App 回归覆盖 exact owner update 后移除。

截图基线：

- `web/e2e/mobile-user-input-card.spec.ts-snapshots/mobile-user-input-card-darwin.png`
- `web/e2e/mobile-user-input-card.spec.ts-snapshots/desktop-user-input-card-darwin.png`

## Native 边界

本次没有重新创建真实 Codex fixture，避免在用户会话历史中增加测试 thread。`interaction.respond` 的真实 owner receipt、exact answer echo 与 turn 继续链路已由 2026-10-05 的专用 `user-input` fixture 验证；本次未修改该传输语义，只扩展问题元数据并替换 Web 呈现。当前版本上的真实 fixture 因此记为未重跑，而不是据此宣称重新验证。
