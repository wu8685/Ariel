# 0109：Web wire 向后兼容与协议非法帧修复

- 状态：Implemented（2026-10-26）。
- 来源：公网 Relay 更新至 `v0.8.0` 后，长期打开的旧 Web 页面收到“协议消息不合法”，此前还会被误判为登录失效并循环登录。
- 关联：[0107 Agent Transport 解耦与 Cache 自愈恢复](0107-resilient-agent-transport-and-cache-recovery.md)、[0108 登录失效与协议异常分离](0108-auth-and-protocol-failure-separation.md)。

## 已证实根因

1. `v0.8.0` 在 `thread.cache.update` 中新增可选字段 `leaseValidUntil`，但协议版本仍为 `v: 1`。
2. 上一版 Web 将 v1 schema 编译进静态 JS，其中 `thread.cache.update` 使用 `additionalProperties: false`，因此不接受该字段。
3. 使用上一版本真实 schema 对当前帧做确定性验证：不含 `leaseValidUntil` 时通过，加入后失败。
4. `leaseValidUntil` 只用于 Relay 内部判断 follower lease 是否有效；Web 源码不读取该字段。
5. HTML 已配置 `Cache-Control: no-store`，hash asset 已正确使用 immutable。问题不是 CDN 返回错误资源，而是手机浏览器保留了部署前已经运行的旧页面；WebSocket 重连后旧 JS 直接连接新版 Relay。

## 行为与边界

1. Relay 继续在进程内维护 `leaseValidUntil`，但不再把它放入 Web-facing cache response 或 `thread.cache.update` event。
2. 从公共 v1 schema 删除该字段；Web 不承担 Relay 内部 lease 计时，也不据此授权 mutation。
3. 长期打开的上一版本 Web 页面连接新版 Relay 后，既有 `thread.cache.update` 必须仍通过上一版 schema。
4. v1 已有 server-to-Web variant 不得直接新增字段，即使字段在新 schema 中声明为 optional；需要新增数据时必须满足以下一种条件：
   - 不上 wire，仅留在 Relay 内部；
   - 通过显式 capability／revision 协商后只向支持方发送；
   - 升级协议 major version，并保留已声明的兼容窗口。
5. 0108 的 `incompatible` UI 继续作为未知跨版本错误的最后保护，但不能替代 wire 向后兼容。
6. 不记录帧正文、token、Cookie、thread 内容或文件内容。

## TDD 与验收

1. Red：Relay cache 测试先证明当前 Web-facing cache data 泄漏 `leaseValidUntil`。
2. Red：加入一份最小的上一版本 `thread.cache.update` 兼容性契约，证明当前 Relay 事件无法被旧契约接受。
3. Green：删除对外字段后，上述测试通过，Relay 内部 lease 过期／续期测试仍通过。
4. Web 当前 schema、generated types 和使用方同步删除该字段；运行 protocol、Relay、Web 全量测试。
5. 运行 production build 与真实 Chromium 回归：模拟旧页面跨 Relay 更新重连，cache update 可继续消费，不进入登录或 incompatible 状态。
6. 公网发布后保留上一容器用于回滚，并由物理 iPhone／Safari 复验后台切换和旧标签页恢复。

## 回滚

恢复字段只会重新引入已证实的旧 Web 不兼容，因此回滚应使用上一完整版本的 Relay + Web 容器，不单独恢复该字段。

## 实现与验证

- 使用上一版本的真实 schema 确认：同一 `thread.cache.update` 不含 `leaseValidUntil` 时通过，包含时失败。
- Relay 兼容性契约使用严格的上一版本消息结构解码当前 cache update，防止内部字段再次泄漏。
- Go 全量测试、Relay／protocol race 与全量 vet 通过。
- Web 31 个测试文件、263 项测试通过；production build 通过。
- Chromium UI 48 项通过，包括 cache hot/cold handoff、Agent reconnect、协议异常保留正文和 Passkey 回归。
- 物理 iPhone／Safari：待公网发布后复验。
