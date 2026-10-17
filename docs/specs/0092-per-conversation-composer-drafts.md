# 0092 按会话隔离的 Composer 草稿

- 状态：Approved（用户于 2026-10-17 确认）
- 日期：2026-10-17
- 影响范围：Ariel Web
- 不涉及：Relay／Desktop Agent 协议、Codex 会话正文、服务端草稿数据库

## 1. 背景与目标

当前 Ariel Web 只有一组 App 级 `draft / screenshots / fileQuotes` 状态。用户在会话 A 输入内容后切换到会话 B，B 会继续显示 A 的草稿；这既不符合使用预期，也可能把正文或附件发送到错误会话。

本规格将未发送的 composer 状态按会话隔离。切换会话时，Web 保存离开会话的本地草稿，并立即恢复目标会话上次留下的草稿；从未填写过的会话显示空 composer。

## 2. 草稿身份与数据

### 2.1 身份

正式会话草稿使用以下复合 identity：

```text
deviceId + threadId
```

- 不使用标题、cwd、列表位置或当前 Agent epoch 作为草稿身份。
- 同一个 `threadId` 在不同设备下不得共享草稿。
- Agent 重连或 epoch 更新时，只要 device 与 thread identity 不变，页面内草稿继续保留。
- 新会话尚未完成 bootstrap 时使用独立的页面内临时 identity；创建成功后按第 3.5 节迁移或清理。

### 2.2 草稿内容

每个会话的 composer 草稿至少包含：

- textarea 正文；
- 尚未发送的截图；
- 尚未发送的文件引用；
- composer revision；
- 如果正在编辑排队消息：`editingQueueId` 及其本地编辑内容。

草稿不包含会话正文、历史消息、subscription、owner 状态或服务器回执。

## 3. 行为规范

### 3.1 切换会话

1. 从会话 A 切换到会话 B 前，Web 将 A 当前 composer snapshot 写入 A 的页面内草稿槽位。
2. 目标 identity 确定后，Web 在同一次切换中恢复 B 的 snapshot：
   - B 有草稿：展示 B 的正文、截图和文件引用；
   - B 没有草稿：展示空 composer。
3. 不得在 B 中短暂绘制 A 的草稿；owner subscription、历史缓存或网络响应再慢，也不能造成一帧跨会话内容泄漏。
4. 返回 A 时恢复 A 最新的未发送草稿，不使用 B 的内容覆盖。
5. 重复点击当前会话不重置草稿，也不改变 composer revision。

### 3.2 Composer 的全部入口

下列操作都必须更新当前会话自己的草稿槽位：

- textarea 输入、粘贴、清空；
- 添加或删除截图；
- 从文件查看器添加或删除文件引用；
- 开始、修改或取消排队消息编辑；
- 发送成功、失败或 outcome unknown 后的状态变化。

任何异步 callback 都必须携带创建时的草稿 identity，不能依赖 callback 执行时的“当前会话”。

### 3.3 发送与异步回执

1. 发送请求 snapshot 必须绑定 `deviceId + threadId + composerRevision`。
2. 发送被精确确认且原草稿 revision 未变化时，只清空发送来源会话的草稿。
3. 如果用户在请求完成前切到另一个会话，迟到的成功回执不得清空当前会话的 composer。
4. 发送期间用户继续修改来源草稿时，旧回执不得清空新内容。
5. rejected、timeout 或 outcome unknown 继续保留来源会话草稿，遵循现有发送确认规则。

### 3.4 截图与文件引用安全边界

1. 截图和文件引用与正文使用同一个会话 identity；不得单独保留为全局附件。
2. 发送按钮的 enabled 状态仅根据当前会话草稿与当前 owner 状态计算。
3. 切换到空草稿会话后，即使来源会话存在截图或文件引用，也不得允许发送一个“看不见的附件”。
4. 文件引用中的 thread identity 必须与草稿 identity 一致；不一致时拒绝恢复并移除该异常引用，不静默改写其来源。

### 3.5 新会话草稿

