# 0059 原生 Passkey 双认证验收

- 日期：2026-10-08
- 规格：[0059 无数据库的单用户 PIN / Passkey 双认证](../specs/0059-native-single-user-passkey-auth.md)
- 结论：自动测试、真实 Chromium virtual authenticator、production build、race、vet 与 Docker 双模式 smoke 均通过。

## TDD 记录

第一轮 Red 先加入以下契约测试：

- `configInput` 的 PIN / Passkey 互斥与 Passkey 必填配置；
- AES-256-GCM Cookie 的 round-trip、篡改、过期和用途隔离；
- Passkey 凭据文件的首次创建、`0600`、原子持久化、损坏文件拒绝；
- Passkey WebSocket 只接受已验证 Cookie，拒绝 PIN、`s_` Session 与 `p_` 配对凭据；
- Web base64url 转换、registration / assertion 序列化和无 PIN 降级 UI。

Red 阶段分别以缺少 `internal/webauth`、缺少 Relay Passkey 配置字段、缺少 Web `auth.ts` 和找不到“使用 Passkey 登录”结束。实现后上述测试转为 Green；随后继续以 Red → Green 补齐三类安全边界：ceremony Cookie 单次消费、并发首次登记只能成功一次，以及并发 assertion 写回不能让 sign counter、clone warning 或已锁存的 user verification 状态回退。现有凭据文件若不是私有普通文件（存在 group / other 权限）也会拒绝启动。

## 自动验证

```text
GO111MODULE=on GOTOOLCHAIN=auto go test ./...
PASS

GO111MODULE=on GOTOOLCHAIN=auto go test -race ./internal/webauth ./internal/relay ./cmd/relay
PASS

GO111MODULE=on GOTOOLCHAIN=auto go vet ./...
PASS

npm test
21 files / 159 tests passed

npm run build
PASS；Vite 保留既有 >500 kB chunk warning，无新增构建错误

npm run test:ui
15 tests passed
```

Playwright 的 Passkey 用例在临时自签名 HTTPS Origin 上启动真实 Relay，使用 Chromium DevTools `WebAuthn` virtual authenticator 完成：

1. Relay 注入 `passkey` 模式，页面不展示 6 位 PIN；
2. setup token 开始第一次 registration；
3. authenticator 创建 resident credential；
4. Relay 验证 attestation、原子保存 `auth.json`、签发 Secure HttpOnly Cookie；
5. WebSocket 依靠 Cookie 进入 `ready`；
6. 已登录页面切换到第二个 virtual authenticator，登记备用 Passkey；
7. 浏览器 logout 清除 Cookie；
8. 备用 Passkey 完成 assertion 并恢复 WebSocket `ready`。

## 容器验证

`docker build -t ariel-relay:0059-test .` 成功。随后分别启动：

- PIN 容器：`/healthz` 返回 `ok`，`/api/auth/status` 返回 `mode=pin`，HTML 注入 `ariel-auth-mode=pin`；
- Passkey 容器：使用私有 `/data` volume 启动，`/api/auth/status` 返回 `mode=passkey` 与 `enrollmentRequired=true`，HTML 注入 `ariel-auth-mode=passkey`。

验证后已删除临时容器、volume 与测试镜像；未涉及用户数据。

## 视觉证据

- [首次登记页面](../../web/e2e/passkey-auth.spec.ts-snapshots/passkey-first-enrollment-darwin.png)
- [日常 Passkey 登录页面](../../web/e2e/passkey-auth.spec.ts-snapshots/passkey-login-darwin.png)

两张截图均为 390×844、2× DPR、dark scheme 的真实 Chromium 输出。检查结果：彩色主标、标题和认证卡片在窄屏内无横向溢出；Passkey 模式无 PIN 输入与二维码入口；setup token 使用 password input；日常登录只保留单一主操作。

## 证据边界

- virtual authenticator 验证了真实 WebAuthn 协议、HTTPS Origin、Cookie 与 WebSocket 链路，但不等价于物理 iPhone / Android 的系统 Passkey UI。
- 本次未部署到真实公网域名，也未验证特定 TLS 反向代理产品；反向代理要求已写入运维文档。
- 无数据库设计不提供单 Session 服务端撤销和多用户审计；全局注销通过轮换 `ARIEL_SESSION_KEY` 完成。
