# 0041：会话加载标识改为可靠的蓝色矢量图

> 本规格已由 [0044 “风之信使”正式 Logo 系统](0044-wind-messenger-logo-system.md)取代；非 emoji 的兼容要求继续有效，但正式反白微标不再通过 `currentColor` 染色。

- 状态：Superseded（由 0044 取代；以下内容仅为历史记录）。
- 修复：0037 只给 Unicode `✳` 设置 CSS 颜色；移动 Safari 可能把它作为彩色 emoji 字形渲染，忽略 `color`，因此仍可能显示绿色。

## 用户可见行为

1. “正在同步会话”与空会话占位使用 SVG 矢量标识，不再依赖系统 emoji 字体。
2. SVG 使用 `currentColor`，固定继承 Ariel 的 `--color-accent` 蓝色。
3. 标识仍为纯装饰，不增加屏幕阅读器噪音；标题和说明文字不变。

## 验收

1. 组件测试验证占位标识是无文本 SVG，不含 `✳` 字符。
2. 主题测试验证会话加载 SVG 的颜色来自 `--color-accent`。
3. 新增真实移动浏览器 loading fixture：延迟 snapshot，点击会话后断言计算色值为 Ariel 蓝，并生成截图基线。
4. 全量 Web tests 与 production build 通过；物理 iPhone/Safari 仍需最终人工复验。
