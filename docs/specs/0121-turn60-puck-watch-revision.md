# 0121 Puck watch 内存 revision

- 状态：Approved
- 日期：2026-11-09
- 授权：Turn 60 用户明确授权 architecture review 的单调 revision 方案

## 规格

- Store 在相关 mutation 成功提交后递增进程内单调 revision，并广播 notify。
- revision 与 notify 不写入 SQLite，不改变 schema；进程重启后从初始值重新开始。
- watcher 首次仍立即查询并发送完整 snapshot，以保持公开协议不变。
- 后续 250ms tick 若 revision 未变化，不执行 Assistant/Messages 查询或 JSON 序列化。
- mutation notify 到达时立即检查 revision；有变化才重新查询并发送变化后的完整 snapshot。
- notify 是广播语义，多个 watcher 不互相消费通知。
- revision 变化但目标 assistant snapshot 未变化时，不重复发送 payload。

## 验收

- 空闲超过一个 polling interval 时 snapshot query 计数不增加。
- AppendMessage 后 watcher 无需等待下一轮长期健康检查即可收到 snapshot。
- 多 watcher、并发 mutation 通过 race test。
