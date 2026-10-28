# Cache 恢复后被迟到错误再次置为只读：回归记录

- Issue：#61
- 规格：0107 §5.5（2026-10-28 已批准的既有恢复规则补漏）
- 范围：Relay outbound delivery、promotion failure 与 Web stale lineage；不改变 v1 协议、Native Adapter、安全门禁或缓存存储边界。

## 根因

1. Relay 在 `Server.mu` 内生成 delivery，但解锁后才入 Web latest-only mailbox。较新的 snapshot／lease 可以先入队，随后被较早生成的 stale delivery 覆盖；即使新消息已出队，旧消息仍会重新入队。
2. Web fresh 分支已有 stream／seq fencing，stale 分支没有。退休 stream 或当前 stream 的较低 seq 仍能把恢复后的页面改为只读。
3. promotion transport write failure 未核对 operation ID；发送错误晚于恢复 snapshot 或下一次 promotion 时，仍能污染当前 cache 并清除新 operation。
4. Web 在 `hello.ok` 写入前就进入广播集合，设备 presence 事件可能先到，使严格客户端拒绝握手。
5. cold 历史 read 若在 owner 接管后、owner 暂时 stale 时返回，会覆盖新正文并把 seq 清为 0；之后同 seq lease 被误当重复 snapshot，无法恢复发送。

## Red → Green

| 测试 | 修复前实际失败 | 修复后行为 |
| --- | --- | --- |
| `TestRelayLateCacheDeliveryCannotRollBackWatcher` | replacement stream、新 seq、同 seq lease、sender drain 后均可被旧错误覆盖 | 按 entry identity＋内部 revision 校验后才入队 |
| `TestRelayLateFreshDeliveryCannotClearCurrentFailure` | 旧 fresh 覆盖当前真实错误 | 保留真实 stale 与错误原因 |
| `TestRelayLateDeliveryCannotOutliveWatcherOrEntry` | unwatch／entry 重建后仍能入旧消息 | 核对 watcher identity 与 entry pointer；另补 revision 碰撞和 watcher ownership 用例 |
| Web App unit | fresh 新 stream 后旧 stream 错误重新显示“重新连接” | 退休 stream／低 seq 不改变正文、草稿或发送状态；当前真实错误继续禁用 mutation |
| Chromium 手机 viewport | 同一顺序实际产生只读 banner，发送禁用 | 页面不再被旧错误降级，且真实错误／新 snapshot 恢复仍有效 |
| `TestRelayPromotionSendFailureBelongsToItsOperation` | 已恢复／新 operation 被旧发送失败污染 | 仅当前 operation 的真实发送失败可以结束 promotion 并标 stale |
| `TestRelayHelloPrecedesConcurrentDeviceBroadcasts` | 64 次真实 WebSocket 连接竞争中复现 `device.status` 抢在 `hello.ok` 前 | 握手写入成功后才注册 Web 广播；race 30 轮通过 |
| late history unit＋手机 Chromium | stale owner 的正文和 lineage 被迟到历史覆盖，影响同 seq 恢复 | owner 一旦接管，旧 read 在后续 stale 阶段仍无覆盖权限 |

浏览器负向断言使用同一 socket 上稍后的无副作用事件作为 DOM barrier，避免旧错误尚未处理时用“未看到报错”误判成功。所有浏览器变更请求仅允许隔离 fixture，未向真实会话发送、停止、重排或审批。

## 验证

- Web unit：44 文件，371 项通过。
- Web production build：通过；无新增依赖和 wire 字段。
- 最终完整 Chromium：154 项通过，包括真实 Go Desktop Agent Service／Relay 故障注入、文件阅读／Diff／引用、登录原位恢复及 streaming。
- Go：`go test ./...`、`go vet ./...` 与 Relay／Desktop Agent／heartbeat race 通过。
- Relay 入队顺序／operation ID／握手广播回归以 race 连续运行 30 次通过，mock Agent 端到端连续 100 次通过；补充保护后重新执行全量 Go、race 与完整浏览器回归。
- 真实本机只读 compatibility detect：Desktop IPC 初始化成功；当前较新 Desktop 版本的 profile 仍标记 `unverified`，本次不引入或声称验证新的私有 IPC 能力。

## 发布与限制

发布必须同时核对公网 Relay/Web 版本、公开 JS／CSS 哈希、容器健康与资源限制，以及本机 launchd Agent 的新 binary／PID／ready／Relay 连接。保留旧容器与 Agent 配置用于回滚；关闭看板，不操作 Chilling。

线上 bundle 仍用隔离 WebSocket 重放同样故障序列；它验证真实发布产物，但不冒充真实 Native 写入或物理 iPhone／Safari。物理设备的弱网、后台恢复与当前会话仍需用户复验。此前单独记录的 lease 偶发问题没有稳定归因，不宣称因此已完全解决所有“重新连接”原因。
