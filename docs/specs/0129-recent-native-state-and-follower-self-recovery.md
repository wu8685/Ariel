# 0129：大会话旧正文回收与按需订阅自恢复

- 状态：Implemented（2026-11-01；用户确认后按 TDD 完成自动／只读 fixture 验证，已授权发布 v0.13.3；首次入站超限仍由 #64 跟踪，部署结果另记于 Release）。
- 日期：2026-11-01。
- Issue：#68；首次入站 frame 超限继续由 #64 跟踪。
- 依据：0100、0104、0107、0121、0128。扩展 0128 明确未实施的 reducer 减负；不将“扩大缓冲区”当作持续吞吐不足的完整修复。

## 1. 问题、证据与非结论

用户反馈另一台 Mac 的长会话在约 50 MB 同步之后出现 OVERLOADED，随后 RESYNC_REQUIRED，其他较小会话继续更新。原记录文件大小、时间和目标机运行版本尚未由开发机直接核验；不提交真实会话名称、正文、ID 或原始日志。

当前 main 的代码可以确定：

1. Relay 的规范化缓存已只保留最近两个 turn。`NormalizeLive` 却先解码全部原生 turn／items，再选最近两个；这一限制不等于 Agent 的原生状态也只有两个 turn。
2. `Reducer.Apply` 对每批 patch 解码上次整份状态，应用 patch，再编码整份状态；`observationState.apply` 随后复制并解析它，`Follower.Current` 再复制。旧正文会重复参与处理，即使此次只改变一个很小的字段。
3. native patches 依赖 exact owner、baseRevision 和原生路径。直接删除旧数组元素、丢弃排队 patch 或把最近一个 patch 当完整状态，会破坏增量定位／连续性。
4. follower 结束时 `Service.pump` 失效当前 live 和订阅，Agent 删除对应 cache subscription；恢复主要依赖后续显式 cache promotion／transport reconnect。Web 将 RESYNC_REQUIRED 和 OVERLOADED 作为终止本轮同步的原因，不会仅因 Relay 长连接仍正常而重建该 follower。
5. 既有测试覆盖“失败后显式 promotion 能重建”，没有证明“不刷新、不点击、传输未断的情况下能自主重建”。本轮重跑显式恢复及 burst 测试通过，不作为本规格新行为的验收。

**边界**：没有验证 native 支持最近两 turn／字段选择／按 revision replay 的订阅接口。Agent 丢弃旧正文不会让 Codex App 首次发出的完整 snapshot 自动变小；记录文件大小也不能代替单帧、排队、归约状态或 RSS 的测量。

## 2. 输入、输出与目标

输入仍为版本化 Adapter 已验证来源的 native snapshot／连续 patches，以及 Relay 已建立的会话关注需求。

输出仍为既有两 turn cache、只读历史分页与 follower 更新；不新增 Web 私有 IPC，不改变 Codex SSOT 或 mutation owner。

目标：

1. Agent 的常驻会话状态不再保留全部历史正文；默认保留最近两个 turn 的正文及必要当前交互上下文。
2. 小增量不再触发全部历史正文的重复 JSON 解码／编码／复制，完成状态仍即时发布。
3. 单个 follower 因可恢复错误失效时，只要该会话仍被关注，就自动退避重建；不用用户刷新，不重启 Relay／Agent，不影响其他小会话。
4. 不删除、截断、压缩或改写 Codex 的原始记录，不扩大本阶段既有容量限制，不自动重放任何写请求。

## 3. Agent 内的旧正文回收

### 3.1 常驻表示与回收时机

1. 在 Adapter 内采用可连续应用 patch 的常驻结构；snapshot 解码后建立基线，patch 只解码变更值并修改对应位置。不为每条 patch 重建完整 JSON 字符串；需要输出时才生成有界投影。
2. 已完成且离开最近两个 turn 窗口的正文、图片载荷和 tool 详情及时释放，而非等事件队列满后才清理。旧 turn 保留必要的轻量身份、顺序、状态及原生路径骨架，数组索引与实体 key 不重编号；骨架也受字节限制，不承诺无限历史零成本。
3. 当前 active turn、pending question／approval 所引用的必要内容，以及仍在对账的本次 mutation 证据不可按“旧 turn”盲目回收。若这些必需内容自身超过预算，明确容量失败，不伪造可写状态，也不能静默截断文本。
4. 不把不同语义的缺失内容当空正文；内部回收标记与原生用户字符串区分。对已回收子树的纯详情更新，只推进经验证的 revision 而不重新累积正文；身份、顺序、运行／交互等结构变化仍必须处理或明确拒绝。
5. snapshot 重建、turn 回滚、删除／插入、旧 turn 重新进入最近窗口都必须得到真实内容后才能发布为完整。不能把回收标记渲染到 Web，也不能把已回收的 turn 冒充空 turn。需要补充内容时复用已验证的只读路径；没有安全补齐路径则保留旧视图并明确失败。
6. 只在已验证的 canonical／legacy turn 结构中做回收。未知 native 类型、无法确认顺序或身份的结构不得猜测裁剪；继续受限拒绝并报告兼容性原因。

### 3.2 容量与生命周期

1. native reader 保留当前 wire 256 MiB／projected frame 64 MiB 上限，0128 的队列与共享预算不变。本阶段保证限额内的大状态可在建立基线后瘦身，不声称已解决尚未进入 reducer 就超限的所有 snapshot（另见 #64）。
2. 常驻结构、导出投影、历史索引、正在处理的 frame 和队列分别计量；实际 RSS 不等于 JSON 字节数。释放旧正文必须切断引用，不能只缩短 slice 而让底层大对象继续被持有。
3. 未知字段仍遵守原兼容与限额，不为让测试通过而剔除所有未知 metadata；既有 source／host／cwd／owner／revision 和 UTF-8 校验不放宽。
4. 更早正文继续走现有按需分页，Web 每次“加载更早”最多三个 turn；文件原文／Diff／引用／图片／tool 详情继续按需读取。回收本机状态不得让这些入口消失或以缓存正文代替来源证明。
5. mutation 所需证明不能从只读 recent cache 推断。受回收影响的现有发送／停止／审批／回答／队列／编辑校验必须逐一回归；无法取得完整证明时拒绝写入，不降低门禁。

