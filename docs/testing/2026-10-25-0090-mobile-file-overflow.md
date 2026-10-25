# 0090 移动端文件页横向溢出回归

- 日期：2026-10-25
- 规格：[0090 文件查看器导航、双视图与片段引用](../specs/0090-file-viewer-wrap-and-text-selection.md)

## 根因

FileViewer 使用只有 row 定义、没有显式 column 下限的 CSS Grid。长且不可换行的文件路径参与隐式 `auto` column 的 min-content 计算，把 grid track 撑到 viewport 之外；正文虽然启用了 `pre-wrap`，却按这个更宽的不可见 track 排版，因此右侧被裁切。

## 修复与证据

- viewer 显式使用 `grid-template-columns:minmax(0,1fr)`，并限制 `max-width` 与内部 grid/flex item 的最小宽度。
- header 标题保持单行省略；原文和 Diff 行限制为容器宽度并继续软换行。
- Playwright 先以长中文文件路径复现失败，再增加 dialog scrollWidth 与 viewport right-edge 的几何断言。
- Web 31 个 test file、258 项测试、production build 通过。
- `mobile-file-context.spec.ts` 与 `workspace-browser.spec.ts` 共 4 项真实 Chromium 测试通过，3 张移动端截图基线已更新。

物理 iPhone Safari 仍由用户最终复验；自动化结论仅代表真实 Chromium 手机 viewport。
