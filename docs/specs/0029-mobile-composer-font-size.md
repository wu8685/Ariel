# 0029：手机输入字号与会话正文一致

- 状态：Implemented（自动测试、production build 与 390×844 隔离浏览器验收通过；物理 iPhone 待用户复验）。
- 来源：用户在物理手机上观察到 composer 输入文字明显大于上方对话正文，希望两者字号一致。
- 范围：宽度不超过 800px 的 Ariel Web composer 文字；不改变会话正文字号、桌面 composer、输入高度、发送／停止行为或 0028 的上下分层布局。

## 输入、输出与行为

1. 手机 composer 的 textarea 文字和 placeholder 使用与 `.message-text` 相同的 14px 字号，不再使用 16px。
2. 行高继续为 24px，空草稿 44px、八行 212px 上限和内部滚动保持不变，避免字号调整再次引入高度抖动。
3. iPhone／iPad 启动时给 viewport 增加 `maximum-scale=1`，阻止 WebKit 因小于 16px 的表单文字而在聚焦时自动放大；该处理只应用于 iOS，不改变 Android 与桌面浏览器的 viewport。
4. iOS 10 及以后仍允许用户主动 pinch zoom；这里仅抑制浏览器对表单聚焦的自动缩放。依据：[WebKit iOS 10 交互行为](https://webkit.org/blog/7367/new-interaction-behaviors-in-ios-10/) 与 [WebKit focus zoom 修复记录](https://bugs.webkit.org/show_bug.cgi?id=157771)。

## TDD 与验收

1. 先写失败的样式测试，要求手机 textarea 与会话正文都为 14px。
2. 先写失败的 viewport 测试，覆盖 iPhone、触屏 iPad、重复调用、Android 与桌面浏览器；非 iOS 不得写入 `maximum-scale`。
3. Web 全量测试和 production build 通过；在 390×844 隔离浏览器确认 composer 与消息正文计算字号均为 14px，高度与宽度不变且无横向溢出。
4. 物理 iPhone 最终确认文字视觉一致、聚焦时页面不自动放大。
