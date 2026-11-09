# 0122 Web Puck watch 健康轮询

- 状态：Approved
- 日期：2026-11-09

## 规格

- `puck.watch` 返回有效 `watchId` 后，Bot 列表与当前 history 的全量 polling 都降为 60s 健康校验。
- watch 尚未建立、被拒绝、抛错或随 selection 清理时，列表恢复 5s、history 恢复既有频率（存在 event transport 时 10s，否则 2s）。
- event payload、公开协议与 history merge 语义不变。
- 切换 Bot 时新 watch 未确认前按 fallback 频率运行，避免把旧 Bot 的健康状态误用于新 Bot。

## 验收

- 正常 watch 的前 59s 不发生周期性 list/history 请求，第 60s 执行健康校验。
- watch rejected 时 5s 内恢复 list polling，10s 内执行 history polling。
