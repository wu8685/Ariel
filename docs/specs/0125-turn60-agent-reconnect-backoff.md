# 0125 Desktop Agent 重连 backoff 复位

- 状态：Approved
- 日期：2026-11-09

## 规格

- Relay transport 在完成 `hello.ok` 且用户 `OnReady` 成功后视为 ready。
- 一次 attempt 达到 ready 后，其后断线等待恢复为初始 200ms；握手前连续失败仍按 2 倍增长，最大 5s。
- 复位不重启 Codex service，不改变 agent epoch、协议或 heartbeat。
- `OnReady` 失败不视为 ready，避免错误配置形成快速重试风暴。

## 验收

- 连续握手前失败达到 5s 上限。
- 经历多次抖动后每次 ready 都复位到 200ms。
- 长期稳定连接返回后下一次重连仍从 200ms 开始。
