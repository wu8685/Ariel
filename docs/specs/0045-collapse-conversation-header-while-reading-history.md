# 0045：阅读历史时自动收起会话 Header

- 状态：Implemented（138 个 Web 单测、8 个真实 Chromium 用例、390×844 完全收起几何断言与截图、production build 已验；物理 iPhone/Safari 待用户复验）。
- 范围：Ariel Web 的会话 Header、底部输入区域、对话历史滚动行为、手机与桌面响应式布局、自动测试与真实移动浏览器视觉验收。
- 关联：[0011 手机会话紧凑布局](0011-compact-mobile-conversation-header.md)、[0017 实时更新时保留历史阅读位置](0017-preserve-reading-position-on-live-updates.md)、[0039 真实浏览器手机 UI 与截图回归](0039-real-browser-mobile-ui-regression.md)。

## 目标

用户向上离开最新消息、阅读较早的对话历史时，顶部会话 Header 和底部输入区域整层自动收起，把上下两侧的全部垂直空间交给对话历史；用户回到最新消息、点击“回到最新”或切换会话后，两者自动恢复完整状态。

## 交互契约

1. Header 的折叠判断复用对话区“是否跟随最新消息”的滚动距离，不单独依赖瞬时滚动方向，避免 streaming 或程序化滚动触发来回抖动。
2. 用户离开底部超过既有跟随阈值后进入完全收起状态；收起状态使用回到底部附近才展开的滞回区间，避免 Header 改变高度后在阈值附近反复切换。
3. 收起状态将 Header 的高度、padding 与边框占位全部降为 0，并隐藏其中的标题、手机会话列表入口和权限入口；底部输入区域同时收至 0，隐藏输入框、排队区、截图草稿和操作按钮。两者恢复后重新可见、可操作。
4. 点击“回到最新”立即展开 Header 并滚动到底部。
5. 切换到另一会话时立即展开 Header；若连接恢复后明确恢复了旧阅读锚点，则保持紧凑状态。
6. streaming 更新期间：正在跟随最新消息时 Header 保持展开；用户正在阅读旧历史时 Header 保持完全收起，且不得抢走阅读位置。
7. `prefers-reduced-motion: reduce` 继续禁用过渡动画。
8. 两层完全收起时显示一个不占据文档布局的“回到最新”浮动按钮，防止视口扩张后已直接覆盖到底部、无法再靠滚动触发展开的死路；按钮点击后恢复上下两层。

## 布局契约

1. 手机完整 Header 延续现有 66px 高度；Header 与输入区域收起后均为 0px，不保留触控目标、safe-area padding 或边框占位。
2. 桌面 Header 与输入区域同样完整收起至 0px，但不改变侧栏、权限语义或对话正文宽度。
3. Header 展开时标题继续单行截断，不能造成横向滚动。
4. Header 高度变化后 `.transcript` 通过现有 flex 布局自动获得释放空间。

## 验收

1. 单元测试覆盖：离开最新消息后折叠、回到最新后展开、streaming 更新不展开、切换会话后展开。
2. 样式契约测试覆盖：移动端 Header 66px → 0px、输入区域 → 0px、隐藏两侧全部内容并移除边框／safe-area 占位，同时保留过渡／reduced-motion。
3. 390×844、touch-enabled 的真实 Chromium fixture 覆盖：Header 和输入区域高度归零、对话区获得两者释放的全部高度、两侧控件不可见、回到底部后恢复。
4. 保存折叠状态截图基线并运行 Web 全量单测、Playwright 与 production build。
5. 物理 iPhone/Safari 的手势与 safe area 仍需用户最终复验；Chromium 结果不得冒充物理 Safari 结论。

## 实现结果

1. `.conversation-head` 与 `.composer-wrap` 的 `history-collapsed` 状态将桌面／手机上下两层都完全收至 0px；标题、路径、输入框、队列和控件随整层隐藏。
2. 跟随最新消息与上下两层共用 80px／32px 滞回区间；收起后只接受用户明确向底部滚动或点击浮动按钮来恢复。视口扩张和 streaming 造成的程序化滚动不会把正在读历史的用户错误拉回底部。
3. “回到最新”、滚动到底部和切换会话会恢复完整 Header；同会话断线重订阅成功恢复旧阅读锚点时继续保持紧凑状态。
4. 真实移动浏览器截图 `mobile-history-header-collapsed-darwin.png` 断言 Header 和输入区域都不再可见，并将两层完整空间交还给 transcript；100px 浅上滑边界也不会因视口扩张而反弹。
5. 右下角“回到最新”使用绝对定位，不占 transcript 的布局空间；点击后恢复 Header、输入区与最新消息位置。
