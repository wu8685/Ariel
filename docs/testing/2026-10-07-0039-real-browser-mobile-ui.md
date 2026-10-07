# 0039 真实浏览器手机 UI 验收记录

- 日期：2026-10-07
- 规格：[0038 排队项双向拖拽排序](../specs/0038-bidirectional-queue-drag.md)、[0039 真实浏览器手机 UI 与截图回归](../specs/0039-real-browser-mobile-ui-regression.md)
- 环境：Chromium 1217、390×844 viewport、DPR 2、touch enabled、中文 locale、Night theme。
- 数据：Playwright 进程内隔离 WebSocket fixture；未连接或修改真实 Codex 会话。

## Red 证据

在 0038 第一版修复后，jsdom 回归已通过，但真实 Chromium touch 从队首拖到队尾时，`queue.reorder` 请求仍为空。事件跟踪显示：排队行第一次换位后，浏览器触发 `lostpointercapture`；旧实现立即取消拖拽，后续 `pointermove` 与 `pointerup` 已落到其他行，无法提交最终顺序。

## Green 修复

- 拖拽期间改用 document 级 `pointermove`／`pointerup`／`pointercancel` 监听维护生命周期，不再把 DOM 换位造成的 `lostpointercapture` 当作用户取消。
- 真实 touch 从第一项拖到第三项下方后，fixture 收到完整顺序：`queue-two`、`queue-three`、`queue-one`；权威响应后 DOM 顺序一致。
- jsdom 同时模拟“首次换位后失去 pointer capture，后续事件发送到 document”，防止生命周期回归。

## 视觉与几何验收

- `body.scrollWidth <= viewport width`，无手机横向溢出。
- queue panel 底部不超过 composer 顶部。
- 更多菜单通过 portal 展示，完整位于 composer 上方。
- 已人工查看两张基线：
  - [队列更多菜单](../../web/e2e/mobile-queue.spec.ts-snapshots/mobile-queue-menu-darwin.png)
  - [向下拖至队尾](../../web/e2e/mobile-queue.spec.ts-snapshots/mobile-queue-reordered-darwin.png)

## 命令与结果

- `npm test`：17 files / 124 tests passed。
- `npm run test:ui`：2 Playwright tests passed。
- `npm run build`：production build passed。

## 边界

这组结果证明真实 Chromium layout 与 touch pointer 路径，不等价于物理 iPhone/Safari。后者仍需在可信局域网手机上做最终兼容性复验。
