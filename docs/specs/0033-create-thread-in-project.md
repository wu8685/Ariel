# 0033：在指定项目中新建 Codex 会话

- 状态：Implemented（自动测试与真实 Codex 空 thread 创建／独立读回／清理已验；物理手机待用户复验）。
- 前置：0032 已建立以会话 `cwd` 为项目身份的左侧分组。
- 官方依据：Codex App Server 的稳定 `thread/start` 接口接受 `cwd` 并创建新 conversation；新 thread 与后续 `turn/start` 是两个独立步骤。

## 用户可见行为

1. 支持该能力的 Desktop Agent 在左侧“会话”标题旁显示新增按钮。
2. 点击后打开“新建会话”对话框：
   - 可从当前已加载的项目中选择，选项显示项目名和完整路径；
   - 目录路径仍可编辑，以便使用尚无历史会话的绝对目录；
   - 默认选择当前会话所属项目，否则选择最近项目。
3. 用户明确点击“创建”后才提交。成功后关闭对话框，把新会话放入对应项目，自动选择并加载它，输入框获得焦点；不会自动发送首条消息。
4. 新会话尚无标题时显示“未命名会话”；第一次 turn 后继续使用 Codex 原生标题更新。
5. 创建失败或结果未知时保留对话框和目录输入，不自动重试；用户可以核对 Desktop 后再决定是否重试。

## Ariel 协议

- Agent capability 新增可选布尔值 `threadCreate`。
- 新增 `thread.create` 请求：参数仅含 `cwd`，最大 4096 字符。
- 成功响应返回规范化 `thread`；Web 必须确认 thread ID 和 cwd 后才能自动选择。
- Relay、Web 和 Agent 都把 `thread.create` 视为 mutation，使用有界长超时；超时返回 `unknown/OUTCOME_UNKNOWN`。

## 创建与 owner 边界

1. Desktop Agent 先校验路径为本机存在的绝对目录，并解析符号链接得到规范路径。文件、相对路径、空路径、NUL 和超长路径均拒绝。
2. 每次创建启动一个一次性 App Server，只调用稳定 `thread/start`，参数为规范 `cwd`、`ephemeral:false` 和 Ariel `serviceName`；不调用 `turn/start`。
3. 收到并验证持久化 thread ID／cwd 后立即关闭该 App Server。创建 mutation 不自动重试；连接在回执确认前断开时返回 `OUTCOME_UNKNOWN`。
4. Web 随后沿既有 `thread.subscribe` 链路打开 `codex://threads/<id>`，由 Codex Desktop 成为真实 owner。后续发送、停止、审批、Steer 仍只发给 Desktop owner。
5. 读取用 RestartingRPC 的 allowlist 不增加 `thread/start`，避免后台读取子进程意外获得创建权限。

## 安全与容量边界

- 目录由已通过 PIN／session 的用户显式选择；Ariel 不扫描文件系统，也不向 Web 返回目录内容。
- “已有项目”只来自当前 Web 已加载的 Codex thread `cwd`；手工路径只在 Agent 侧校验，不在 Relay 解释。
- 每个 Web 连接继续受 32 个 pending request 上限保护；每次创建只产生一个 thread。
- 创建成功后若 Desktop owner 无法加载，thread 仍保留在 Codex 历史中，Web 显式报告加载失败，不删除或重建。

## TDD 与验收

1. App Server 单元测试：精确验证 `thread/start` 参数、结果身份、关闭一次、明确拒绝与未知结果；RestartingRPC 继续拒绝该 method。
2. Service／Agent／协议测试：路径校验与 canonicalization、能力位、`thread.create` 路由、规范化响应及 timeout 分类。
3. Web 测试：能力控制入口、已有项目选择、手工路径、成功后插入并自动订阅、失败保留输入、防过期响应。
4. 隔离真实 fixture：在临时目录创建空 thread，关闭创建进程后由独立 App Server 读回同一 ID／cwd，最后只删除该测试 thread。
5. Go race、`go vet`、Web 全量测试、production build 和本地 LAN 重启全部通过；物理手机的目录选择与键盘体验待用户复验。

## 非目标

- 不在创建时发送 prompt、选择 model、权限 profile 或 Git branch。
- 不提供服务器文件浏览器，不列出目录内容。
- 不创建 Ariel 自有会话记录，不把空 thread 伪装成已运行 turn。

## 官方依据

- [Codex App Server：Start a thread](https://learn.chatgpt.com/docs/app-server)
