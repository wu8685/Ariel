# 0071：会话操作菜单字体统一

- 状态：Implemented（Web 全量测试、production build 与真实移动 Chromium 已验）
- 来源：会话列表右键／长按菜单中的“归档”字号明显大于现有紧凑菜单体系。
- 关联：[0068 侧栏会话长按菜单与原生置顶／归档](0068-sidebar-thread-context-actions.md)。
- 范围：仅 Web 会话操作菜单的 typography；不改变菜单内容、交互、点击区域或归档语义。

## 行为与视觉规格

1. `.thread-context-menu` 使用与现有 `.queue-menu` 一致的紧凑菜单字号与行高：`12px / 1.4`。
2. 菜单按钮显式继承容器字号与行高，避免浏览器默认 button 样式把“归档”放大。
3. 菜单项最小高度继续保持 `38px`，不因字号缩小而降低 touch 点击区域。
4. 禁用说明保留现有层级与文案，不改变可访问名称、focus、右键或长按行为。

## TDD 与验收

1. Red：CSS 契约测试要求会话菜单为 `12px / 1.4`，按钮使用 `font-size: inherit` 与 `line-height: inherit`。
2. Green：只补充会话菜单 typography，不改组件状态或 mutation 逻辑。
3. 真实 Chromium 在 390×844 viewport 打开长按菜单，断言“归档”计算字号为 `12px`、点击区域高度不低于 `38px`，并确认菜单不越界。
4. 运行 Web 全量测试与 production build。

## 实施结果

- CSS 契约测试先以会话菜单缺少显式字号失败，随后补充 `12px / 1.4` 和按钮继承后转绿。
- 390×844 Chromium 实测“归档”计算字号为 `12px`、行高为 `16.8px`，点击区域高度保持至少 `38px`。
- Web 23 个测试文件、187 项测试与 production build 全部通过。详见[专项测试记录](../testing/2026-10-11-0071-uniform-thread-context-menu-type.md)。
