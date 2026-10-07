# 0039：真实浏览器手机 UI 与截图回归

- 状态：Implemented（真实 Chromium touch、布局几何与两张截图基线已验；物理 iPhone/Safari 不在本规格验收范围内）。
- 范围：Ariel Web 的关键手机交互与视觉状态，使用隔离的浏览器 fixture，不操作真实 Codex 会话。

## 目标与输入输出

- 输入：固定手机 viewport、touch pointer 事件和受控 WebSocket fixture。
- 输出：可重复的真实 Chromium 交互断言、布局几何断言与截图基线。
- 首批覆盖：运行中会话的排队列表、composer、更多菜单，以及队首向下拖至队尾。

## 行为与不变量

1. 测试必须在真实浏览器渲染引擎中执行，不能以 jsdom 代替 CSS layout、pointer capture 或截图验收。
2. WebSocket fixture 仅在浏览器进程内模拟 Relay 响应，不连接或修改真实 Codex 会话。
3. 390×844 手机 viewport 下不得横向溢出；队列面板不得遮挡 composer；更多菜单必须出现在 composer 之上。
4. 使用真实 touch pointer 序列把队首拖到队尾时，必须提交完整 `queue.reorder` 顺序，并在 UI 中显示权威响应顺序。
5. 截图比较固定 viewport、DPR、locale、color scheme 与 reduced motion；基线变化只能通过显式 update 命令接受。
6. browser fixture 不能成为 Ariel 的第二份业务状态；测试结束后不保留 session、队列或会话数据。

## 测试入口与失败产物

- `npm run test:ui`：构建／启动隔离 Web 页面，执行手机 UI 回归并比较截图。
- `npm run test:ui:update`：仅在人工确认视觉变化后更新截图基线。
- 失败时保留 Playwright screenshot、trace 或差异图到忽略目录，便于定位，不提交运行产物。

## 验收

1. 先以当前实现运行真实 touch 向下拖拽并得到失败，证明测试能捕获 jsdom 漏检的问题。
2. 修复 pointer capture 生命周期后，同一真实浏览器测试通过。
3. Web unit tests、UI tests 与 production build 全部通过。
4. 推送并重启本地 Ariel；物理 iPhone/Safari 仍单独标记，不用 Chromium 模拟冒充。

## 非目标

- 不承诺截图测试替代可访问性、协议、Go race 或物理设备兼容性测试。
- 首批不覆盖所有页面与所有浏览器；后续按真实缺陷逐步扩充高风险状态。
