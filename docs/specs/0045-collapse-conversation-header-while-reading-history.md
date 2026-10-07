# 0045：阅读历史时自动收起会话 Header

- 状态：Implemented（138 个 Web 单测、8 个真实 Chromium 用例、390×844 几何断言与截图、production build 已验；物理 iPhone/Safari 待用户复验）。
- 范围：Ariel Web 的会话 Header、对话历史滚动行为、手机与桌面响应式布局、自动测试与真实移动浏览器视觉验收。
- 关联：[0011 手机会话紧凑布局](0011-compact-mobile-conversation-header.md)、[0017 实时更新时保留历史阅读位置](0017-preserve-reading-position-on-live-updates.md)、[0039 真实浏览器手机 UI 与截图回归](0039-real-browser-mobile-ui-regression.md)。

## 目标

用户向上离开最新消息、阅读较早的对话历史时，顶部会话 Header 自动收成紧凑状态，把释放的垂直空间交给对话历史；用户回到最新消息、点击“回到最新”或切换会话后，Header 自动恢复完整状态。

## 交互契约

1. Header 的折叠判断复用对话区“是否跟随最新消息”的滚动距离，不单独依赖瞬时滚动方向，避免 streaming 或程序化滚动触发来回抖动。
2. 用户离开底部超过既有跟随阈值后进入紧凑状态；紧凑状态使用回到底部附近才展开的滞回区间，避免 Header 改变高度后在阈值附近反复切换。
3. 紧凑状态仍保留会话标题、手机会话列表入口以及权限入口；隐藏次要 eyebrow 与项目路径，缩短 Header 的实际布局高度，而不是只做视觉位移。
4. 点击“回到最新”立即展开 Header 并滚动到底部。
5. 切换到另一会话时立即展开 Header；若连接恢复后明确恢复了旧阅读锚点，则保持紧凑状态。
6. streaming 更新期间：正在跟随最新消息时 Header 保持展开；用户正在阅读旧历史时 Header 保持紧凑，且不得抢走阅读位置。
7. `prefers-reduced-motion: reduce` 继续禁用过渡动画。

## 布局契约

1. 手机完整 Header 延续现有 66px 高度；紧凑 Header 为 46px，并保留至少 44px 的核心触控目标。
2. 桌面紧凑 Header 同样释放实际高度，但不改变侧栏、权限语义或对话正文宽度。
3. 标题在两种状态下均单行截断，不能造成横向滚动。
4. Header 高度变化后 `.transcript` 通过现有 flex 布局自动获得释放空间。

## 验收

1. 单元测试覆盖：离开最新消息后折叠、回到最新后展开、streaming 更新不展开、切换会话后展开。
2. 样式契约测试覆盖：移动端 66px → 46px、隐藏次要信息、保留 44px 触控目标与过渡／reduced-motion。
3. 390×844、touch-enabled 的真实 Chromium fixture 覆盖：Header 高度、对话区空间增长、必要控件可见、回到底部后恢复。
4. 保存折叠状态截图基线并运行 Web 全量单测、Playwright 与 production build。
5. 物理 iPhone/Safari 的手势与 safe area 仍需用户最终复验；Chromium 结果不得冒充物理 Safari 结论。

## 实现结果

1. `.conversation-head` 新增 `history-collapsed` 状态：桌面由 116px 收至 58px，手机由 66px 收至 46px；隐藏 eyebrow 与路径，保留标题和必要控件。
2. 跟随最新消息与 Header 共用 80px／32px 滞回区间。Header 改变高度后，即使 streaming 继续增长，也不会把正在读历史的用户错误拉回底部。
3. “回到最新”、滚动到底部和切换会话会恢复完整 Header；同会话断线重订阅成功恢复旧阅读锚点时继续保持紧凑状态。
4. 新增真实移动浏览器截图 `mobile-history-header-collapsed-darwin.png`，并断言紧凑 Header 将 20px 实际空间交还给 transcript。
