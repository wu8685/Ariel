# 0132：Puck 毛玻璃浮层与对话顶栏悬浮气泡

- 状态：Implemented（2026-11-06，第 33 轮完成顶栏三点定位与 composer 浮层修正；Vitest、全量 Playwright 与 production build 已验）。
- 日期：2026-11-06。
- 范围：Puck Web 的对话顶栏、composer、新建助理 dialog，以及手机端覆盖对话区的 info 面板；不改变 Relay、Desktop Agent、协议或数据状态。
- 关联：0130（Puck 布局与深色视觉）、0111（52px 可触达顶栏）、0116（紧凑圆形操作图标）。

## 1. 用户结果

Puck 的非主界面 chrome 使用统一的圆润深色毛玻璃语言。对话顶栏不再呈现横贯内容区的实心色带，而由菜单、标题和操作区三类独立气泡组成；它们保持固定可触达，并在视觉上浮于 transcript 上方。composer 同样只保留浮动输入气泡，不再由整条 footer 覆盖内容。左侧助理列表继续是抽屉，不改成 chips。

## 2. 输入、输出与结构

1. 输入仍为当前助理、Puck 状态、移动面板状态和现有事件回调；不增加请求、状态或持久数据。
2. 对话 header 保留既有 `header` 语义，但不再占据独立 grid row；它作为透明、绝对定位的 overlay 叠在 transcript 上方，内部拆为：移动端菜单气泡、标题气泡（头像、名称、可选 chevron）、操作气泡（idle 为 info，busy 为 stop）。
3. 320／390／1280px 均呈现全宽三点定位：菜单圆 chip 靠 header 左内边距，标题 pill 的几何中心与 header 水平中心重合，操作圆 chip 靠 header 右内边距。三颗 chip 不得作为整体居中簇；左右 chip 等宽。手机安全区叠加到 overlay 偏移，不降低点击可达性。
4. 对话 footer 保留语义但改为底部透明 overlay，不占 grid row、不绘制全宽底色，空白区域不拦截 transcript 操作；仅 composer shell 恢复 pointer events。composer 使用同一组 glass token，transcript 通过底部 padding 保证最后一条消息滚到底时位于 composer 上方。
5. 新建助理 dialog 与手机端 info overlay 使用同一组玻璃 token。桌面左右常驻栏保持既有实心深色层级；左侧助理抽屉在所有 viewport 都保持抽屉形态和实心背景。

## 3. 视觉 token 与性能边界

1. 在 Puck 根 token 中定义并复用半透明深色填充、12–20px blur、浅色边框与阴影；header 气泡、dialog 和手机 info overlay 不各自硬编码一套玻璃参数。
2. 毛玻璃层使用 `backdrop-filter` 与 `-webkit-backdrop-filter`；半透明填充本身必须在不支持 blur 时仍维持文字对比度。
3. transcript、消息长列表、页面根、左右常驻栏不得施加 backdrop blur。blur 面积只限于小型 header 气泡和当前打开的 overlay 面板。
4. 页面底色继续为 `#050505`；助理气泡继续使用 `--puck-assistant-bubble`（当前为 `#1c1c1e`），用户气泡、头像出现规则、发送按钮／头像同色、composer 加号和 Passkey 均不得回退。
5. 所有玻璃气泡采用圆润 pill／card 轮廓，焦点、hover、active、disabled 状态继续清晰；玻璃层不得截断现有按钮的命中区域。

## 4. 布局、不变量与错误边界

1. 320／390／1280px 均无横向滚动。长标题仍单行省略，不能挤走菜单、info 或 stop。
2. header 与 footer overlay 始终可见，且滚动只发生在 transcript；transcript 与对话区共享完整高度。顶部／底部 padding 只保证首尾消息可读，不形成独立 chrome 占位行。滚动时消息必须能从浮层间隙和浮层下方穿过，并在 glass chip 内被 backdrop blur 采样；overlay 空白区域不得拦截 transcript 的 pointer／touch 交互。
3. 手机打开助理列表仍从左侧滑入并由 backdrop 关闭；打开 info 仍从右侧滑入。玻璃视觉不得改变其打开、关闭、焦点或内容滚动语义。
4. dialog 表单字段、提交／取消、auto focus 与可访问名称不变。浏览器不支持 blur 时不报错、不隐藏内容。
5. 本轮不扩展共享 Ariel chrome，避免改变非 Puck 页面；后续若统一全局 token，须另行确认范围与回归基线。

## 5. TDD 与验收

1. 先增加并运行失败的真实浏览器断言：header／footer 本体透明、绝对定位且无整条底色／grid 占位；header 内存在左圆／中 pill／右圆三颗独立玻璃 chip，composer 本体也是独立 glass chip；气泡与 dialog 共享实际 blur、低于 0.6 alpha 的半透明背景、边框和圆角。
2. 量测三颗 header chip 互不重叠、左右等宽、左／右分别贴近 header 内边距、中间 pill 几何中心接近 header 中心；明确不再验收整簇居中。transcript 与对话区顶部、底部重合，首尾消息分别由 padding 保持可读；滚动后消息几何区域可进入 header／footer 背后，浮层仍保持可见。手机助理抽屉保持实心且不应用 blur，info overlay 应用玻璃 token。
3. 更新受影响的 320／390／1280px 固定截图并人工检查；保留隔离 WebSocket fixture，不写真实 Codex／Puck 数据。
4. 运行 Puck Vitest、完整 Web Vitest、Playwright Puck suite 与 Web build。Chromium 手机 viewport／touch 验收不能替代物理 iPhone／Safari，后者保留用户复验。
