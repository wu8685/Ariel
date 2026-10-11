# 0069：已确认发送后可靠清空输入框

- 状态：Implemented（用户于 2026-10-11 确认；Go／Web／390×844 Chromium 已验）
- 来源：用户发现点击发送后，输入框中的文本偶尔没有被清空。
- 关联：[0004 M2 真实 Desktop Agent](0004-m2-real-desktop-agent.md)、[0031 Codex 原生后续输入队列](0031-codex-follow-up-queue.md)、[0063 Agent 重连状态 fencing](0063-agent-reconnect-state-fencing.md)、[0065 新建会话 bootstrap 订阅](0065-new-thread-bootstrap-subscription.md)、[0066 运行中输入焦点稳定性](0066-running-composer-focus-stability.md)。
- 范围：普通发送、运行中 `queue.add`、排队消息编辑的 composer 清理与精确送达对账；不自动重发，不用正文相等推断送达。

## 根因

1. Web 当前在发送前记录 `pendingSelect.current`，只有回执到达时该 selection generation 完全不变才处理清空。
2. 同一 device／thread 若在发送期间因 `RESYNC_REQUIRED`、stream gap、短暂重连或自动重新订阅调用 `selectThread`，generation 会变化。即使 `turn.start` 已明确返回 accepted，`stillSelected` 也会变为 false，整个 composer 清理分支被跳过。
3. `turn.start` 的 `clientMessageId` 当前在 `send()` 内临时生成，没有保留为待对账记录；规范化 user item 也没有暴露原生 `clientId`。因此回执为 unknown 后，后续 live snapshot 即使已经包含该消息，Web 仍无法按身份确认并清空草稿。
4. `queue.add` 的 live `queuedMessages` 已包含 `clientMessageId`，但 Web 同样没有保留本次提交 ID，无法利用已有证据完成 unknown 对账。
5. 结果 rejected、not_submitted 或仍无法确认的 unknown 保留草稿是既有安全语义，不属于本 bug；不能为追求视觉上的立即清空而丢失未确认内容。

## 目标行为

1. 用户点击发送后，在 mutation 未完成期间保持当前草稿和 disabled 发送按钮，沿用现有“先确认、再清空”语义。
2. 收到明确 accepted 时，只要当前语义选择仍是同一 device／thread，且 composer 内容自提交以来没有被用户或队列编辑流程修改，就清空本次文本和截图；同一会话的 resync／重新订阅 generation 变化不得阻止清空。
3. 用户在等待回执期间继续输入、删除、粘贴、添加／移除截图、开始编辑队列项或切换到其他 device／thread 后，不得清空新的内容。
4. rejected、not_submitted 保留原草稿并显示现有错误；unknown 保留草稿、禁止自动重发，并显示“执行结果不确定，正在等待会话状态确认”。
5. unknown 之后，若身份匹配的 live snapshot／update 证明本次提交已进入 Codex：
   - `turn.start`：存在 `role=user` 且 `clientMessageId` 精确相等的 item；
   - `queue.add`：存在 `clientMessageId` 精确相等的 queued message；
   则把本次提交升级为 confirmed，并按第 2 条的 composer revision 规则清空。
6. live 对账确认时提示“消息已确认发送”；如果 composer 已被修改，只清除 pending 状态和不确定提示，不改动新草稿。
7. 文本内容相同、turnId 相近、item 顺序、runtime 变为 active 或标题更新时间都不能作为送达证据。只能使用 exact client message identity。
8. 页面刷新、主动切换设备、Agent epoch 改变或待对账记录过期后停止对账；保留当前草稿，不跨生命周期猜测结果。

## 身份与协议

