# 0085：后台同步期间的发送按钮加载态

- 状态：Approved（2026-10-15，用户已确认；实施中）。
- 来源：0084 已能在 WebSocket 重连或页面 reload 后立即展示本地缓存正文，但用户仍希望明确知道 fresh owner 正在后台恢复。
- 关联：[0073 会话增量同步与无白屏缓存恢复](0073-incremental-thread-sync-and-stale-view-cache.md)、[0083 前后台与会话切换的非破坏性恢复](0083-nondestructive-foreground-resume.md)、[0084 Passkey 滑动会话与本地持久恢复](0084-passkey-sliding-session-and-durable-resume.md)。

## 用户行为

1. 当前会话正在展示已确认的本地缓存正文，但 fresh owner snapshot 尚未完成时，composer 保持可见，正文、草稿和滚动位置不变。
2. 原发送按钮在原位置显示旋转的 loading spinner，让用户知道会话正在后台同步。
3. loading 按钮与普通发送按钮保持相同的圆形、宽高、边框和占位，不引起 composer 或页面布局跳动。
4. 后台同步期间发送按钮保持 disabled；fresh owner snapshot 完成并且原有发送条件均满足后，恢复发送图标与发送能力。
5. 同步失败或连接断开时，按钮继续保持 disabled；重试期间仍显示 spinner。只有明确进入 Passkey 登录态、没有任何可展示会话，或用户切换到其他状态时，才按对应既有 UI 展示。

## 状态边界

1. loading 仅表示“当前可见的是 stale display，正在恢复该会话的 owner 能力”，不能复用 `working`、消息发送回执等待、Codex `inProgress` 或历史分页 loading。
2. 判断必须来自现有的 identity／subscription fencing 状态：当前已有 `view`，但 `ownerReady === false`，且客户端处于连接或会话同步恢复链路。
3. 普通只读历史、Agent 明确离线、权限受限和未选择会话不得伪装成后台同步；这些状态继续使用现有提示与禁用语义。
4. spinner 只承担状态表达，不新增轮询、WebSocket、Relay 消息或 Codex 请求。

## 可访问性与视觉

1. loading 按钮使用明确的 `aria-label="正在同步会话，暂不可发送"`，spinner 自身为装饰元素。
2. 支持 `prefers-reduced-motion`：保留 loading 图形，但停止连续旋转动画。
3. spinner 的视觉尺寸不大于原 `SendIcon`，按钮触控区域保持现有最小 `36 × 36px`；移动端的实际几何尺寸必须与正常发送按钮一致。

## TDD 与验收

1. Red：App 测试预置 stale cached view 与未就绪 owner，断言发送按钮 disabled、aria-label 正确、显示 spinner 且不显示 SendIcon；fresh snapshot 后恢复原图标和能力。
2. Red：断言普通 `working`、Codex `inProgress`、历史分页和只读历史不会错误触发该 spinner。
3. 真实移动 Chromium 关闭 WebSocket并延迟 fresh snapshot，断言正文持续可见、按钮立即进入 loading、点击与 Enter 均不能发送；snapshot 到达后恢复。
4. page reload 从 IndexedDB 恢复正文时覆盖同一行为。
5. 通过浏览器几何断言和手机 viewport 截图，确认 loading 与发送按钮的 bounding box、圆形和位置一致，没有 composer 跳动。
6. 运行 Web 全量测试、production build 和全部 Playwright 回归；物理 iPhone／Safari 由用户复验。

## 风险与回滚

- 风险：若 loading 判断范围过宽，会把正常 Codex 处理中误表示为同步；通过独立派生的 owner recovery 状态和负向测试约束。
- 风险：spinner 动画造成视觉干扰；遵守 reduced-motion，并只在 mutation 确实不可用的恢复窗口展示。
- 回滚：恢复原 disabled SendIcon，不改变 0084 的缓存、重连和认证机制。
