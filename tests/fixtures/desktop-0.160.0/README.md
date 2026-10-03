# Desktop 0.160.0 协议形状样例

以下为本机验证后重构的最小 contract 样例，身份和值使用合成替代；不是完整的原始抓包。来源和具体限制见 [M0 报告](../../../docs/compatibility/2026-10-03-m0.md)。

- `start-success.json`：owner 接受 turn，真实 turn ID 位于响应的 `result.result.turn.id`。
- `interrupt-success.json`：owner 的停止回执，目标位于响应的 `result.interruptedTurnId`。
- `stale-user-input-ok.json`：不存在的补充问题也可能返回 `result.ok=true`；禁止据此判断受理成功。
- `user-input-completed.json`：两题的 pending request 与 canonical history 中 completed userInputResponse，包含一个选项和一个自由文本答案；`TestUserInputRedactedContract` 验证此最小结构。

实际 ID、cwd、用户内容与凭据均未保存。当前审批请求的完整 Schema 尚未通过真实 pending request 验证，不在这里伪造样例。
