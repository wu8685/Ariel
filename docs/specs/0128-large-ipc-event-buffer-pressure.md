# 0128：大会话 IPC 事件积压与有界缓冲

- 状态：Approved；用户于 2026-11-01 明确确认本规格，进入 TDD。规格确认不代表代码已部署。
- 日期：2026-11-01。
- Issue：#66。关联：0013、0093、0100、0104、0107、0121；与 #64 的冷接入 `HISTORY_TOO_LARGE` 分别验证，不把它们当作同一个故障。

## 1. 问题与证据边界

用户提供另一台 Mac 的诊断：某长会话记录文件约 146 MB，单次同步约 40–50 MB，事件积压引发 `OVERLOADED`，随后重新建立订阅／读取快照，出现反复“重新连接”。这些大小、次数和时间均为用户提供，当前开发机未直接核验目标 Mac 的日志或会话。

实施前 main `088874f` 的代码可以确定：

1. `productionLargeIPCOptions` 保持 reduced frame／reducer 上限 64 MiB、wire 上限 256 MiB，事件队列字节上限 64 MiB；未指定 `MaxEvents`，因此采用 Client 默认 64 条。
2. `Client.readLoop` 发现事件数或排队字节超限就 `fail(ErrOverloaded)`，清空队列并关闭 IPC。因此单事件合法、多个事件积压仍可能触发断线；记录文件大小不等于单帧大小，也不等于排队字节数。
3. Adapter 已接收完整 snapshot 和有 `baseRevision` 的增量 patches。patch 必须连续应用；删除中间事件后直接处理后续 patch 不能保证状态正确。
4. large follower 的进程 admission 目前按每连接 64 MiB reservation、总计 512 MiB 计算，不是完整 RSS 计量，也没有跨连接事件排队字节统计。只翻倍每连接事件限额，会同时翻倍这部分的最坏积压容量。

## 2. 本阶段目标与输入输出

输入：通过现有版本化 Adapter 验证、已完成 lazy projection 的 IPC broadcast；同一 follower 仍绑定 exact thread／owner。

输出：按照接收顺序交付完整事件；缓冲容量以内的突发积压不因旧 64 条／64 MiB 阈值而断线。超出新预算仍明确失败，不伪造连续状态。

用户已提出 1024 条／128 MiB 的调整意图。本规格同时补充共享字节约束，避免只修改两个常量而扩大多会话最坏积压。

## 3. 拟实施的最小修复

1. 仅 production large follower 显式设置 `MaxEvents = 1024`、`MaxEventBytes = 128 << 20`。128 MiB 指 134,217,728 字节，不是 128,000,000 字节。
2. 保留 reduced frame／reducer 64 MiB、wire 256 MiB，以及普通 Client 默认值；不因放宽排队容量而接受单个超限状态或放宽图片／tool projection 范围。
3. production large follower 共用一个 **512 MiB 实际排队字节预算**，每连接仍不得超过 128 MiB。512 MiB 对应旧配置 8 份 64 MiB queue 的最坏总量，不为本次放宽而增加；不得仅按会话个数估算积压。共享预算不预分配内存，也不降低现有 follower admission 数量。
4. 共享预算只计量队列实际持有的 event bytes：入队前原子申请；`NextEvent` 出队、错误关闭、显式关闭和初始化失败时精确释放；释放幂等，无负数、重复释放和关闭竞争泄漏。普通 probe／普通 follower 不自动加入新的共享池。
5. 共享排队预算与现有 reducer／controller reservation 分别计量。两者之和、正在处理的帧、解码副本及原生 wire 读入都会影响 RSS；**不声称 Agent RSS 被此池限制在 512 MiB，也不把公网 Relay 1 GiB 容器限制当作本机 Agent 的保护**。测试记录实际峰值 RSS，并明确单机／并发条件；不通过提高现有预算来掩盖失败。
6. 单队列条数、字节或共享池不足时保持既有 `OVERLOADED` 与 fail-closed 行为。错误后不依据旧状态发送消息、停止或审批，不自动重放 mutation，不增加 follower 或轮询。
7. 为容量诊断记录脱敏原因类别（事件数／队列字节／共享字节）、当时计数及配置上限；不记录正文、路径、真实 thread／owner ID 或原始 IPC 内容。明确区分 `HISTORY_TOO_LARGE`，不能把所有容量问题统一归为网络异常。

