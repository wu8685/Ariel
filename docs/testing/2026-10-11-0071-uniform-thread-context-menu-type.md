# 0071 会话操作菜单字体统一测试记录

- 日期：2026-10-11
- 规格：[0071 会话操作菜单字体统一](../specs/0071-uniform-thread-context-menu-type.md)
- 范围：Web 会话列表右键／长按菜单 typography；不改变归档 mutation。

## Red → Green

1. 先在 `theme.test.ts` 加入 CSS 契约，要求 `.thread-context-menu` 使用 `12px / 1.4`，按钮继承字号与行高，并保持 `38px` 最小高度。
2. 修复前测试以菜单缺少 `font-size: 12px` 失败。
3. 仅补充会话菜单 typography 后，目标测试 18/18 通过。

## 真实浏览器

在隔离 WebSocket fixture 与 Chromium 390×844 viewport 中通过长按打开会话菜单：

- “归档”计算字号为 `12px`；
- 计算行高为 `16.8px`；
- 菜单项高度不低于 `38px`；
- 归档仍只发出一次 mutation，成功后仍显示撤销入口。

该用例未连接真实 Codex 会话，也未修改业务数据。物理 iPhone／Safari 留待公网版本由用户复验。

## 全量回归

- Web：23 个测试文件、187 项测试通过；
- production build：通过；
- Vite 报告现有主 bundle 超过 500 kB 的 warning，不影响本次构建成功，也不是本次 typography 变更引入的功能错误。
