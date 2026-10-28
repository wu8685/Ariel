# 0126：执行中的异步问题与回答卡片

- 状态：Approved；用户于 2026-10-28 明确确认实现并发布。
- 日期：2026-10-28。
- Issue：#56。
- 未完成的异步写入 compatibility gate：#63；本轮只读 fallback 发布不代表该项完成。
- 关联：0005／0098／0100／0109；补充异步问题，不替代既有阻塞式问题与审批。

## 1. 根因与证据

用户截图为可以继续执行任务的异步问题，不能等同于 0098 的 `item/tool/requestUserInput`。本机 Desktop 26.930.51102、内置 Codex 0.160.0 只读检查确认：

1. 当前会话的原生历史中有 7 个 `agentMessage` 包含 `questions`；截图对应的问题存在，问题对象字段为 `title`、`options`，选项为字符串。它们不是 `requests` 中的 server request；观察时 `requests` 为空。
2. 对每个原生问题 item 单独调用当前 `NormalizeStored`，7 个均丢失 `questions`，排除了最近两 turn 窗口造成的观察歧义。`normalizeTurn` 的 `agentMessage` 分支只保留 text，Web 没有收到构建卡片需要的数据。
3. 本机 App 从消息 item 与题目下标派生稳定问题身份，通过当前 turn 的 `steerTurn` 发送原生结构化回答信封；历史答案来自已接受的 steering／user message。不是 `thread-follower-submit-user-input`。
4. 原有测试只覆盖阻塞式 server request，因此不能发现本次缺失。上述检查未发送、回答、停止或重启业务会话，未保存或上传问题正文、真实 ID 和快照。

**证据边界：**读取结构和源码路径已确认；外部客户端提交异步答案的真实写入、回显与竞争行为仍为 unverified，必须通过隔离 fixture 后才启用。

## 2. 用户体验与范围

- 在问题对应的会话位置展示完整题目、原生顺序的选项和自由输入；不依赖用户展开工具详情，不再只展示邻近正文。
- 异步问题不阻塞 Codex 继续执行，也不禁用普通 composer、排队、停止等原有能力。不能直接塞进具有阻塞语义的 `pendingInteractions`。
- 每题可独立选项或输入答案；选项不自动提交，也不因为预选就代表用户确认。多个题目分别保留未提交状态。
- 明确点击“发送回答”才提交；提交中禁用该题，其他题目、阅读位置和主草稿保持不变。题目/身份变化不套用旧答案。
- “跳过”仅收起本浏览器的该题，不发送空答案、不取消 turn、不宣称已回答；允许再次展开。本阶段不承诺跨浏览器同步跳过。
- 原生已确认的答案显示为只读“已回答”；原 turn 完成、失败或中断后仍可阅读题目与已有答案，未回答项标为“已结束，未回答”，不能偷偷创建新 turn 继续回答。
- stale、离线、owner 未确认或有阻塞式请求时仍显示题目，但禁用异步提交并说明原因。不把过期卡片当作当前有效请求。
- 三种文件入口、原文／阅读／Diff、公式、Tab、引用、普通发送及 0098 卡片均不改变；Chilling 不参与此能力。

## 3. 数据与状态归属

### 3.1 Adapter 读取

输入只来自版本化 Adapter 的当前 owner snapshot／follower update，或明确分页请求取得的原生历史：

```text
agentMessage: { id, text, questions: [{ title, options?: string[] }] }
```

- 绑定 exact thread、turn、source item 和 question index；public question ID 为 Adapter 生成的 opaque identity，指纹同时覆盖题目和选项。
- 不从普通正文、代码块、工具参数、alt 或包含“请确认”的语句推断出可回答请求；缺少原生来源时不合成可操作卡片。
- 规范化题目、选项和已接受的回答，不转发原始工具内容；未知、敏感或不完整结构局部提示回 Desktop 处理。
- 题目与回答属于 Codex 历史；Ariel 不建立第二套问题数据库。Web 未提交答案仅存页面内存，不写日志、durable history、localStorage 或文件 checkpoint。

### 3.2 增量与有界缓存

