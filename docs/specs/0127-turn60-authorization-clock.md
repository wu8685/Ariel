# 0127 Pending authorization 倒计时时钟

- 状态：Approved
- 日期：2026-11-09

## 规格

- 存在 running work 时保持既有 1s 重渲染时钟。
- 没有 running work、但存在 `cardState=pending` 的 authorization 时启动 5s 低频时钟。
- 两者都不存在时不启动时钟。
- authorization 到期后无需服务端新事件即可切换为“已过期”且隐藏决策按钮。

## 验收

- fake timer 验证空闲页面上的 pending authorization 在到期后 5s 内更新。
- 既有 PuckWorkspace 测试通过。
