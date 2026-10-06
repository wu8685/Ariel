# 0027：二维码一次性配对登录

- 状态：Implemented（用户于 2026-10-06 明确批准；自动测试、production build、本机部署与真实 Relay 配对链路已验证；物理手机相机扫码待用户点验）。
- 背景：当前新手机首次访问 Ariel 时，需要手工输入 6 位 Web PIN。用户希望在一台已经登录的浏览器里展示二维码，手机扫码并明确确认后，直接进入已登录状态。
- 依赖：沿用 0003 的 Web／Agent 凭据隔离与 0010 的 Relay Web Session；不改变 Agent token、Codex Desktop IPC 或会话数据边界。

## 核心决策

二维码不得包含 6 位 Web PIN，也不得包含可复用的 `s_` Web Session。Relay 为已登录的 Web 连接签发一个高熵、短时、一次性的 `p_` 配对凭据；手机确认后以它换取标准 Web Session。用户看到的结果等价于“自动填入连接码并登录”，但连接码本身不会进入二维码、URL、浏览器历史或日志。

## 用户可见行为

1. 已登录浏览器的界面提供“手机扫码登录”入口；未登录页面不能创建二维码。
2. 点击后打开模态框，展示只由本机 Web 代码生成的二维码、两分钟倒计时、“重新生成”和“关闭”。二维码内容指向当前 Ariel Origin。
3. 手机系统相机扫码后，在浏览器打开 Ariel 的确认页。确认页显示要登录的 Ariel 地址和“确认在此手机登录”“取消”两个动作；扫码本身不得自动建立登录会话。
4. 用户点击确认后，手机使用一次性配对凭据连接 Relay。Relay 成功兑换后签发普通 `s_` Web Session，Web 将其写入当前标签页的 `sessionStorage`，清理配对状态，并直接进入正常会话界面。
5. 原浏览器收到“手机已登录”结果并关闭或完成二维码状态；同一二维码再次扫描或确认必须失败。
6. 配对过期、被取消、已使用或 Relay 重启时，手机显示明确错误，并提供返回 6 位连接码登录的入口。原有 PIN 登录始终保留。

## URL 与页面状态

- 二维码 URL 使用当前 `location.origin`，配对凭据只放在 URL fragment 中，例如 `http://host/#pair=<credential>`；fragment 不随初始 HTTP 请求发送到 Relay。
- 手机页面首次读取 fragment 后，必须立即用 `history.replaceState` 清除地址栏中的配对内容，再渲染确认页；原始凭据只保留在该页面内存中，刷新后不可恢复。
- 不允许把 PIN、Agent token、Web Session 或配对凭据写入 query、path、localStorage、日志、错误文本、埋点或剪贴板。二维码 SVG／Canvas 不从外部服务加载或上传数据。

## Relay 协议与状态

1. 新增 Relay 本地请求 `auth.pair.create`：仅已鉴权 Web 连接可调用；返回一次性 `p_` 凭据和绝对过期时间。Web 自己拼装当前 Origin 的 fragment URL。
2. 新增 `auth.pair.cancel`：创建者关闭、刷新或重新生成二维码时撤销当前邀请。创建者断开连接时，Relay 也撤销其未使用邀请。
3. 手机确认后仍走 WebSocket `hello(role=web)`，但 token 为 `p_` 配对凭据。Relay 原子消费邀请并返回现有 `hello.ok.sessionToken`；不增加独立 HTTP 登录接口。
4. 配对成功后，Relay 向创建者发送 `auth.pair.consumed` 事件。事件不携带手机身份、PIN、配对凭据或新 Session。
5. Relay 只在内存保存配对凭据的 SHA-256 摘要、创建连接、创建时间和过期时间，不保存原文；Relay 重启会自然撤销全部邀请。

## 安全与容量边界

- 配对凭据使用至少 32 字节密码学随机数，格式固定为 `p_` 加 64 位小写十六进制；有效期固定为两分钟。
- 每个已登录 Web 连接最多一个待使用邀请；创建新邀请先撤销旧邀请。Relay 全局最多保存 8 个待使用邀请，满载时显式拒绝。
- 邀请只能成功消费一次；并发确认时只有一个手机得到 Session，其余请求失败。过期、取消、来源连接断开和 Relay 重启后均不可兑换。
- 无效或过期的 `p_` 凭据不得计入 6 位 PIN 的十次失败锁定；普通错误 PIN 仍沿用现有锁定语义。
- 配对仅授予与手工 PIN 登录相同的 Web 权限和 24 小时 Session，不扩大 Codex 操作权限，也不绕过 Relay 的 Origin 检查、容量限制和协议校验。
- 本功能仍限定可信局域网。HTTP／WS 没有传输加密；二维码被旁观者拍摄时，两分钟内可能被抢先消费，所以手机端必须再次确认，二维码页面也必须明确这一风险。

