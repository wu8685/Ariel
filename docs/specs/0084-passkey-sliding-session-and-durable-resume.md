# 0084：Passkey 滑动会话与本地持久恢复

- 状态：Implemented（2026-10-15；Go、Web、race、真实移动 Chromium 与公网 Docker 已验；物理 iPhone／Safari 待用户复验）。
- 来源：用户发现 iPhone 从其他应用返回 Safari 后，有时需要重新输入 Passkey；认证后当前默认会话又进入整页同步。用户希望省去重复认证和破坏性同步。
- 关联：[0061 ECS 单机一键部署](0061-ecs-single-node-one-command.md)、[0073 会话增量同步与无白屏缓存恢复](0073-incremental-thread-sync-and-stale-view-cache.md)、[0083 前后台与会话切换的非破坏性恢复](0083-nondestructive-foreground-resume.md)。
- 范围：Passkey Cookie 续期、真实 WebSocket 重连时保留 stale view、Safari 页面进程被回收后的本机持久显示缓存；不改变 Codex SSOT，不在 Relay 保存会话正文。

## 已确认的根因

1. Passkey 登录 Cookie 当前是固定 24 小时有效；登录后不会续期。一个已建立的 WebSocket 可以继续工作到 Cookie 过期以后。
2. Safari 在应用切换、锁屏或资源回收时可能关闭 WebSocket。下一次握手重新校验 Cookie；若 Cookie 此时已经超过 24 小时，Relay 以 policy violation 拒绝，Web 才显示“登录会话已失效”。因此应用切换是暴露条件，固定过期才是认证根因。
3. App 当前对任何非 `ready` 状态都清空 current view、页面内历史缓存、warm subscription 和当前选择；`onReady` 又再次清空。即使只是短暂断网，也会把仍可安全展示的确认内容删掉。
4. 0083 只覆盖“页面进程仍在且 socket 仍为 OPEN”的健康恢复。若 socket 真断开，subscription 已随连接销毁，协议没有断线事件 replay cursor，网络层仍必须重新订阅才能确认遗漏增量。
5. 若 Safari 直接回收并重建页面进程，现有内存缓存也会丢失。要在 reload 后立即恢复正文，必须改变 0073 的纯内存边界，增加仅位于用户浏览器本机的有限持久缓存。

## 用户行为

1. Passkey 会话采用“24 小时不活跃过期”的滑动语义：
   - 首次加载、页面回到前台和最长每 12 小时一次的存活检查，在旧 Cookie 仍有效时签发新的 24 小时 Cookie；
   - 连续使用期间不重复要求 Passkey；超过 24 小时完全未使用、显式退出、Cookie 被系统清理或 Relay session key 变化时仍必须重新认证；
   - 升级前已经过期的 Cookie 无法被恢复，发布后第一次可能仍需登录一次。
2. socket 短暂断开或重连时继续展示最后确认的当前会话、历史、草稿和滚动位置；仅在连接状态处提示离线／重连，不出现整页“正在同步”。
3. 重连后复用原选择，在后台建立新的 owner subscription。fresh snapshot 按 turn／item ID 合并到 stale view；未变化消息 DOM 不替换。
4. owner 状态重新确认前，发送、停止、审批、回答和队列 mutation 必须禁用；保留正文不等于把 stale view 当作可写 owner。
5. Safari 回收页面并 reload 时，如果 Passkey Cookie 有效：
   - 读取本机 IndexedDB 中最近选择和显示缓存；
   - 在确认 `deviceId + agentEpoch + threadId` 仍匹配后立即展示缓存正文；
   - 后台重新订阅最近 4 个 turn 并合并，不显示全页 loading；
   - 缓存不存在、过期或身份不匹配时才使用现有同步页面。
6. 浏览器到 Relay 仍只有一条多路复用 WebSocket；不增加逐会话轮询、逐会话 auth 请求或 Relay 正文缓存。

## Passkey 安全边界

1. 保持 `Secure + HttpOnly + SameSite=Strict + __Host-` Cookie，不把认证凭据写入 JavaScript storage。
2. 只有一个尚未过期且成功解密的 owner session 才能续期；续期不降低 WebAuthn 的首次认证要求。
3. `/api/auth/status` 可以为有效会话滚动续期，并继续返回 `Cache-Control: no-store`；无效会话不得获得新 Cookie。
4. 固定 24 小时有效期改为最长 24 小时不活跃窗口。显式退出继续删除 Cookie。
5. 前台续期请求失败只显示非阻塞网络状态，不主动注销仍可能有效的本地会话；WebSocket 收到明确的 1008 unauthorized 才进入登录界面。

