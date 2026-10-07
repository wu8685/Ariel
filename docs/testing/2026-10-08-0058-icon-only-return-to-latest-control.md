# 0058 纯图标“回到最新”控件验收

- 日期：2026-10-08
- 规格：[0058 纯图标“回到最新”控件](../specs/0058-icon-only-return-to-latest-control.md)
- 结论：通过。右下角入口已移除可见文字，改为标准向下箭头；可访问语义和滚动恢复行为保持不变。

## TDD 记录

实现前先新增以下失败断言：

1. `aria-label="回到最新"` 的按钮没有文本节点，并包含 `aria-hidden="true"` 的 SVG。
2. CSS 将按钮固定为 40×40px、网格居中，并把 SVG 固定为 18×18px。
3. 390×844 真实 Chromium 中，折叠状态的按钮宽高均为 40px、SVG 可见且文本为空。

旧实现仍渲染“回到最新”文字且使用文字 padding，三组契约按预期失败；替换为内联 SVG 与圆形尺寸后转绿。

## 视觉与交互结果

- 图标由一条竖直箭杆和对称下箭头组成，使用 `fill="none"`、`stroke="currentColor"`、圆角端点与连接。
- 按钮为 40px 圆形，图标为 18px，居中且不受字体缩放影响。
- SVG 是装饰性内容；按钮仍可通过“回到最新”或“恢复输入区并回到最新”的无障碍名称定位。
- 点击后仍恢复 Header、输入区域并稳定贴住最新消息。
- 历史断层正文中的“回到最新”操作不属于右下角悬浮控件，本次未修改。

视觉证据：`web/e2e/mobile-history-header.spec.ts-snapshots/mobile-history-header-collapsed-darwin.png`。人工检查确认箭头朝下、线条清晰、按钮圆形且未遮挡最新消息。

## 全量回归

- `npm test -- --run`：20 个文件、148 个测试通过。
- `npm run build`：TypeScript 与 Vite production build 通过；保留既有单 chunk 大于 500 kB 警告。
- `npm run test:ui`：13 个真实 Chromium 测试全部通过。
- `GO111MODULE=on go test ./...`：通过。
- `GO111MODULE=on go vet ./...`：通过。
- `git diff --check`：通过。

物理 iPhone/Safari 仍保留用户复验；自动测试验证的是移动 viewport／touch 配置下的真实 Chromium。
