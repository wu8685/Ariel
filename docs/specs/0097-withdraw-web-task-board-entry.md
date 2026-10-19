# 0097：撤回 Web 任务看板入口

- 状态：Implemented（2026-10-19 已部署公网；Web 237 项、Go 全仓、production build、桌面／手机真实 Chromium 专项已验；物理 iPhone／Safari 待用户复验）
- 影响范围：Ariel Web、Relay 的 Web HTML 配置注入与对应浏览器测试
- 不影响范围：Ariel Relay／Desktop Agent 的 task protocol 与后台路由、chilling 的任务领域／MySQL／scheduler，以及既有会话和文件上下文能力
- 取代关系：本规格获批后，取代 0091 中“Web 看板、导航与 task client”相关要求；0091 的后台 task gateway、跨主机路由和 chilling SSOT 边界继续有效

## 1. 目标

将 Ariel Web 恢复为引入任务看板之前的会话界面：用户无论是否启用后端 task flag，都看不到任务看板入口、模式切换或看板页面，Web 也不主动发起 task 查询与订阅。

这是一项 **Web UI 撤回**，不是删除后端任务能力。已开发的 Relay／Desktop Agent／chilling 能力保留为默认关闭的实验能力，后续若重新引入 Web 看板，必须另写并确认新规格。

## 2. 输入与输出

### 输入

- Relay 以默认配置启动。
- Relay 以 `--enable-tasks` 或等价环境变量启动。
- 浏览器使用桌面或手机 viewport 打开 Ariel Web。

### 输出

三种情况下均只呈现原有会话界面：

1. 顶部不出现「会话／任务看板」模式切换。
2. 侧栏不出现「进入任务看板」入口。
3. 页面不挂载 `TaskBoard`，不加载看板专用 UI、样式或轮询逻辑。
4. 浏览器不发送 `task.*` 请求或 task subscription。
5. 会话列表、会话正文、输入框、文件上下文和连接状态保持原行为。

## 3. 实现边界

### 3.1 Web 删除项

- 从 `App` 删除 task workspace mode、看板导航、侧栏入口、`TaskBoard` 挂载及其状态。
- 删除只为看板服务的 Web client 调用、组件、样式、fixture、测试与截图基线。
- 删除 `index.html` 中 `ariel-enable-tasks` runtime meta。
- production Web bundle 不应包含可达的看板入口或 task polling 代码。

### 3.2 Relay 保留项

- Relay 不再向 Web HTML 注入 task enablement 值。
- `--enable-tasks` 可继续控制后台 task gateway／route；它不能改变 Web UI。
- task protocol 与后台路由不因本规格删除，避免影响 chilling 和无浏览器执行链路的后续验证。

### 3.3 不变量

- Codex 仍是 native 会话和执行状态的 SSOT；chilling 仍是任务领域的 SSOT。
- Web 不新增任务缓存、数据库、离线队列或第二套任务状态机。
- 撤回看板不能破坏会话草稿、会话缓存、后台同步、unread、附件和文件引用。
- 不通过 CSS 隐藏残留交互；入口和挂载逻辑必须从可执行 Web 路径移除。

## 4. 错误与兼容性

- 老部署仍设置 task flag 时，Web 应忽略该 UI 信号并正常进入会话界面。
- 后台 task 服务不可用时，Web 不应显示 task 错误，因为它不再访问 task 服务。
- 若残留代码仍尝试发送 `task.*` 请求，测试必须失败，不能静默吞掉错误。
- 已登录 session、Passkey、WebSocket 多路复用和当前会话恢复语义保持不变。

## 5. TDD 顺序

### Red

先补测试并确认在当前实现上失败：

1. 即使 task flag 为 true，桌面和手机界面也不存在看板模式、侧栏入口和 `TaskBoard`。
2. 页面启动、设备切换、会话切换和前后台恢复过程中均不发送 `task.*` WebSocket 请求。
3. production HTML 不包含 `ariel-enable-tasks` meta 或运行期占位符。
4. 会话模式关键 DOM 与引入看板前的契约一致。

### Green

删除最小范围的 Web 集成与 HTML 注入，使上述测试通过。

### Refactor

清理不可达的看板组件、样式、fixture 与测试辅助代码；不顺手改动后台 task protocol。

## 6. 验收标准

1. Web 单元／集成测试全绿，且新增断言能证明 task flag 无法暴露看板 UI。
2. Web production build 成功。
3. 使用隔离 WebSocket fixture 在桌面和固定手机 viewport 完成真实 Chromium 回归。
4. 手机截图中只存在会话界面，侧栏和主内容区均无看板入口。
5. 会话选择、历史恢复、输入草稿、发送禁用态和文件上下文至少各完成一次非破坏性 smoke check。
6. 物理 iPhone／Safari 未在本机完成时，明确保留给用户复验，不以 Chromium 模拟冒充。

## 7. 实施与验证记录

- Red：task flag=true 时仍能看到「任务看板」按钮；Relay 仍将 task UI meta 注入 HTML。两项测试均先失败。
- Green：Web 恢复单一会话 workspace；移除看板入口、组件挂载、task Web client 方法和 HTML task meta；Relay 的后台 task gateway flag 保留，但不再影响页面。
- Web：30 个 Vitest 文件、237 项全部通过；production build 通过。
- Go：`go test ./... -count=1` 全部通过。
- Chromium：新增桌面 1440×900 与手机 390×844 专项用例和截图，均验证无看板入口、无 `task.*` 请求；完整 Playwright 为 37/40，通过项覆盖会话草稿、缓存、文件引用和恢复。
- 完整 Playwright 的 3 项失败与本规格无关：既有品牌指南截图漂移、Passkey 截图 21 像素漂移，以及登录 Logo 测试仍期待旧文件名而实现已使用 `-512.png` 优化资产。未据此更新无关基线。
