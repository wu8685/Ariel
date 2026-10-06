# 0030 工具调用折叠文字入口测试记录

- 日期：2026-10-07
- 关联规格：[0030 工具调用折叠文字入口](../specs/0030-tool-activity-text-toggle.md)
- 来源：用户认为折叠后的圆形箭头按钮过大，希望参考 Codex 改成一段稍灰的可点击文字。

## TDD

1. Red：组件契约要求折叠态显示“使用了 3 个工具”且不含 SVG，旧实现实际没有可见文字；样式契约要求无边框、透明背景和 12px 灰字，旧实现仍是 44×44px 圆形按钮。定向测试得到 2 个预期失败，其余 58 项通过。
2. Green：折叠态使用“使用了 N 个工具”；展开态使用“收起工具调用（N 项）”，原 `aria-label`、`title`、`aria-expanded` 和 button 键盘语义保留。
3. Green：入口改为透明无边框的 inline text control，移除 SVG 与旋转样式；工具详情、交错顺序、实时追加和稳定锚点逻辑不变。

## 自动验证

- `npm test -- --run src/App.test.tsx src/theme.test.ts`：2 个文件、60 项测试通过。
- `npm test`：15 个文件、108 项测试通过。
- `npm run build`：通过；Vite 仅报告既有的主 chunk 大小提示。
- `GO111MODULE=on go test ./...`：全包通过。

## 390×844 浏览器样式验收

- 使用 production build 和真实 `.activity-toggle` 样式渲染代表性折叠入口；实际尺寸约 87.2×24px，文案为“使用了 3 个工具”。
- 计算样式为 12px、`rgb(184, 184, 184)`、透明背景、0px 边框，SVG 数量为 0。
- 文档 `clientWidth` 与 `scrollWidth` 都是 390px，没有横向溢出。
- 浏览器样式验收使用隔离 Mock 页面中的代表性 DOM；实际 React 展开／收起、三项内容和交错顺序由组件测试覆盖，未操作真实 Codex 历史。物理手机触感由用户在部署后最终复验。
