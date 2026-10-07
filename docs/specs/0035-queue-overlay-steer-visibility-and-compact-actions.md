# 0035 队列菜单、引导消息可见性与紧凑输入操作

- 状态：Implemented（自动测试与 production build 已验；物理手机待用户复验）
- 来源：用户在手机端实际使用后报告：排队项“更多”菜单被输入框遮挡；排队项“引导”成功后，补充内容没有显示在对话中；输入框底部三个操作 icon 视觉尺寸偏大。

## 行为规格

1. 排队项“更多”菜单必须脱离队列滚动容器的裁剪上下文，以 viewport 浮层显示，并高于输入框；优先在触发按钮上方展开，空间不足时可在下方展开。
2. 菜单保持现有“编辑 / 上移 / 下移”语义。点击菜单内部不得被外部点击监听提前关闭；滚动、缩放或点击菜单外部时关闭，避免浮层与触发项错位。
3. Codex Desktop 已确认接受的原生 `steeringUserMessage` 必须被投影为当前 turn 中的用户消息，并展示其文字和截图；未接受的临时 steering 状态不伪装成已发送消息。
4. `queue.steer` 的 owner、turn、queue 和 client message 身份校验及“确认后才删 durable queue item”的边界不变；Ariel 不创建第二份持久队列或伪造 turn。
5. 手机端附件、停止、发送三个 icon 的视觉尺寸缩到接近 14px 正文字号；按钮仍保留至少 36px 的触控区域和 accessible name，不改变发送、停止或附件行为。

## TDD 与验收

1. Web 测试验证“更多”菜单挂载到页面级浮层，位于队列和输入框裁剪上下文之外，且菜单项仍可操作。
2. Desktop Agent 测试先用原生 `steeringUserMessage` 复现正文丢失，再验证 accepted 项投影为 `role=user` 的完整文字；图片引用随同保留。
3. 样式回归验证手机端三个操作按钮使用紧凑视觉 icon，同时保留触控尺寸。
4. 完整 Web tests、production build、Go race tests 和 `go vet` 通过后，重启本地 Ariel，并以 LAN health endpoint 验证部署。

## 非目标

- 不改变 Codex queue 的执行顺序、Steer 语义或 owner 权限边界。
- 不在 Web 中保存已引导消息的持久副本；消息内容仍来自 Codex 原生会话状态。
- 不重做输入框结构或队列交互信息架构。
