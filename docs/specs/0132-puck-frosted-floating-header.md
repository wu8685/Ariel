# 0132：Puck 毛玻璃浮层与对话顶栏悬浮气泡

- 状态：Implemented（2026-11-06，第 29 轮用户确认设计并授权实施；Vitest、Playwright 与 production build 已验）。
- 日期：2026-11-06。
- 范围：Puck Web 的对话顶栏、新建助理 dialog，以及手机端覆盖对话区的 info 面板；不改变 Relay、Desktop Agent、协议或数据状态。
- 关联：0130（Puck 布局与深色视觉）、0111（52px 可触达顶栏）、0116（紧凑圆形操作图标）。

## 1. 用户结果

Puck 的非主界面 chrome 使用统一的圆润深色毛玻璃语言。对话顶栏不再呈现横贯内容区的实心色带，而由菜单、标题和操作区三类独立气泡组成；它们保持固定可触达，并在视觉上浮于 transcript 上方。左侧助理列表继续是抽屉，不改成 chips。

## 2. 输入、输出与结构

1. 输入仍为当前助理、Puck 状态、移动面板状态和现有事件回调；不增加请求、状态或持久数据。
2. 对话 header 保留既有 `header` 语义与 52px 高度，在内部拆为：移动端菜单气泡、标题气泡（头像、名称、说明）、操作气泡（info／stop）。不存在的操作不制造空气泡。
3. 桌面隐藏移动端菜单和 info 入口；标题与现有操作仍使用相同业务条件。手机安全区继续叠加到 52px header，不降低点击可达性。
4. 新建助理 dialog 与手机端 info overlay 使用同一组玻璃 token。桌面左右常驻栏保持既有实心深色层级；左侧助理抽屉在所有 viewport 都保持抽屉形态和实心背景。

## 3. 视觉 token 与性能边界

1. 在 Puck 根 token 中定义并复用半透明深色填充、12–20px blur、浅色边框与阴影；header 气泡、dialog 和手机 info overlay 不各自硬编码一套玻璃参数。
2. 毛玻璃层使用 `backdrop-filter` 与 `-webkit-backdrop-filter`；半透明填充本身必须在不支持 blur 时仍维持文字对比度。
3. transcript、消息长列表、页面根、左右常驻栏不得施加 backdrop blur。blur 面积只限于小型 header 气泡和当前打开的 overlay 面板。
4. 页面底色继续为 `#050505`；助理气泡继续使用 `--puck-assistant-bubble`（当前为 `#1c1c1e`），用户气泡、头像出现规则、发送按钮／头像同色、composer 加号和 Passkey 均不得回退。
5. 所有玻璃气泡采用圆润 pill／card 轮廓，焦点、hover、active、disabled 状态继续清晰；玻璃层不得截断现有按钮的命中区域。

## 4. 布局、不变量与错误边界

1. 320／390／1280px 均无横向滚动。长标题仍单行省略，不能挤走菜单、info 或 stop。
2. header 始终可见，且滚动只发生在 transcript；header 气泡不遮住首条或末条消息，不破坏 composer 与安全区。
3. 手机打开助理列表仍从左侧滑入并由 backdrop 关闭；打开 info 仍从右侧滑入。玻璃视觉不得改变其打开、关闭、焦点或内容滚动语义。
4. dialog 表单字段、提交／取消、auto focus 与可访问名称不变。浏览器不支持 blur 时不报错、不隐藏内容。
5. 本轮不扩展共享 Ariel chrome，避免改变非 Puck 页面；后续若统一全局 token，须另行确认范围与回归基线。

## 5. TDD 与验收

1. 先增加并运行失败的真实浏览器断言：header 本体透明且无整条底边，内部存在独立标题／操作玻璃气泡；气泡与 dialog 共享实际 blur、半透明背景、边框和圆角。
2. 量测 header 气泡互不重叠、位于 header 内、首条消息不被遮挡；滚动后 header 保持可见。手机助理抽屉保持实心且不应用 blur，info overlay 应用玻璃 token。
3. 更新受影响的 320／390／1280px 固定截图并人工检查；保留隔离 WebSocket fixture，不写真实 Codex／Puck 数据。
4. 运行 Puck Vitest、完整 Web Vitest、Playwright Puck suite 与 Web build。Chromium 手机 viewport／touch 验收不能替代物理 iPhone／Safari，后者保留用户复验。
