# 0010：刷新页面复用 Web session

- 状态：Approved，用户于 2026-10-04 确认按此规格进入 TDD。
- 来源：2026-10-04 用户希望刷新页面后不再反复输入固定 6 位连接码，并询问浏览器与 Relay Server session。
- 依赖：[固定 6 位连接码](0008-fixed-six-digit-web-pin.md)。本规格确认后，覆盖其“刷新页面需重新输入”条款。

## 用户可见结果

- 手机首次打开仍输入固定 6 位连接码；同一浏览器标签页刷新后自动连接 Relay，不再重复输入。新标签页、浏览器清理会话数据、Relay 重启或 24 小时会话到期后，需要重新输入。
- 页面点击“断开”清除本标签页保存的 session，回到连接页；不会自动重连。
- 连接码本身不进入浏览器持久化存储。Relay 只在内存中保存随机 session 凭据的摘要，不保存第二套 Codex 会话或正文数据。

## 输入、输出与安全边界

- Web 首次用 PIN 完成 `hello`，Relay 生成高熵随机 session token，在 Web 的 `hello.ok` 中返回；Web 只把该随机 token 放在 `sessionStorage`。后续连接的 `hello.token` 可使用此 token，Relay 验证后取得同样 Web 权限。Agent 仍只能使用独立 `ARIEL_TOKEN`，不能使用 PIN 或 Web session token。
- Relay session 最长 24 小时、进程重启即失效，最多保留 32 个；超额淘汰最早的 token。已连上的 Web 不因 token 到期／淘汰被强制断开，但下次重连需输入 PIN。
- `hello.ok` 的 `sessionToken` 为 Web 专用可选字段，保留协议 v1；Agent ack 不含此字段。PIN 和 session token 都不能放 URL、日志或仓库文件。
- 错误 PIN 的 10 次进程级锁定仍有效；锁定后所有新的 Web 鉴别（包括 session token）都拒绝，已连接 Web 与 Agent 不断开。错误 session token 不消耗 PIN 错误次数。失效 session token 在浏览器清除后提示重输 PIN，不自动无限重试。
- HTTP/WS 局域网传输仍未加密；同网段窃听者可截获 PIN 或 session token。本功能只面向可信局域网，不宣称公网安全。

## TDD 与验收

1. 先写 Relay session 签发、复用、24 小时过期、32 个上限、重启清空、role 隔离、锁定边界的失败测试；再实现。
2. 先写 Web 首次 PIN 登录后保存 session、刷新自动连接、失效回退、断开清除的失败测试；再实现。
3. Go race tests、Web tests/build 后，在物理手机完成“首次输入 → 刷新自动连接 → 关闭标签再打开重输”的验收。