## 4. 不在本阶段实施

- 不在队列满时随意丢弃／覆盖 patches，不把最新单个 patch 当成完整状态。
- 不阻塞唯一 IPC reader 等待消费者，从而阻塞后续 RPC receipt／discovery 或制造未知 mutation 结果。
- 不新增未经实测的“从某 revision 补拉增量”私有接口，不通过重新读取完整历史冒充增量恢复。
- 不修改 Relay／Web wire、最近两 turn cache、正文 5 秒合并、文件能力或 Chilling。

后续若需根治持续消费落后，应先测量 reducer／projection 的耗时与分配，再设计有严格 revision／owner 证明的批量归约或完整 checkpoint 合并。若当前 Desktop 确有 replay API，需另做只读兼容性探测并更新规格；本阶段不宣称这一能力存在。

## 5. TDD 与验收标准

确认后先实际运行 Red，再实现最小修复：

1. 生产选项测试先证明旧配置缺少 1024 条／128 MiB；帧 64 MiB／wire 256 MiB、普通 Client 与现有 follower admission 不变。
2. IPC 确定性暂停消费者：超过 64 条的小事件、合计超过 64 MiB 但不超过 128 MiB 的合法事件，以旧配置复现 `OVERLOADED`，新配置完整按序排空、revision 连续、连接可继续返回普通 RPC receipt。大字节用例采用合成内容，不保存真实会话。
3. 1024 条／128 MiB 的等于及超过边界；单帧／reducer 超过 64 MiB 仍被拒绝；持续超载明确失败，不能静默丢事件。
4. 多 Client 共享预算的申请、独立配额、出队复用、初始化失败／Close／overflow 释放、并发竞争与无泄漏；用缩小的等价预算覆盖精确边界，另以真实 128 MiB 配置做容量回归。
5. follower snapshot → 多批 patches → completed 的内容与终态正确，旧 owner／revision gap／reset 仍拒绝；零 native mutation，无额外重连／load-complete-history 请求。
6. Go 全量／vet／关键包 race；必要时复用 Unix IPC → Agent → Relay → Chromium 隔离 fixture，验证草稿保留、正文及完成状态及时可见、正常发送门禁恢复，不只断言 socket 未关闭。
7. 真实目标 Mac 只读核验必须记录版本、事件峰值、帧与队列字节、重连次数和 Agent RSS。无法访问该主机时标记 `unverified`，不得用开发机／合成测试通过宣称另一台 Mac 已修复。

## 6. 实施与发布边界

用户已于 2026-11-01 确认本规格，按 TDD 实现、验证并按 Ariel 的 Git 约定提交／push。未更新其他 Mac、未打 tag 或重启本机／公网服务；后续部署范围以用户要求为准。旧异步问题 PR 不混入此修复。

## 7. 本轮实现与证据

已完成 1024 条／128 MiB large profile、512 MiB 共享实际排队池、精确释放与脱敏容量日志。旧 64 条／64 MiB 故障在隔离 IPC 中先复现；新边界、关闭竞争、follower 连续 patches 与完成状态、Go／race／Web／Chromium 回归通过。生产 timeout／单帧／reducer／wire 限额未变，没有实现未验证的增量 replay。目标 Mac 未直接访问或更新，实际恢复效果仍 unverified。

详细命令、首轮 race fixture 超时及修正、约 128 MiB 排队时测试进程实际峰值，见 [0128 验收记录](../testing/2026-11-01-0128-ipc-event-buffer-pressure.md)。