1. Web 每次 `turn.start`／`queue.add` 先生成并保存同一个 `clientMessageId`，记录 `{deviceId, agentEpoch, threadId, kind, clientMessageId, composerRevision, text, imageRevision, submittedAt}`。
2. composer revision 在所有用户输入、粘贴、截图增删、队列编辑载入／取消和程序化清空时单调递增。不能仅比较 `current === submittedText`，因为用户可能先修改后又输入相同文本。
3. Ariel `Item` 新增可选 `clientMessageId`，仅允许出现在 `role=user`：
   - 原生 `userMessage.clientId` → `clientMessageId`；
   - 已 accepted 的 `steeringUserMessage.clientUserMessageId` → `clientMessageId`；
   - assistant、system、未知 item 不得携带。
4. Desktop Agent 只投影格式合法且有界的 ID，不把其他 owner／request 私有字段暴露给 Relay 或 Web。该 ID 只用于本地提交对账，不建立持久化 receipt 数据库。
5. 旧 Agent 缺少 item `clientMessageId` 时协议仍兼容：明确 accepted 回执仍可修复 resync 场景，unknown 则继续保留草稿并提示无法确认。

## 有界状态与竞态

1. Web 同时最多保留 8 条待对账发送，每条最长 2 分钟；达到上限时拒绝新的发送并提示 overloaded，不淘汰尚未确认的记录来假装成功。
2. 同一 composer 同时只有一个 foreground submission；运行中 queue 仍可有多条 Codex durable queue，但 Web 仅跟踪由当前页面发起且尚未得到最终证据的提交。
3. accepted response 与 live confirmation 可以任意先后到达，状态转换必须幂等：同一 `clientMessageId` 最多触发一次清空和一次确认提示。
4. 旧 subscription／stream、错误 device、错误 thread、旧 Agent epoch 和已取消 pending record 的事件不得清空 composer。
5. 自动 same-thread resync 不取消 pending record；用户显式选择另一 thread、切换 device、登出或 Agent epoch 变化会取消记录但不清空草稿。
6. queue update 不生成新 client message identity；其明确 accepted 清理由 queueId、同一语义选择与 composer revision 共同约束。unknown 时保留编辑内容，不用后来同文 queue item猜测成功。

## TDD 与验收

1. Red：复现普通发送过程中收到 `RESYNC_REQUIRED` 并重新订阅、随后原请求 accepted；现有代码下草稿残留，修复后清空且只发送一次。
2. Red：复现 stream gap 触发 same-thread resync，以及短暂 Agent reconnect 后同 epoch 恢复；明确 accepted 均能清空原草稿。
3. Red：在请求等待期间修改文本、修改后恢复相同文本、添加／删除截图，accepted 均不得清空新 revision。
4. Red：切换 thread／device、Agent epoch 变化后旧 accepted 或旧 live item 不得清空当前 composer。
5. Red：unknown 后保留草稿且不重试；随后 exact `clientMessageId` user item 到达时清空未修改草稿，错误 ID、只有相同正文或只有 active runtime 时不清空。
6. Red：`queue.add` unknown 后用 queued message 的 exact ID 对账；`queue.update` unknown 继续保留编辑内容。
7. Red：normalize／协议测试验证 userMessage 与 accepted steering message 的可选 `clientMessageId`，拒绝 assistant/system 携带、超长或格式非法 ID。
8. Red：pending registry 覆盖 8 条／2 分钟上限、重复事件幂等和页面生命周期清理。
9. 真实 Chromium fixture 覆盖移动端点击发送、same-thread resync 后输入框清空、unknown 先保留后确认清空，以及发送期间继续输入不被清除；同时断言发送请求始终只有一次。
10. 运行 Go 全量／race、`go vet`、Web 全量、production build 和全部 Playwright；公网部署后由物理 iPhone／Safari 复验。

## 非目标与回滚

- 不在点击瞬间无条件清空，不以牺牲未确认草稿为代价制造“已经发送”的假象。
- 不自动重试任何 `turn.start`、`queue.add` 或 `queue.update`。
- 不按正文、时间或 runtime 猜测送达，不在 Relay／Agent 落盘保存草稿或 receipt。
- 可单独关闭 live receipt reconciliation；关闭后仍保留 accepted response 跨 same-thread resync 的清理修复。
