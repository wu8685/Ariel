# 0029 手机输入字号与会话正文一致测试记录

- 日期：2026-10-07
- 关联规格：[0029 手机输入字号与会话正文一致](../specs/0029-mobile-composer-font-size.md)
- 来源：用户在物理手机上观察到 composer 输入文字明显大于上方对话正文。

## TDD

1. Red：样式契约要求 textarea 与 `.message-text` 都为 14px，旧实现实际为 16px，定向测试得到预期失败。
2. Red：新增 iPhone、iPad desktop mode、Android、桌面端与幂等性测试；实现前因 viewport 模块不存在而按预期失败。
3. Green：手机 textarea 调整为 14px；应用启动时只对 iOS viewport 写入唯一的 `maximum-scale=1`，Android 与桌面端保持原值。
4. 回归：24px 行高、一行 44px、八行 212px、0028 上下分层和桌面 composer 均不变。

## 自动验证

- `npm test -- --run src/theme.test.ts src/viewport.test.ts`：2 个文件、14 项测试通过。
- `npm test`：15 个文件、108 项测试通过。
- `npm run build`：通过；Vite 仅报告既有的主 chunk 大小提示。
- `GO111MODULE=on go test ./...`：全包通过。

## 390×844 隔离浏览器验收

- iPhone user agent 下，textarea 与用户消息 `.message-text` 的计算字号均为 14px。
- textarea 仍为 24px 行高、44px 单行高度和 368px 宽；composer 仍为 370×94px。
- viewport 为 `width=device-width, initial-scale=1.0, maximum-scale=1`。
- 文档 `clientWidth` 与 `scrollWidth` 都是 390px，没有横向溢出。
- 隔离 Mock 会话未操作真实 Codex 历史；物理 iPhone 的实际聚焦缩放与视觉观感由用户在本地部署后最终复验。