- 复用既有每个 hot 会话唯一的 Agent follower、Agent→Relay 持久连接、Web→Relay 多路复用连接；不按题目/浏览器增开 follower、socket 或轮询。
- Relay 最近两 turn 缓存新增轻量问题 sidecar，绑定相同 agentEpoch／cacheStreamId／seq，与正文原子发布及 rotate；只保存在进程内存，计入实际 cache 字节预算。
- 新问题、问题失效和答案确认作为交互状态及时发布；普通正文仍保持 5 秒合并，不为本功能把全部文本刷新频率调回 1 秒。
- 读取历史只对本次最多三 turn 的分页结果附带问题；不因展示或回答问题扫描/重新读取全部历史。
- 单消息最多 16 题、每题最多 32 选项；题目 2048 字符、单选项 512 字符；最近窗口 sidecar 最多 64 题、64 KiB。超限必须局部明确提示，不能静默截断后声称题目完整，更不能导致整会话断线。

## 4. Wire 扩展与新旧端

采用显式协商的独立问题 sidecar，不把未经协商的新属性直接加进所有客户端的严格 v1 `item`／`interaction`：

1. 新 Relay 在既有 `device.list` 的宽松 response.data 声明 `asyncQuestionsVersion: 1`；旧 Relay 无此字段，新 Web 不发送新方法或参数。
2. 新 Agent 在既有 `thread.cache.touch` 成功 response.data 声明本连接的问题扩展能力；新 Relay 按 exact 当前 Agent 与请求关联确认。旧 Relay 忽略可选 response.data，不会收到未知 hello capability。
3. Relay 确认后才在后续 touch 带上扩展 opt-in；Agent 因此知道 Relay 接受新的 sidecar，立即补发一次完整问题状态，不等待正文有新变化。连接替换重置协商，不能沿用退休 Agent 的能力回执。
4. 新 Web 仅在 Relay 已声明支持后对 cache watch 发送问题 opt-in。Relay 向已 opt-in 的 Web 发送有界 sidecar；旧 Web 收到的 snapshot／update 保持原 v1 形状。历史 response.data 可附带同版本问题数据，但不能以扩展字段破坏旧 schema。
5. 新增公共回答请求 `thread.questions.respond`，仅在当前 Agent／Relay／Web 均确认能力后调用。请求绑定 thread、agentEpoch、turn、opaque question ID、expected fingerprint、operation ID 和非空 answer。Relay/Web 不接触 Desktop 私有方法名。
6. 新旧 Agent／Relay／Web 的组合必须测试；不支持时保留会话正文和普通交互、局部提示，禁止协议关闭、登录循环和自动重放。旧端已丢失的题目不能伪造恢复成功。

## 5. 提交语义与安全门禁

- 沿用显式交互写开关；新异步写入另设可关闭的 capability，只有当前版本隔离验证通过才宣称 supported。展示能力不等同于写入能力。
- 提交前在 hot controller 内核对最新 owner、cwd、thread、epoch、仍为 inProgress 的 exact turn、题目身份/指纹与未回答状态；有阻塞式 request 时先处理该 request，不能绕过现有审批。
- Adapter 生成原生 `send_user_message_question_reply` 信封，用当前 owner 的既有 follower steer 路径提交；Web 不能上传自定义原生信封或任意 request ID。
- 答案最多 8192 字符，整个请求受既有 64 KiB 文本预算限制；不携带截图、文件引用或其他普通 composer 附件，不清空主草稿、不制造 queue 项，不发送到新会话。
- 仅 native transport 回执不算成功。核验 exact turn 内已接受的 user／steering message，要求问题身份、题目、答案和本次 client identity 精确匹配；有拒绝、未知或仅 optimistic 记录时不能标已回答。
- 提交中重复点击／相同 operation ID 不重复发送；超时或断线显示“结果不确定”，只核对状态，不自动重试答案。重连重新从当前 owner 建立有效性，不能用 Relay cache 作为 mutation 授权。
- 同一题已被 Desktop／另一 Web 回答时拒绝旧提交并显示原生答案；跨 native 客户端竞态不承诺 question 级 exactly-once。多个答案竞争时以原生最终已接受记录呈现，不把第一个 ok 当作唯一胜者。
- 本阶段不支持修改已回答的答案，也不自动延长原 turn／另起新 turn；若需要，与用户另行确认。

## 6. TDD 与 compatibility gate

用户确认后，严格先 red，再最小实现，再 refactor：