## 4. 仍被关注会话的自动恢复

1. Agent 保存独立于 live 实例的需求与恢复状态：会话关注、generation、恢复尝试、退避 timer、取消句柄。native follower 的失败不等于 Web 已取消关注。
2. OVERLOADED、连续性丢失／RESYNC_REQUIRED、临时 IPC 断开或 timeout 属于只读重建候选；每会话最多一个恢复任务，复用现有 attach／cache load 并发门禁，不为每个 Web 新建 follower。
3. 使用带抖动的指数退避，基准为 1、2、4、8、16、30 秒，之后封顶 30 秒。单次尝试沿用有界 load timeout；仅在仍存在关注需求时继续尝试，不轮询健康 follower，不定时重读健康会话。
4. 容量拒绝、未知协议／形状、认证失效、确认永久不存在等原因不得无休止整份重传。对确定性容量错误保留原视图，提供明确失败原因和显式重试；持续原生队列过载且恢复没有实际进展时触发熔断，避免“接回即断”的重传循环。具体进展判据与阈值在实现前用测试固定：成功接回且消费到比故障前更新的有效 revision／终态，才算进展；连续三次无进展重建后停止自动尝试，需显式 promotion 或新 Agent／owner generation 才重新允许。
5. 重建成功后通过新 cache stream 的首个合法 snapshot 恢复 freshness，之后等普通 follower updates；成功 RPC receipt 或连上 socket 不算已恢复。旧 timer／旧 follower／迟到错误不得清除新实例或使其回退为只读。
6. 多客户端仍只共享同一会话 follower。取消最后一个 watcher 后按既有 warm／active 规则撤销无需求恢复；Agent 关闭、Relay transport generation 变化时取消旧任务，下一次连接使用当前需求恢复。
7. 失败期间 Web 保留正文、草稿与文件现场，继续禁用依赖实时证明的写操作。恢复成功自动更新并解除只读，无页面刷新、重新登录或 mutation 自动重发。

## 5. TDD、兼容性与性能验收

获批后先写并实际运行 Red，再实现 Green：

1. 合成约 50 MiB 历史正文＋最近两个小 turn 的 owner state；旧实现证明常驻／导出随全部正文增长，新实现只保留窗口、必要 pin 与索引；原始 fixture 在运行时生成，不提交真实大文件。
2. 连续小 patch／新 turn／completed 的内容、顺序、revision 和交互状态正确；加入中间历史详情 patch、数组插入删除、回滚、字段顺序变化、未知格式及预算边界。故障前后不丢正文，不仅断言“没有断线”。
3. benchmark 分别测首次 snapshot 与 steady-state 小 patch 的 wall time、分配和 RSS；历史正文增大时稳态成本不得与旧正文大小线性增长。记录实现前后同机、同 fixture 结果，不以易抖动的固定毫秒数作为唯一 CI 断言。
4. 隔离 IPC → Agent → Relay → Chromium：一次 overload／revision gap 后，不断 Relay 长连接、不手工 promotion、不刷新页面，自动恢复到新正文和 completed；其他两个小会话持续更新，草稿／文件现场保留，native mutation 为零。
5. 退避、取消、并发去重、熔断、真正容量失败、无 watcher、Agent 关闭、epoch／owner 替换，以及旧错误迟到均有确定性时钟测试；证明没有忙重试或泄漏。
6. 旧历史每页最多三 turn，文件阅读／Diff／正文链接与选区引用、pending interactions、设置与 mutation gates 均回归；Go 全量／vet／关键包 race，Web／真实 Chromium 验证通过。
7. 只读 native compatibility gate 验证当前 Desktop 真实 shape 和连续性；只记录计数／大小／耗时／脱敏原因。目标另一台 Mac 的版本、过载原因、自动恢复与内存需直接核验；无法访问时明确 unverified，不以本机测试代替。

## 6. 实施与发布边界

本规格改变 Agent 的常驻状态表示和会话失败后的自动恢复策略，用户已明确批准。按 TDD 实现并验证后提交／push；发布仍需明确执行并分别核对目标 Agent 与 Relay，不能把“代码实现”写成“目标主机已修好”。

首次完整 native snapshot 仍可能较大，这一限制必须在发布说明中明确。未经兼容性验证不得宣称 Codex App 已支持源头裁剪，也不为了完成本阶段而抬高 64 MiB／256 MiB 上限。

## 7. 实现与当前验收边界

已实现常驻 decoded reducer、已知旧正文回收、必要写入证据 pin、独立关注需求与有界退避恢复、无进展熔断及错误分类。先在原行为上实际复现旧正文保留、失效后无自动恢复；也用 main v0.13.2 隔离构建运行同一真实浏览器用例，两个缺陷均复现。测试记录见 [0129 验收记录](../testing/2026-11-01-0129-recent-state-and-recovery.md)。

当前真实超长会话只读检查已触发 reader 阶段的 `projected output exceeds 67108864`，尚未进入本规格的 reducer 回收阶段；该实际目标不能报告为已修复。另一隔离 Codex Desktop fixture 的只读 cold attach／两 turn normalize 通过，不能代替超限会话或另一台 Mac 的验收。未更改现有安全限额，也没有据此自动重启线上服务。