## 本机正文缓存边界

1. 缓存只写浏览器同源 IndexedDB，不写 `localStorage`、Service Worker、Relay、Desktop Agent 日志或仓库。
2. 只保存渲染所需的已确认会话数据：`deviceId`、`agentEpoch`、`threadId`、最近选择、最近 4 个 turn 和用户已下载的旧历史页；不保存认证 Cookie、Passkey、Agent token、pending interaction 回答、permission owner 状态、待发送截图二进制或 mutation receipt。
3. 缓存上限：最近 20 个会话、单条 2 MiB、合计 25 MiB、最长 7 天；每次写入和启动时按 LRU、大小与 TTL 淘汰。超限、配额不足或 IndexedDB 不可用时退化到现有内存缓存，不影响连接。
4. 缓存 key 至少包含 `deviceId + agentEpoch + threadId`。Agent epoch 改变、显式退出或用户点击断开时清理对应正文缓存；普通 socket 断开不得清理。
5. 持久缓存永远是只读 stale display。只有 fresh owner snapshot 完成 identity 与 seq fencing 后才开放 mutation。

## 实现约束

1. 将 App 的“连接状态”与“已确认显示状态”分开：`disconnected/connecting` 只撤销 owner-ready 能力，不清空 view、history、draft 或选择。
2. `onReady` 不再无条件清空缓存；先比较 Relay／Agent 身份，再决定复用显示缓存或隔离失配数据。
3. 真重连后旧 subscription 必须视为失效并从 warm 状态表移除；不得把旧 `subscriptionId/streamId/seq` 用于 mutation 或接收新事件。
4. durable cache 的写入在 snapshot、合法 delta、历史分页完成和切换会话时异步发生，不阻塞渲染或 WebSocket 事件处理。
5. fresh snapshot 与缓存合并沿用 0073 的稳定 ID 规则；相同 turn/item 的 DOM identity 尽量保持，服务端 fresh 数据优先。
6. 页面 reload 后恢复草稿不在本规格范围；普通 socket 重连且页面进程未丢失时必须保留草稿。

## TDD 与验收

1. Red：WebAuth 测试令有效 Cookie 接近／超过原签发时间后调用 auth status，断言获得续期 Cookie；无效、篡改和已过期 Cookie不得续期。
2. Red：App 测试模拟 socket 1006 close → retry → hello.ok，断言正文、历史、草稿和滚动位置在全过程保持，mutation 在 fresh snapshot 前禁用，页面不出现全页同步。
3. Red：重连 fresh snapshot 只合并变化项；旧 subscription 迟到事件继续被丢弃。
4. Red：IndexedDB cache 测试覆盖身份隔离、20 条／2 MiB／25 MiB／7 天上限、LRU、损坏记录、配额错误和显式清理。
5. Red：App reload 测试预置有效 Passkey 状态和持久缓存，断言先显示缓存正文、再后台订阅；Agent epoch 不匹配时不得展示缓存。
6. Playwright 真实移动 Chromium 覆盖 socket 被关闭后的恢复和 page reload 恢复；断言正文稳定、无登录面板、无全页 loading、fresh owner 前发送禁用。
7. 运行 Go 全包测试、race、Web 全量测试、production build 和全部 Playwright 回归。
8. 公网 Docker 发布后验证 Cookie 续期响应、容器 health、新 Web asset 与真实重连；物理 iPhone／Safari 由用户复验应用切换和页面回收场景。

## 风险与回滚

- 风险：滑动 Cookie 让持续活跃设备长期保持登录；通过 24 小时不活跃上限、HttpOnly／Strict Cookie 和显式退出控制，不改成永久登录。
- 风险：本机 IndexedDB 含会话正文，同一浏览器 profile 的本地使用者可能读取；这是为了抵抗 Safari 页面回收而明确接受的新数据边界。
- 回滚：停止 auth status 续期、恢复断线清空 view，并删除 IndexedDB cache adapter；Relay／Codex 数据无需迁移。回滚版本首次启动应异步删除 0084 cache database。
