# 0063：Agent 重连状态 fencing 与会话恢复

- 状态：Implemented（2026-10-11；Go／Web 单测、Go race、production build 与真实 Chromium 回归通过）。
- 来源：手机端在 Desktop Agent 短暂断线后长期停留于“正在同步会话”，并可能继续出现“事件顺序发生变化，正在重新同步”。
- 范围：Relay 的 `device.status` 生成、Web 的设备 epoch／subscription generation fencing、Desktop Agent 断线诊断与重连退避；不改变 Codex SSOT、会话正文存储、安全门禁或正常 seq 连续性校验。

## 已确认原因与未知项

1. 本机日志确认 Agent 与 Relay 的 WebSocket 曾以 `EOF` 或 `use of closed network connection` 结束；因此“Desktop Agent 暂时离线”不是历史缓存产生的假文案。
2. 当前日志把 heartbeat 主动关链也折叠成 `use of closed network connection`。缺少同一时刻的 Relay／反向代理日志时，无法确定底层首次关链来自 heartbeat timeout、网络切换还是服务端关闭；本规格不以猜测替代证据。
3. Relay 删除旧 Agent peer 后无条件广播 offline。删除锁释放到广播之间，新 epoch Agent 可以完成注册并广播 online，随后旧 peer 的 offline 反而晚到，造成状态倒序。
4. Web 收到 offline 时清空 `view`，但没有同时清空 `expectedSubscription`、递增选择 generation。迟到的旧 stream update 仍通过 `belongsToSubscription`，再因 `view === null` 被误判成 seq gap，触发第二次 resync。
5. Web 用单个 `resuming` boolean 防止并发恢复；恢复进行中到达的较新 online 信号会被直接丢弃。若当前 subscribe 随后失败，系统没有保证再次恢复，页面可能持续显示同步中。

## 行为与不变量

1. Relay 的设备状态通知必须描述发送瞬间 registry 中的当前 Agent，而不是无条件复述刚关闭的 peer：
   - 若同一 `deviceId` 已注册新 peer，只能广播 online 和新 peer 的 capabilities／epoch；
   - 只有 registry 中确实没有该设备时才广播 offline；
   - 旧 peer 清理不得把新 peer 标为 offline，也不得删除新 peer 的 route／subscription。
2. `device.status` 增加可选 `agentEpoch`：online 必须携带当前 epoch；offline 在能确认离开的当前 epoch 时携带该 epoch。字段保持 optional，以便滚动升级期间旧 Web／旧 Relay 仍可互通。
3. Web 对每个设备记录最近观察到的 online epoch。若收到属于更旧 epoch 的 offline，忽略该事件，不清空当前视图或 cache；缺少 epoch 时保持现有 fail-closed 行为，并通过 `device.list` 重新确认。
4. Web 接受当前设备的有效 offline 时，必须在同一同步步骤中：
   - 递增 selection generation，使在途 subscribe/history 响应失效；
   - 清空 `expectedSubscription`，并立即废弃当前 owner view 的事件接收权；
   - 清除该设备的 history cache；
   - 保留所选 `deviceId`、`threadId`、用户草稿和截图草稿；
   - 短暂重连窗口内可继续展示最后确认的 owner view，但必须明确标为只读并禁用全部写操作；8 秒后仍未恢复才清空展示并显示离线；
   - 此后所有旧 subscription／stream 事件只能被忽略，不能再触发 seq-gap resync。
5. 每次 online／device-list 确认都提升 recovery generation。任一时刻最多一个恢复 subscribe；若执行期间出现更新 generation，当前尝试结束后必须以最新 generation 再判断一次。没有新的 online 证据时不得无界重试。
6. 新 owner snapshot 到达前继续禁用写操作；成功 snapshot 后恢复当前会话、清除临时离线／同步 notice。真正连续流中的 `baseSeq/seq` 缺口仍按现有规则 fail closed 并重新订阅，不能为了减少提示而放松顺序校验。
7. Desktop Agent 的指数退避只累计连续握手／连接失败；一次完成 Relay hello 后应把下一次断线的 backoff 重置为初始值。断线日志应区分 heartbeat failure 与普通 read EOF，且不记录 token、会话正文或私有 IPC 内容。

## 错误与边界

- Agent 真正离线时，Web 可以显示一次明确的暂时离线状态，但不能反复叠加 offline 与 seq-gap 两个错误。
- 新 epoch online 后若 subscribe 返回确定性 `NATIVE_STATE_UNCERTAIN`／`HISTORY_TOO_LARGE`，沿用现有降级，不自动循环恢复。
- `DEVICE_OFFLINE`、transport unknown 或被新 generation 取代的恢复尝试不复用写请求，也不自动重发用户消息。
- Relay／Web 重启、浏览器刷新、多个 Web 同时订阅和多个设备同名显示不共享 recovery generation。

## TDD 与验收

1. Red：Relay 测试构造“旧 peer 已从 registry 删除、新 epoch peer 注册、旧 peer 才完成清理广播”，断言 Web 最终只看到当前新 epoch online，不出现迟到 offline。
2. Red：Web 测试先发送当前 snapshot，再发送 offline，随后发送旧 subscription update；断言 update 被忽略、只由新 online 触发一次恢复，不显示 seq-gap notice。
3. Red：Web 测试在恢复 subscribe 进行中再次收到更新 online generation，并让首次尝试失败；断言首次结束后对最新 epoch 再恢复一次，且同时最多一个 subscribe。
4. Red：Desktop Agent 测试连续连接失败会指数退避；一次 hello 成功后，下一次断线恢复为初始 backoff。heartbeat failure 的日志／返回错误保留原因。
5. 真实 Chromium 隔离 WebSocket fixture 覆盖 offline → 新 epoch online → 旧 epoch late event，断言草稿保留、旧事件不触发二次 resync、fresh snapshot 恢复会话。
6. 完成 Go 全包测试、Web 全量测试、production build 与真实 Chromium 回归；远端真实链路需结合 Relay／Caddy 日志复验断线根因，本地自动化不能冒充公网故障已完全消失。

## 实施结果

- Relay 将设备状态广播串行化，并在发送时重新读取 registry；旧 peer 清理若与新 epoch 注册交错，广播的是当前新 Agent online，而不是迟到的 offline。
- `device.status.agentEpoch` 作为 optional protocol 字段加入。Web 立即记录新 online epoch，忽略属于旧 epoch 的 offline；合法 offline 会同步废弃 selection generation、旧 subscription 的事件接收权和该设备 history cache，同时在 8 秒重连窗口内只读保留最后确认的会话内容。
- Web recovery generation 保证同时最多一个恢复 subscribe；恢复过程中到达的新 online generation 不再丢失，当前尝试结束后会基于最新设备状态再判断一次。
- Desktop Agent 在成功完成 Relay hello 后重置指数退避；heartbeat 主动关链会把原始 heartbeat failure 返回到日志，不再只留下模糊的 `use of closed network connection`。
- 自动化证明上述竞态已修复；首次公网传输断开的更底层原因仍需部署后结合 Relay／Caddy 同时刻日志确认。
- 验收证据见 [`docs/testing/2026-10-11-0063-agent-reconnect-state-fencing.md`](../testing/2026-10-11-0063-agent-reconnect-state-fencing.md)。
