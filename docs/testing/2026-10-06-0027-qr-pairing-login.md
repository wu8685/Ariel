# 0027 二维码一次性配对登录测试记录

- 日期：2026-10-06
- 范围：Relay 一次性配对状态、协议约束、Web 二维码与手机确认页、真实本机 Relay 配对链路
- 数据边界：未打印或提交真实 PIN、Agent token、Web Session、配对凭据和会话正文；未执行 Codex 写操作

## TDD

1. Red：协议 schema 拒绝 `auth.pair.create`、`auth.pair.cancel`、`auth.pair.consumed`；Relay 缺少配对状态；Web 缺少二维码模块、扫码确认页与入口，定向测试实际失败。
2. Green：Relay 覆盖摘要存储、单次消费、两分钟过期、替换、取消、来源断开、全局容量、并发确认、Agent 角色隔离和 PIN 错误次数隔离。
3. Green：Web 覆盖 fragment 读取后立即清除、扫码不自动连接、明确确认、取消回退、标准 Session 保存、二维码本地生成、源端成功状态，以及结果未知时不重放一次性凭据。
4. Refactor：标准 Web Session 签发复用一条实现；QR 模块按需加载，未使用扫码功能时不把生成器合入主入口 chunk。

## 自动验证

- `GO111MODULE=on go test -race ./...`：通过。
- `GO111MODULE=on go vet ./...`：通过。
- `npm test`：14 个文件、103 项测试通过。
- `npm run build`：通过；二维码生成器拆为独立按需 chunk，Vite 仅报告既有的主 chunk 大小提示。

## 真实本机部署与协议验收

- `GO111MODULE=on ./scripts/ariel.sh restart-local`：构建完成后只重启受管 Relay／Agent，`status` 显示 Relay 在线、Agent 已握手且 Codex 就绪。
- 端到端探针从受保护的本机文件读取 PIN 且不打印，连接正在运行的 Relay 创建邀请；返回值为预期的 `p_` 高熵格式。
- 模拟手机明确确认后获得标准 `s_` Web Session，源 Web 收到不含敏感字段的 `auth.pair.consumed`，并可正常调用 `device.list`。
- 使用同一配对凭据再次连接被拒绝，证明运行中 Relay 的一次性消费生效。
- 浏览器自动化接口连续超时，因此未声称完成物理手机相机扫码。最终人工点验项：已登录浏览器点击“手机扫码登录”→手机系统相机扫描→确认页点击“确认在此手机登录”→进入 Ariel。
