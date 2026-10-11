# 0064：设备切换时的只读请求超时语义

- 状态：Implemented（2026-10-11；Go／Web／race／production build／真实 Chromium 已验）
- 来源：公网 Relay／Web 已升级后，手机从一个设备切换到另一个设备，目标设备显示 Agent 在线，但会话列表返回“执行结果不确定。请先查看会话状态，不要直接重发。”
- 范围：Relay、Web 与 Desktop Agent 的只读请求 timeout budget 和错误呈现；不改变 mutation 的 `OUTCOME_UNKNOWN` 语义，不自动重发消息、审批、队列变更或其他写操作。

## 已确认事实与待验证项

1. 公网 Web 已加载包含 0063 修复的新 bundle；截图发生时，目标设备仍显示 Agent 在线。
2. 当前 Desktop Agent 日志没有截图时刻对应的断链记录；截图中的设备名称也不等于本机受管 Agent，因此不能用本机日志证明目标设备当时的内部状态。
3. `thread.list` 是只读请求，但当前 timeout budget 倒挂：Relay 默认 10 秒、Web 12 秒，而 Desktop Agent 到 Codex 原生读取允许 30 秒。目标设备冷启动 App Server 或首次加载超过 10 秒时，Relay 会先返回 `OUTCOME_UNKNOWN`，Agent 的读取仍可能继续。
4. Web 对 `thread.list` 的 `OUTCOME_UNKNOWN` 复用了 mutation 文案“不要直接重发”，语义不成立。只读列表可以安全重试；不能以写操作的重复执行风险警告用户。
5. 我不确定截图中的单次请求是否确实耗时超过 10 秒；确认这一点需要目标 Desktop Agent 与 Relay 同期日志。但 timeout budget 倒挂及错误分类本身是确定缺陷。

## 行为与不变量

1. 跨层 timeout 必须满足：Web deadline > Relay route deadline > Desktop native read deadline。默认预算为：
   - Desktop native read：30 秒；
   - Relay 只读 route：35 秒；
   - Web 只读等待：40 秒。
2. Relay 的只读长预算适用于 `thread.list`、`thread.read`、`thread.history`、`thread.history.items` 和 `thread.image`；`thread.subscribe` 沿用现有 30 秒 Relay／35 秒 Web 预算。
3. mutation 继续使用既有 45 秒 Relay／50 秒 Web 预算；已转发但回执不明时仍返回 `unknown/OUTCOME_UNKNOWN`，Web 不得自动重发。
4. Desktop Agent 能确定原生只读调用达到 deadline 时，返回 `rejected/TIMEOUT`，不得伪装成可能已执行的 mutation unknown。Relay 自己先耗尽 35 秒仍可返回 `unknown/OUTCOME_UNKNOWN`，因为它无法确认 Agent 是否取得请求结果。
5. Web 加载设备会话列表收到 `TIMEOUT` 或只读 `OUTCOME_UNKNOWN` 时，显示“加载会话超时，请重试。”，保留刷新入口；不得显示“不要直接重发”。
6. 设备 A → B 切换会递增 list generation。A 的迟到成功、失败或 timeout 不得覆盖 B 的列表、loading 或错误；B 的请求也不得写回 A。
7. 不进行无界自动重试。用户再次刷新是一个新的只读 generation；切换设备不会自动重放任何 mutation。

## 错误与边界

- 目标 Agent 在请求转发前已离线：保持 `DEVICE_OFFLINE`。
- Web 与 Relay 连接本身断开：连接恢复逻辑处理；在途 mutation 仍保持 unknown。
- 目标 Codex 原生读取在 30 秒内明确返回协议或容量错误：保持 `PROTOCOL_UNSUPPORTED`、`HISTORY_TOO_LARGE` 等原错误。
- 搜索会话当前已使用 35／40 秒预算，纳入统一只读分类后不得退化。
- timeout 延长只影响在途 route 生命周期，仍受每个 Web 最多 32 个 pending request 的有界限制。

## TDD 与验收

1. Red：Relay 测试让普通 `thread.list` 在旧 10 秒等价测试窗口后、35 秒等价窗口前返回，断言请求不被提前判为 `OUTCOME_UNKNOWN`。
2. Red：Web timeout 单测断言所有只读方法为 40 秒，且严格大于 Relay 默认 35 秒；mutation 仍为 50 秒。
3. Red：App 测试执行设备 A → B，随后让 A 返回迟到错误、B 返回 `TIMEOUT`／`OUTCOME_UNKNOWN`，断言只显示 B 的只读超时文案，且 A 不污染 B。
4. Red：Desktop Agent 将 `context.DeadlineExceeded` 映射为 `TIMEOUT`，其他不确定 mutation 仍为 `OUTCOME_UNKNOWN`。
5. 真实 Chromium 隔离 WebSocket fixture 覆盖设备切换和目标设备延迟列表响应；断言 10 秒等价边界后仍保持 loading，成功响应到达后展示 B 的列表。
6. 完成 Go 全包、`go vet`、相关 race detector、Web 全量测试、production build 与真实 Chromium 回归。物理 iPhone／Safari 由用户部署后复验。

## 实施结果

- Relay 新增独立的 35 秒只读 route budget，覆盖列表、读取、分页历史、分页 item 和图片；普通短请求、订阅与 mutation 仍保留各自预算。
- Web 对上述只读方法统一等待 40 秒，严格晚于 Relay；订阅保持 35 秒，mutation 保持 50 秒。
- Desktop Agent 将能确定的 native deadline 映射为 `rejected/TIMEOUT`；mutation 回执不明仍为 `unknown/OUTCOME_UNKNOWN`。
- 会话列表的 `TIMEOUT`／只读 `OUTCOME_UNKNOWN` 改为“加载会话超时，请重试。”，不再显示写操作的重复执行警告。
- 设备 list generation 继续隔离 A／B 响应；真实 Chromium 用虚拟时钟跨过旧 12 秒边界，证明 A 的迟到错误不会污染 B，B 的延迟成功结果可以正常呈现。
- 验收证据见 [`docs/testing/2026-10-11-0064-device-switch-read-timeout.md`](../testing/2026-10-11-0064-device-switch-read-timeout.md)。
