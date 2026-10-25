# 0108：登录失效与协议异常分离

- 状态：Implemented（2026-10-26）。
- 来源：公网 Passkey 页面偶发跳回登录界面，重新登录后再次跳回，形成循环。
- 关联：[0084 Passkey 滑动会话与本地持久恢复](0084-passkey-sliding-session-and-durable-resume.md)、[0107 Agent Transport 解耦与 Cache 自愈恢复](0107-resilient-agent-transport-and-cache-recovery.md)。

## 已确认事实

1. 公网当前容器及前两个回滚容器使用相同的 `ARIEL_SESSION_KEY` 指纹，当前 Relay `RestartCount=0`；频繁跳转不是 session key 轮换或容器重启导致。
2. WebSocket 客户端当前把以下情况统一折叠为 `invalid`：
   - Relay 明确以 `1008 / unauthorized` 拒绝认证；
   - Relay 以 `1008 / invalid hello|invalid message|invalid role or message` 拒绝协议消息；
   - Web 收到无法解析、无法通过当前 schema 校验或时序不允许的消息。
3. Passkey 页面把所有 `invalid` 都解释为“登录会话已失效”，清空当前选择和显示缓存并回到登录页。协议问题在重新登录后仍会再次发生，因此形成循环。
4. 现有运行信息没有保留浏览器看到的 close reason，无法从服务端历史日志反推出本次具体是哪一帧触发。旧 Web 页面跨版本保持打开后收到新协议消息，是高概率触发条件，但在补充可观测性前不得冒充已证实事实。

## 用户行为

1. 只有 Relay 明确关闭为 `1008` 且 reason 精确为 `unauthorized` 时，Web 才进入登录失效状态：
   - Passkey 模式显示“登录会话已失效”；
   - PIN 模式按现有规则清理已拒绝的 Relay session。
2. `invalid hello`、`invalid message`、`invalid role or message`，以及 Web 本地 JSON/schema/消息时序校验失败，统一进入独立的 `incompatible` 状态：
   - 不把它表述为登录失效；
   - 不调用 logout，不删除 Passkey Cookie，也不清理浏览器持久会话正文缓存；
   - 停止自动重连，避免同一不兼容条件造成循环；
   - 页面明确提示“页面版本与 Relay 不兼容，请刷新页面”，提供刷新按钮。
3. 普通网络中断、heartbeat 超时和 `1006` 继续走现有自动重连；保留最后确认的会话内容和草稿，owner 未恢复前禁用 mutation。
4. 若不兼容发生时已有 stale view，继续展示正文并在会话区域显示阻塞提示；若没有 view，显示独立兼容性面板，不显示 Passkey 登录表单。
5. 刷新后仍不兼容时保持明确错误，不自动发起 Passkey ceremony。

## 安全与日志边界

1. close reason 只允许映射到固定类别：`unauthorized`、`protocol`、`transport`；不得把原始帧、Cookie、token、会话正文或任意对端字符串写入日志或 DOM。
2. 客户端不得仅凭 close code `1008` 判定认证失败；必须同时校验固定 reason。
3. 未知 `1008` reason fail closed 为 `incompatible`，不能回退成自动重连或要求用户重新认证。
4. 本规格不延长 Cookie 生命周期，不改变 WebAuthn ceremony、Relay session key 或认证边界。

## TDD 与验收

1. Red：client 测试证明 `1008 / unauthorized` 进入 `invalid`，`1008 / invalid message` 和未知 `1008` 进入 `incompatible` 且不重连。
2. Red：client 测试证明无效 JSON、schema 不兼容和 hello 后的意外消息进入 `incompatible`，不被后续 close 覆盖成 `disconnected`。
3. Red：App Passkey 测试证明只有 `unauthorized` 显示登录失效；协议异常显示刷新提示且不出现“使用 Passkey 登录”。
4. Red：已有会话正文时发生协议异常，正文与本机 durable cache 保留、mutation 禁用，并显示兼容性阻塞提示。
5. 普通 `1006` 重连、前后台恢复、Passkey 登录和 PIN session 回归保持通过。
6. 运行 Web 全量测试、production build 与真实 Chromium 回归；物理 iPhone/Safari 仍由用户在公网发布后复验。

## 回滚

仅回滚 Web 状态分类和兼容性 UI。认证 Cookie、Passkey 凭据、Relay 数据及 Codex 会话无需迁移。

## 实现与验证

- Web unit：31 个测试文件、263 项测试通过。
- Chromium UI：48 项测试通过，其中包含手机 viewport 下的协议异常专项回归。
- Production build：通过；仅保留既有的大 chunk 提示。
- 物理 iPhone / Safari：待公网发布后由用户复验。
