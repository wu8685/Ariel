# 0121／0107：未知 JSON type 与同序号 freshness 恢复

Issue #43；按照既有已批准契约修复两个已复现缺陷，不扩大内存限额、lazy 字段范围或 mutation 授权。

## 两个独立故障

1. **v0.11.3 兼容性回归**：8–64 MiB 原生 frame 进入已有 streaming projection 后，保留的未知元数据只要含 `type`，就被当作最长 256 bytes 的字符串 enum。JSON schema 的 `type: ["string", "null"]`、数字、null 等合法 JSON 因此被拒绝。隔离 v0.11.2 相同约 9 MiB fixture 原本通过，只更换 v0.11.3 reader 后失败。
2. **Web freshness 恢复被正文去重拦截**：初始 watch 之后至少收到一次异步内容更新，再发生 lease stale；Relay 核验相同 follower lease 恢复时，正文 stream／seq 可以不变。Web 的 StreamFence 将这次恢复当作重复内容丢弃，仍显示同步中且禁用发送。此前用例只覆盖恢复时 seq 增长或 stream 替换，遗漏此情况。

用户提供的列表／发送成功与 cache 发布日志证明 Agent 仍在处理请求，不能据此证明报错会话已恢复；失败 guard 记录缺少完整时间及 thread opaque key，尚未与成功记录关联。本次合成复现不能冒充目标设备的已确认触发原因。

## 实际 Red → Green

| 覆盖 | 修复前实际结果 | 修复后要求 |
| --- | --- | --- |
| `TestReadProjectedFramePreservesUnknownTypeValues` | 6 类约 9 MiB 合法 frame 全部 `ErrInvalidFrame` | 数字、null、布尔、对象、数组和长字符串逐字保留，原字节上限不变 |
| `TestNativeLargeFrameOpensAndPatchesAtProductionLimits` | 在保留字段加入 JSON schema union 后，生产 OpenFollower cold open 失败 | 8→64 MiB fallback、schema 保留、tool／image lazy、连续 patch 与完成状态可读 |
| `stream-fence.test.ts` | 同序号恢复被拒绝 | 仅显式恢复允许当前 seq；更旧 seq、退休 stream、cache-pending 仍拒绝 |
| `App.test.tsx` | 异步更新后同 seq fresh 找不到正常发送按钮 | 初始 watch 和后续 update 两条路径都能恢复；保留草稿，不产生发送请求 |
| `relay-live-cache.spec.ts` | 真实 Relay lease 恢复后，Chromium 等待正常发送按钮超时 | 原位恢复；`RESYNC_REQUIRED` 仍不能靠 lease 清除，新的合法 snapshot 才接管 |
| `native-wire-reconnect.spec.ts` | 原生 JSON schema union 会阻止首次正文到达 | 真实 IPC → Agent → Relay → Chromium 的首次正文、增量与终态全部可见；零 native mutation |

JSON reader 仅把短字符串值作为 lazy discriminator 候选；其他值按普通 JSON 解析和保留。envelope 的字段类型仍由原消息解析校验。Web 仅在已有 view 已 stale 且收到 Relay fresh 事件时允许相同 seq；普通 fresh 重复事件仍被丢弃，不让每次 lease 触发正文持久化。

## 验证边界

- `go test ./...`、`go test -race ./...`、`go vet ./...` 通过；生产大帧专项 `-race -count=5` 通过。
- Web 319 单测与 production build 通过；Chromium 全量 100 项通过，原有截图不变。真实 Relay 恢复专项连续 5 次通过；新增 schema union 的 native wire 浏览器专项连续 3 次通过。
- 本机真实 Codex owner 只读探测通过，10 秒观察收到 2 个 snapshot 与 3 批 patch，状态为 57,460,599 bytes；没有存储正文或真实标识，没有发送消息、停止或审批。该 native 版本高于最小基线，探针仍有 `unverified` 版本提醒；本次仅证明实际 read／follow，不扩大发送兼容性结论。
- Chromium 的固定手机 viewport／touch 测试不代表物理 iPhone／Safari。另一台 Mac 未取得远程访问方式，不宣称已更新或实机验收。
