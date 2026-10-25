# 0101：手动加载旧历史每次最多三个 Turn

- 状态：Implemented（2026-10-25；Web 单元测试、production build 与移动 Chromium 全量回归通过）
- 日期：2026-10-25
- 来源：会话顶部的“加载更早消息”当前单页请求 10 个 turn；大会话中一次加载和渲染的数据仍然偏大。
- 关联：[0013 大会话最近十回合与 64 MiB 有界同步](0013-paged-history-and-large-owner.md)、[0062 Web 端有界会话历史缓存](0062-bounded-web-history-cache.md)、[0072 新会话首屏最近四回合](0072-four-turn-initial-history-window.md)、[0100 Agent 持续跟随与 Relay 双 Turn Hot Cache](0100-agent-fed-relay-two-turn-hot-cache.md)。
- 取代范围：本规格获准后，仅取代 0072 中“用户显式加载旧页仍最多请求 10 个 turn”的产品行为；`thread.history` 协议的通用 `limit` 上限仍为 10。

## 1. 当前行为与问题

1. Web 点击“加载更早消息”时调用 `thread.history`，当前默认参数为 `limit: 10`。
2. Desktop Agent／Codex App Server 按 cursor 分页，因此当前实现不是一次加载全部历史；但单次有效旧页最多可向页面加入 10 个 turn。
3. Web 为跨过 recent window 的重叠页，允许在一次点击内继续读取后续 cursor；当前每个请求仍以 10 为上限。
4. 单个超大 turn 内的 `thread.history.items` 是 item 子分页，不等同于 turn 历史分页，本规格不调整其 `limit: 100`。

## 2. 目标行为

1. 用户每次显式点击“加载更早消息”，Web 发出的每个 `thread.history` 请求使用 `limit: 3`。
2. 一次点击最多向当前页面新增 3 个不重复的、更早 turn；不能因为跨过 recent window 重叠页而累计加入超过 3 个 turn。
3. 若第一页只包含当前 recent window 中已经可见的 turn，Web 可以沿 `nextCursor` 继续读取，直到：
   - 找到并加入 1～3 个更早 turn；
   - `nextCursor` 为空，确认历史已耗尽；
   - 达到既有 20 页防护上限；
   - cursor 不前进或响应格式非法。
4. 加载期间保留当前消息、阅读位置和输入草稿；不得切换成覆盖会话正文的全屏 loading。
5. 新加入的旧 turn 继续进入当前页面的有界历史缓存，并服从既有 50-turn／32 MiB 页面内存上限。
6. 最近两个 turn 的首屏／Relay hot cache、后台增量更新和跨会话恢复行为保持不变。

## 3. 协议与兼容性边界

1. `thread.history` 协议继续接受 `limit` 1～10，以兼容旧 Web、诊断工具和内部恢复流程；不把协议级 maximum 改为 3。
2. Relay 继续只转发该显式历史读取，不拼接、不持久化旧正文，也不把旧页写入 0100 的双 turn hot cache。
3. Desktop Agent 必须尊重请求中的 `limit`。Web 不依赖服务端返回超过请求数量的数据来推进 cursor。
4. 若兼容性异常导致单页返回超过请求的 3 个 turn，Web 不得静默丢弃中间 turn 后继续 cursor；应保留当前已展示内容、停止本次加载并提示历史页格式异常。
5. turn 数量按完整 native turn 计算，不按 message、item 或文本块计算；不得为满足三个 turn 而截断普通 turn。

## 4. 错误处理

- Agent 离线、超时或返回非 `accepted` 时，保留当前页面和 cursor，用户可以重试。
- cursor 没有前进且未 exhausted 时，停止加载并提示“历史页没有继续前进”。
- 连续重叠页超过既有 20 页保护时，停止并提示历史变化过快，不形成无界请求。
- 页面切换到其他 device／thread 或 selection epoch 变化后，迟到响应不得写入新页面。

## 5. TDD 与验收

1. Red：更新 Web 行为测试，点击“加载更早消息”必须发送 `thread.history { limit: 3 }`，旧的 `limit: 10` 断言先失败。
2. Red：新增多页重叠测试；第一页只有 recent overlap，第二页返回 3 个旧 turn，一次点击恰好加入 3 个且不再请求第三页。
3. Red：新增防御测试；服务端对 `limit: 3` 返回超过 3 个 turn 时不跳过未展示历史，并给出可重试错误。
4. Green：以独立的 `manualHistoryTurnPageLimit = 3` 实现，不复用 recent window 常量，也不收窄协议级 1～10 范围。
5. 回归：Web 全量测试与 production build 通过；在隔离 WebSocket fixture 的真实移动 Chromium 中点击按钮，断言请求 limit、最多新增三个 turn、阅读锚点和 composer 均保持。
6. 公网部署后由用户在物理 iPhone／Safari 上复验大会话的加载耗时与滚动稳定性。
