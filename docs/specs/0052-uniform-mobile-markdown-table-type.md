# 0052：移动端 Markdown 表格列字体一致

- 状态：Implemented（139 个 Web 单测、10 个真实 Chromium 用例、移动表格截图与 production build 已验；物理 iPhone/Safari 待用户复验）。
- 范围：会话消息中的 GFM Markdown 表格、移动端 WebKit 文本缩放约束、真实浏览器几何与截图回归。
- 不涉及：Markdown 语法、安全链接、表格内容或列宽分配规则。

## 问题

iPhone Safari 中，较宽的 Markdown 表格会出现不同列字号不一致：第一列接近会话正文，后续列被明显放大。现有 CSS 只在 `table` 上设置相对字号 `.92em`，`th`/`td` 没有显式继承统一字体，横向滚动容器也没有约束 WebKit text inflation；`width: max-content` 的不同列可能因此被分别放大。

## 用户结果

同一张表格的所有表头和单元格使用一致的 13px 字号与行高。粗体只改变字重，不改变字号；宽表格继续在自身容器内横向滚动，不撑宽页面。

## 实现契约

1. `.markdown-table-scroll` 显式使用 13px 字号、统一行高，并设置 `-webkit-text-size-adjust: 100%` 与标准 `text-size-adjust: 100%`。
2. `.markdown-table-scroll table`、`th`、`td` 显式继承容器字体；表头继续使用 700 字重，正文中的 `strong` 继续只表现为粗体。
3. 保留表格的 `width: max-content`、`min-width: 100%` 和容器 `overflow-x: auto`，不通过压缩列宽牺牲内容可读性。
4. 390×844 移动视口中，全部 `th`/`td` 的 computed `font-size` 必须同为 13px，行高一致，表格滚动容器不得超出消息气泡或页面宽度。

## 验收

1. TDD：先增加 CSS 契约与移动端表格字号断言并确认现有样式失败。
2. 使用包含中英文、粗体和长文本的两列表格 fixture，在真实 Chromium 手机视口中断言 computed style、内部横向滚动和页面无横向溢出。
3. 保存移动端截图并人工检查列字体与排版；物理 iPhone/Safari 待用户复验。
4. 运行 Web 全量单测、全部 Playwright 用例和 production build。

## 实现结果

1. Markdown 表格容器统一使用 13px 字号与 1.55 行高，并通过 `text-size-adjust: 100%` 及 WebKit 前缀禁止移动 Safari 按列放大文字。
2. `table`、`th`、`td` 显式继承同一字体；表头和正文粗体仍保留字重差异，但不再改变字号。
3. 新增 390×844 真实 Chromium 表格 fixture：18 个表头／单元格 computed `font-size` 全部为 13px、行高唯一，表格在消息气泡内独立横向滚动，页面无横向溢出。
4. 移动端截图已人工检查；Web 全量 139 项单测、10 项 Playwright 用例和 production build 均通过。
