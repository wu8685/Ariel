# 0028 手机输入框与操作栏上下分层测试记录

- 日期：2026-10-07
- 关联规格：[0028 手机输入框与操作栏上下分层](../specs/0028-stacked-mobile-composer-actions.md)
- 来源：用户希望手机端停止按钮只显示 icon，并参考 Codex 将附件、停止和发送操作放到文字输入区下方。

## TDD

1. Red：新增 DOM 契约后，停止按钮缺少精确的 `停止` 可访问名称、独立 icon 和提示；新增移动样式契约后，composer 仍为横向布局。定向测试出现 2 个预期失败，其余 58 项通过。
2. Green：停止按钮拆为 icon 与文字两个节点，手机端隐藏文字但保留 `aria-label`／`title`；composer 改为上下两层，textarea 独占上层宽度，下层用 `margin-right: auto` 将附件与停止／发送分置两侧。
3. 回归：桌面端仍显示“停止”文字；发送、中断、截图、输入高度与 streaming 稳定性语义不变。

## 自动验证

- `npm test -- --run src/App.test.tsx src/theme.test.ts`：2 个文件、60 项测试通过。
- `npm test`：14 个文件、105 项测试通过。
- `npm run build`：通过；Vite 仅报告既有的主 chunk 大小提示。
- `GO111MODULE=on go test ./...`：全包通过。

## 390×844 隔离浏览器验收

- textarea 宽度为 368px，等于 composer 370px 扣除两侧边框；停止按钮出现前后宽度不变。
- textarea 高度为 44px；下层操作栏从 textarea 底部开始，高度为 48px，没有覆盖输入文字。
- 附件按钮位于左侧；停止与发送按钮位于右侧，三个按钮均为 44×44px。
- 手机端停止文字的计算样式为 `display: none`，可见内容只有方形停止 icon；按钮提示和可访问名称仍为“停止”。
- 文档 `clientWidth` 与 `scrollWidth` 都是 390px，没有横向溢出。
- 隔离 Mock 会话未操作真实 Codex 历史；物理手机上的视觉和触感由用户在本地部署后最终复验。
