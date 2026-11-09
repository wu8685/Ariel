# 0124 Cache follower 有界并发恢复

- 状态：Approved
- 日期：2026-11-09

## 规格

- Agent 重连时仍按 active、watching、last-used 顺序选择 follower。
- 排序后的候选以最多 4 个并发的批次发送 `thread.cache.touch`；一批完成后才进入下一批，保持优先级层级。
- 单个 key 发送失败只结束该 key 的 promotion，不中断同批或后续 key。
- 不创建无界 goroutine，不改变 cache protocol、receipt ownership 或 stale 语义。

## 验收

- 并发恢复峰值不超过 4，并实际高于 1。
- 某 key 失败后其他 key 仍全部尝试。
- 定向 race test 通过。