| 层 | 必须覆盖 |
| --- | --- |
| 复现 | 合成 agentMessage.questions、空 requests，经当前转换和真实浏览器证明没有卡片；不使用私人题目作 fixture |
| 读取 | 选项/自由输入/多题、精确身份、消息与题目同时存在、未知/敏感/超限、canonical history/公开分页、两 turn rotate |
| Wire | 新旧三端协商矩阵、连接替换、未 opt-in 不发送新字段/方法、旧 Web 不断线、非法 sidecar 局部拒绝、预算实际计量 |
| Web | 320/390/1280px、滚动与卡片几何、选项/自由回答/显式发送/本地跳过恢复/已回答/已结束、普通草稿/queue/停止不被卡住 |
| 提交 | 过期 turn、题目变更、已在别端回答、连点、重复 operation ID、断线/超时 unknown、accepted 与 optimistic echo 区分 |
| 性能 | 一会话一 follower、单 WebSocket、多客户端不增加 native 订阅、仅当前缓存窗口/指定历史页，正文 5 秒 cadence 不变 |

隔离 native fixture 必须真实产生一次异步问题；从外部提交建议项和自由回答，确认原 thread、原 turn、原生 accepted echo，且模型后续确实收到答案。另测 Desktop 先回答及中断过期。若当前版本无法在隔离 fixture 触发工具或不能证明写入语义，则只交付已验证的只读卡片并明确保留写入门禁，不能把 mock 全绿算作真机验证。

最终运行 Go 全量／race／vet、Web UT／build／完整 Chromium，并检查新截图；物理 iPhone/Safari 留给用户复验。用户已在本轮明确授权 push 与本机 Agent／公网 Relay/Web 双端发布，不修改或重启 Chilling。

## 7. 依据与未验项

- [官方 App Server 输入请求](https://learn.chatgpt.com/docs/app-server#toolrequestuserinput)描述 server request 与 resolved 通知；不能据此外推 Desktop 异步问题共用该接口。
- [官方异步工具说明](https://developers.openai.com/api/docs/guides/async-tool-calling#add-a-tool-to-ask-the-user-for-input)说明可边执行边等用户，`request_user_input_async` 不是 Responses 内建工具。文档不能替代 Desktop 私有 Adapter 实测。
- Desktop 消息 questions、稳定题目身份、结构化回答信封、steer 及原生历史匹配来自本机只读源码核对，不是公开稳定 IPC 保证；真实提交仍 unverified。

## 8. 本轮实现与发布边界

2026-10-28 用户已明确确认本规格并授权发布。读取／展示按本规格实现；第 5 节异步提交和原生竞争验证尚未完成，不能将整份规格标记为 Implemented。

- 当前 Desktop 26.930.51102／bundled Codex 0.160.0 的独立 `async-question` fixture 只有 read-only／never 权限，禁止其他工具；实际返回 `ASYNC_TOOL_UNAVAILABLE`，独立持久历史确认没有产生原生异步问题，未使用普通正文或同步问题替代。
- 依照第 6 节 fallback 发布只读卡片：展示题目、选项、原生 accepted 回答、本地跳过与恢复、终态；Web 和 Relay 的异步提交门禁均关闭。未验证 external steer、模型收到回答、Desktop 先回答及中断竞争，不宣称完整回答能力已完成。
- Agent 初次未协商的 snapshot 保持原 v1；之后用精确 request／当前 Agent transport 的成功 touch receipt 协商，补发同一 follower 的完整视图。问题扩展补发占用一个单调递增的公开 seq，不把 cache seq 视为 native seq。
- 只请求已显示的历史页；仅包含部分 items 时标为回答状态未确认，不声称“未回答”，也不能用迟到的部分历史覆盖既有精确已回答状态。
- 复查修正 follower 刚记录超限错误、load receipt 先返回关闭错误时的原因丢失；不增加重试、轮询或修改权限。文件 quota 用例适应合法的独立主会话恢复，不改变文件权限或跳过无 Tab 断言。
- 发布前当前超长会话的生产 cold attach 命中 `HISTORY_TOO_LARGE`（#64），不放宽容量限制。为保留现有热连接与 cache，本轮先 push PR，暂不打版本 tag／重启公网和本机；不能用隔离模拟的通过掩盖真实 smoke 未通过。

测试过程与 native 限制见 [0126 验收记录](../testing/2026-10-28-0126-async-questions.md)。
