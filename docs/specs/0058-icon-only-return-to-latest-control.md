# 0058：纯图标“回到最新”控件

- 状态：Implemented（单元测试、样式契约、真实移动 Chromium 与截图已验）。
- 范围：会话右下角 return-to-latest 控件的图形、尺寸、可访问语义与移动端视觉回归。
- 关联：[0045 阅读历史时自动收起会话 Header](0045-collapse-conversation-header-while-reading-history.md)、[0057 手动滚到最新时可靠恢复会话界面](0057-manual-scroll-restores-conversation-chrome.md)。

## 用户结果

右下角悬浮入口不再显示“回到最新”文字，而是使用简洁、通用的向下箭头。控件保持一眼可辨的“前往最下方”语义，同时减少对对话内容的遮挡。

## 行为与视觉契约

1. 仅替换 `.return-latest-bar` 中的入口；历史窗口断层内的正文操作不在本次范围内。
2. 按钮可见内容只能是一个内联 SVG 向下箭头，不使用 Unicode 字符、Emoji、图片文件或第三方图标依赖。
3. 图形采用竖直箭杆与对称下箭头，`fill="none"`、`stroke="currentColor"`、圆角端点和连接，保持专业且适配现有主题。
4. 按钮为圆形紧凑触控目标，宽高相等；图标居中，不因中文字体或浏览器文字缩放改变尺寸。
5. SVG 标记为装饰性；按钮继续根据状态保留 `aria-label="回到最新"` 或 `aria-label="恢复输入区并回到最新"`，现有点击行为不变。
6. 按钮仍位于右下角、不参与 transcript 布局，不改变 safe-area、滚动恢复和防抖逻辑。

## 验收

1. 单元测试断言按钮没有可见文字、包含装饰性 SVG，并继续能通过原有无障碍名称查询和点击。
2. 样式测试断言按钮宽高相等、图标使用固定尺寸并居中。
3. 390×844 真实 Chromium 折叠状态断言按钮为圆形、图标可见、无文字，且点击后仍恢复 Header 与输入区域。
4. 更新折叠状态截图并人工检查图标的方向、居中、对比度和遮挡范围。
5. 运行 Web 全量单测、Playwright 与 production build；物理 iPhone/Safari 保留用户复验。

## 实现结果

右下角入口现使用内联 `ReturnToLatestIcon`：24×24 viewBox 内的竖直箭杆和对称下箭头，实际显示为 18px。按钮为 40×40px 圆形控件，视觉文本已移除，原 `aria-label` 与点击恢复逻辑保持不变。完整证据见 [0058 验收记录](../testing/2026-10-08-0058-icon-only-return-to-latest-control.md)。