1. 打开“新建会话”并创建本地未命名草稿时，使用独立临时 identity，不覆盖此前正式会话草稿。
2. 用户从本地新会话草稿切回已有会话时，本地草稿仍按现有产品规则要求显式放弃或完成，不借本功能绕过确认。
3. bootstrap 成功并获得真实 `threadId` 后：
   - 已精确确认发送的首条正文和截图从临时草稿清空；
   - 如果首条发送期间产生了更新后的未发送内容，将其迁移到新 `threadId`，不能丢失；
   - 不得把创建前所在会话的草稿迁入新会话。

### 3.6 排队消息编辑

1. 切换离开正在编辑排队消息的会话时，保存 `editingQueueId` 与编辑内容。
2. 返回后，仅当 fresh/cached thread view 仍包含同一个可编辑 queue item 时恢复编辑态。
3. queue item 已不存在或不可编辑时，不自动提交本地内容；退出编辑态、保留为普通未发送草稿，并提示“原排队消息已变化，请核对后重新发送”。

## 4. 生命周期与隐私

1. 草稿缓存只存在于当前 Web 页面的有界 JavaScript 内存中。
2. 本版本不把正文、截图或文件引用写入 Relay、Desktop Agent、`localStorage`、`sessionStorage` 或 IndexedDB。
3. 页面刷新、标签页关闭、显式退出登录或 Web session 失效后，草稿清空。
4. 暂时的 WebSocket 重连、Agent 重连、应用前后台切换不清空页面内草稿。
5. 切换设备只改变当前展示的草稿 identity，不删除原设备会话的页面内草稿；切回原设备后可恢复。

## 5. 实现不变量

1. 使用同步 ref/store 保存权威 composer snapshot，React state 只负责渲染；不能依靠异步 `setState` 的提交顺序完成“先保存 A、再恢复 B”。
2. active composer identity 与 `selection.current` 的 device/thread identity 同步更新。
3. 每个草稿有独立 revision；修改 B 不得递增 A 的 revision。
4. 历史会话缓存和 composer 草稿缓存相互独立，清理历史 DOM 不得清理草稿。
5. 不改变现有 Codex SSOT：草稿仍是尚未提交的 Web 本地状态，不进入会话历史。

## 6. 错误与降级

1. 目标草稿格式异常时，目标会话以空 composer 打开并显示本地提示；不得回退到来源会话草稿。
2. 附件恢复失败时保留可验证的正文，移除异常附件并提示用户。
3. 会话选择失败或超时时，草稿仍归属于用户所选择的目标 identity；重新选择来源会话时应恢复来源草稿。

## 7. TDD 验证计划

实现按 red → green → refactor 执行。

### 7.1 单元测试

- A 输入正文 → 切 B 显示空 → B 输入正文 → 切 A 恢复 A。
- 同名 thread 位于不同 device 时草稿互不共享。
- 正文、截图、文件引用作为一个 snapshot 保存和恢复。
- 修改 B 只递增 B 的 revision。
- 发送 A 后切到 B，A 的迟到成功回执不清空 B。
- A 在发送后继续输入，旧 revision 回执不清空 A 的新内容。
- outcome unknown 保留来源草稿。
- queue item 消失时安全退出编辑态并保留普通草稿。
- logout 清空全部草稿，WebSocket／Agent 重连不清空。

### 7.2 真实浏览器测试

- 手机 viewport 下在两个会话间往返，分别恢复各自正文。
- 会话点击后立即断言 textarea，确认没有绘制前一个会话草稿的中间帧。
- 来源会话携带截图与文件引用，目标会话没有隐藏附件且发送按钮状态正确。
- owner snapshot 延迟、切换竞态与迟到发送回执下，草稿 identity 保持正确。
- 切换回来后 composer 高度按恢复内容重新计算，不遮挡消息区或发送按钮。

## 8. 验收标准

1. 任意两个会话可以分别保留不同的未发送正文，来回切换时恢复正确。
2. 从未填写过的会话始终显示空 composer，不出现其他会话内容。
3. 截图和文件引用不会跨会话泄漏或被误发。
4. 慢加载、重连、发送回执竞态均不会清空错误会话的草稿。
5. 页面刷新后草稿消失；Relay 和 Codex 不保存草稿正文。
6. 现有增量历史、发送确认、队列、新会话 bootstrap 与移动端 composer 稳定性测试全部通过。