## 错误处理

- 创建失败：二维码模态框显示“无法创建配对，请重试”，不得生成假二维码。
- 手机端无凭据、格式错误、过期、取消、已使用或 Relay 拒绝：不得自动改用 PIN、不得循环重试，显示原因归类和“使用连接码登录”。
- 配对连接中断但结果未知：清除页面内存中的配对凭据，要求重新扫码，不能重放兑换请求。
- 二维码渲染失败：显示可重试错误；默认不展示包含配对凭据的纯文本链接，避免误复制和日志传播。
- 原浏览器在等待期间断开：Relay 撤销邀请；手机确认失败，不建立 Session。

## 非目标

- 不做账号体系、跨局域网登录、云端中继、设备长期信任或 push 通知。
- 不通过二维码传递 PIN、Agent token、Codex 凭据或已有 Web Session。
- 不自动打开原生 App，不要求相机权限，也不在 Ariel 页面内调用摄像头；扫码由手机系统相机完成。
- 不改变“同一标签页刷新可恢复、关闭标签页后 Session 消失”的现有 `sessionStorage` 语义。

## TDD 与验收

1. Relay red tests：未鉴权不能创建；每连接单邀请、全局容量、两分钟过期、取消、来源断开、Relay 重启、并发单次消费与摘要存储均有覆盖。
2. 认证 red tests：合法 `p_` 只签发标准 `s_` Session；无效／过期／重放失败且不消耗 PIN 错误次数；配对 Session 不能冒充 Agent。
3. 协议 red tests：`auth.pair.create`、`auth.pair.cancel` 和 `auth.pair.consumed` 的字段、角色与方向均被 schema 约束，未知字段和错误角色被拒绝。
4. Web red tests：只有 ready 状态显示入口；二维码完全本地生成；fragment 先清除再确认；未点击确认不连接；确认成功保存 `s_` Session 并进入主界面；取消、过期、未知结果和手工 PIN 回退可见。
5. 安全测试：构建产物、HTTP 请求、Relay 日志、DOM 文本和 `sessionStorage` 中均不存在 PIN 或配对原文残留；二维码解码结果只含当前 Origin 与一次性 fragment。
6. 回归：Go race tests、`go vet`、Web tests／build 全部通过；用真实桌面浏览器展示二维码，真实手机相机扫码并点击确认，验证直接登录、原浏览器显示成功、二维码重放失败，且 Agent／Codex readiness 与既有会话操作不受影响。

## 验收标准

- 已登录浏览器两次点击内展示可扫描二维码，二维码两分钟后自动失效。
- 手机扫码后必须先看到确认页；确认一次即可进入已登录 Ariel，不需要看到或输入 6 位连接码。
- PIN、Agent token 和可复用 Web Session 从不进入二维码或 URL；一次性配对凭据不落盘、不记日志、不可重放。
- 关闭二维码、来源浏览器断开、超时、Relay 重启或任一失败都不会伪造成功，也不会影响现有已登录连接和 PIN 登录。

## 实施记录（2026-10-06）

- Relay 新增 `auth.pair.create`／`auth.pair.cancel` 本地请求和 `auth.pair.consumed` 事件；一次性凭据为 32 字节随机数，只在内存保存 SHA-256 摘要，固定两分钟过期、每连接一个、全局最多八个。
- 手机扫码 URL 将凭据放在 fragment；生产入口在 React 渲染前读取并立即清除地址栏。扫码只展示确认页，点击确认后才以 `p_` 凭据完成 WebSocket hello，并换取现有 `s_` Web Session。
- QR 使用锁定版本的本地 `qrcode` 依赖按需生成 SVG data URL，不访问第三方二维码服务；PIN、Agent token 和既有 Session 不进入二维码。
- Go race tests、`go vet`、Web 103 项测试与 production build 通过。本机 `restart-local` 部署后，真实 Relay 端到端验证了邀请创建、确认兑换、标准 Session、源端通知、设备列表和重放拒绝。详见[专项测试记录](../testing/2026-10-06-0027-qr-pairing-login.md)。
