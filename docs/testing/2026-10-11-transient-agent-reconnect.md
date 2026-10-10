# Desktop Agent 短暂重连回归

- 日期：2026-10-11
- 对应规格：[0006 M4 断线恢复与有界容量](../specs/0006-m4-recovery-capacity.md)
- 问题：本地 Agent 到云端 Relay 发生约 5 秒的可恢复重连时，Web 会立即清空已选会话并显示离线错误。

## 验收场景

1. 已选会话收到 `device.status(agentOnline=false)` 后，最后确认的会话内容继续可见。
2. 8 秒宽限期内，发送、停止、审批和队列变更均不可用，不产生离线写入或自动重放。
3. 宽限期内收到在线状态后，Web 重新读取设备状态并建立新 subscription；新 snapshot 接管后恢复操作，不遗留离线警告。
4. 离线持续满 8 秒后，Web 才清空旧视图并显示明确提示。

## 验证命令

```bash
cd web
npm test -- --run src/App.test.tsx -t 'transient Agent reconnect|reconnect grace period'
npx playwright test e2e/mobile-session-navigation.spec.ts
```

物理 iPhone / Safari 的公网网络切换仍需用户复验；自动化验收覆盖 Chromium 手机 viewport 与 touch 模式，不冒充物理设备结果。
