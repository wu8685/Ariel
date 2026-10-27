# 侧边聊天上下文：2026-10-27 只读调查

状态：官方契约与本机静态证据已核对；Ariel 端真实 fork／发送／恢复 **unverified**。本报告不批准运行时实现。

## 1. 结论

1. 用户要求的是承接来源会话上下文的 side chat，不是空白 `thread/start`。Codex 的 UI 产品语义区分轻量 side chat 与新的主会话 fork；本机 side chat 的底层确实复用 `thread/fork`，再加 side-conversation 边界。
2. 官方 App Server 有 `thread/fork`，本机内置 CLI 的非 experimental schema 也包含它。它建立新 thread，并继承来源历史；没有证据表明之后两个 thread 的新增内容自动双向共享。
3. 已检查本机 Desktop 的 follower 请求注册表和方法版本表，未发现外部 follower 创建 fork／side chat 的对应入口。App 内部调用 App Server 不代表 Ariel 使用的 Unix follower IPC 已暴露同一能力。
4. 推荐验证 Ariel Desktop Agent 经现有 App Server 适配层创建原生上下文分支，而不是 Web／Relay 下载全文后拼 prompt。只控制新分支，不抢占／resume 主会话 owner。该替代接入路径尚需隔离 fixture 验证，不能标记 supported。

## 2. 证据范围

| 来源 | 核对结果 |
| --- | --- |
| 本机 ChatGPT Desktop | `26.930.51102`；内置 Codex CLI `0.160.0` |
| 本机 ASAR SHA-256 | `a159b8f5b78ed1ba89fc70d5c8448d822a46c4fc2a4a9f18ec348f3cc2f6b8c9` |
| 内置 CLI 离线 schema 生成 | `ThreadForkParams` 包含 `threadId`、`lastTurnId`、`ephemeral`、`excludeTurns`、`developerInstructions` 与模型／权限覆盖参数；只生成 schema，未启动会话服务 |
| 官方 App Server 文档 | 复制存储历史到新 thread；允许指定截止 turn、内存分支及不返回完整 turns |
| Desktop renderer 静态调用 | side chat 调用 fork helper，传入 `ephemeral: true`、`sideConversation: true`，不把继承历史显示为新聊天正文 |
| Desktop main 静态调用 | 内部 manager 发送 `thread/fork`，之后通过 `thread/inject_items` 加入参考上下文边界；新分支有独立状态与 owner |
| 外部 follower 注册／版本表 | 有发现 owner、发送、停止、设置、审批、回答与队列等方法；检查范围未出现 fork／side-chat 创建方法 |
| Ariel 当前代码 | bootstrap 走 `thread/start`，尚无 fork 适配、上下文锚点或 side-chat 生命周期能力 |

检查的 bundle 为 `.vite/build/bootstrap-CXJAEjVI.js`、`.vite/build/src-C1dW0Du8.js` 和 `webview/assets/app-initial-f9b16fbf8fc7.js`。这里只记录方法与行为摘要，不收录应用原始 bundle、真实 thread ID、会话内容或凭据。

## 3. 共享的语义与边界

- **创建时继承**：新分支从来源历史建立上下文，两个 thread 随后各自发展；没有证据支持实时双向共享或自动合并回主会话。
- **不继续旧任务**：本机 side chat 同时使用 developer instructions 和注入的历史边界，将继承内容限定为参考。旧任务、工具调用、审批或计划不能因 fork 被自动继续。默认轻量只读探索；写操作必须由侧边聊天里的用户新请求明确授权。
- **模型上下文不等于 UI 正文**：不显示继承历史并不等于模型没有该上下文；Relay 仍只缓存最近两个 turn 的 Web 展示投影。
- **来源正在处理**：官方拒绝指向 in-progress turn 的 `lastTurnId`；省略截止点时对部分 turn 有 interruption-marker 规则。不能承诺侧边聊天已经包含屏幕上所有尚未存储的 streaming 内容。
- **生命周期不同**：原生 side chat 是 ephemeral；本机代码关闭时可能中断／释放分支，durable-host 恢复也有客户端重建逻辑。不能把它当作持久 thread，或假定收起、关 Tab、Agent 重启后必然可恢复。
- **文件引用另有契约**：父历史被继承不等于父会话的精确文件 selection 可以改 threadId 转交。现有文件内容、SHA、范围和目标会话授权仍需新的来源复验契约。

## 4. 性能判断

`excludeTurns: true` 可以不把完整历史放进 fork 响应。父上下文复制由 Codex 在设备本地完成，不需要经 Relay／Web 转发完整主会话。它不保证 Codex 内部不读磁盘、不复制历史或没有创建开销；需要大历史 fixture 测时与测量 wire bytes。

Web 打开任意数量轻量 Tab，不应立即 fork 所有分支。建议在首次显式提交时才创建，并展示来源与实际截止锚点；如果产品要求在开 Tab 当刻冻结上下文，则必须另外确认创建时机，不能悄悄改变语义。

## 5. 实现前的隔离 compatibility gate

以下均未执行；必须先批准 spec，再按 TDD 在无真实用户数据的 fixture 上验证：

1. 来源 fixture 有两个带唯一标记的已完成 turn；新分支能答出既有事实，且不会继续参考历史中的旧任务。
2. fork 前后新增标记，证明上下文截止点、主会话继续运行不被中断、两个 thread 不自动双向注入。
3. 源会话 streaming、未落盘、找不到锚点或 ephemeral 来源：无法证明完整性时明确 waiting／unsupported，不静默退为空白聊天或截断的两个 turn。
4. `excludeTurns` 确实避免全文响应；测大历史耗时与 wire bytes。只对可见会话建立现有共享 read／watch，不引入每 Tab socket／follower／轮询。
5. 验证新分支独立 owner、真实 turn／完成事件、交互请求和重连；响应超时／结果 unknown 不自动再 fork 或重发。
6. 明确 ephemeral 与持久分支选择，验证收起／切 Tab 不停止，关闭与 Agent 重启的行为；丢失时保留 Web 草稿并明确提示，不伪造可恢复。
7. 文件选区提问只在目标分支身份确认后，按批准的新契约重新验证来源与目标；拒绝改写旧引用的 threadId。

若上述接入不可用，应报告当前版本不支持，再讨论显式的摘要／选区上下文降级。不能把手工拼最近两轮称为 Codex 原生 side chat。

## 6. 外部来源

- [官方 side chat 使用说明](https://developers.openai.com/blog/mastering-codex-remote-for-engineering)：side chat 围绕当前工作提问，与创建新主会话的产品动作不同。
- [官方 App Server 文档](https://learn.chatgpt.com/docs/app-server)：`thread/fork`、截止 turn、ephemeral 与 `excludeTurns`。
- [OpenAI Codex 固定提交的 ThreadForkParams](https://github.com/openai/codex/blob/6fe77fc12346591b8037a8704640e40196b19044/codex-rs/app-server-protocol/schema/typescript/v2/ThreadForkParams.ts)：与本机生成 schema 交叉核对；不能因此宣称 Desktop 外部 IPC 暴露此接口。
