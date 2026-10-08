# 0059：无数据库的单用户 PIN / Passkey 双认证

- 状态：Implemented（Go / Web 单测、race、真实 Chromium virtual authenticator、production build 与 Docker 双模式 smoke 已验）。
- 范围：Relay Web 登录、WebSocket 鉴权、Passkey 凭据文件、无状态会话 Cookie、部署配置、Web 登录界面与安全文档。
- 不在范围：多用户、RBAC、操作级授权、外部 OIDC、Agent 与本机桌面 App 之间的鉴权、云端数据库。

## 背景

Ariel 需要同时覆盖两种部署形态：

1. 在可信局域网中快速部署，继续使用现有 6 位 PIN；
2. 将 Relay 暴露到公网时，不再依赖低熵 PIN，而使用浏览器原生 Passkey。

Relay 部署在云端，Agent 运行在桌面 App 所在机器。Agent 与 Relay 之间跨越公网，因此必须继续使用独立、高熵的 `ARIEL_TOKEN` 和 WSS。当前只有一个用户；登录成功即可操作全部会话，不引入多用户权限模型。

## 用户结果

- `pin` 模式保持当前使用方式：输入 6 位 PIN，Relay 签发短期 Web Session。
- `passkey` 模式中，浏览器通过平台 Passkey 登录，随后使用加密、`HttpOnly`、`Secure` Cookie 访问 WebSocket；页面 JavaScript 不接触会话密钥。
- Relay 不依赖 OIDC 或数据库。Passkey 公钥凭据保存在一个小型 JSON 文件中；Web Session 本身不落库。
- 两种模式互斥，公网模式不允许以 PIN 或旧 `s_` Session 降级登录。

## 配置契约

### 共同配置

| 变量 | 约束 |
| --- | --- |
| `ARIEL_TOKEN` | Agent 专用高熵密钥，不得与任何 Web 凭据复用 |
| `ARIEL_ORIGINS` | 允许连接 `/ws` 的精确 Origin 列表 |
| `ARIEL_WEB_AUTH` | `pin` 或 `passkey`；未设置时为 `pin`，兼容现有部署 |

### PIN 模式

| 变量 | 约束 |
| --- | --- |
| `ARIEL_WEB_PIN` | 必填，恰好 6 位 ASCII 数字，且不得与 `ARIEL_TOKEN` 相同 |

Passkey 专用变量不参与 PIN 模式。二维码一次性配对继续仅属于 PIN 模式。

### Passkey 模式

| 变量 | 约束 |
| --- | --- |
| `ARIEL_PUBLIC_ORIGIN` | 必填、唯一 HTTPS Origin，例如 `https://ariel.example.com` |
| `ARIEL_WEBAUTHN_RP_ID` | 必填；必须等于公共域名或其可注册父域 |
| `ARIEL_WEBAUTHN_CREDENTIALS_FILE` | 必填；保存单用户的一个或多个 Passkey 公钥凭据 |
| `ARIEL_SESSION_KEY` | 必填；64 个十六进制字符（32 bytes），用于 AES-256-GCM Cookie |
| `ARIEL_PASSKEY_SETUP_TOKEN` | 凭据文件尚无 Passkey 时必填；至少 32 个字符，仅用于首次登记 |

Passkey 模式禁止配置 `ARIEL_WEB_PIN`，避免产生可被误认为有效的降级入口。`ARIEL_PUBLIC_ORIGIN` 必须同时出现在 `ARIEL_ORIGINS` 中。

## HTTP 与 WebSocket 契约

### HTTP API

- `GET /api/auth/status`
  - 返回 `mode`、`authenticated`、`enrollmentRequired`。
- `POST /api/auth/passkey/register/options`
  - 首次登记要求 JSON 中提供 setup token；已有会话可登记额外 Passkey。
  - 返回 WebAuthn creation options；挑战保存在 5 分钟有效的加密 `HttpOnly` ceremony Cookie 中。
- `POST /api/auth/passkey/register/verify`
  - 验证 attestation，原子写入凭据文件，并签发会话 Cookie。
- `POST /api/auth/passkey/login/options`
  - 返回 assertion options，并写入短期 ceremony Cookie。
- `POST /api/auth/passkey/login/verify`
  - 验证 assertion，更新 counter / flags 后原子写入凭据文件，并签发会话 Cookie。
- `POST /api/auth/logout`
  - 清除当前浏览器会话 Cookie。

除 `status` 外，认证端点只接受 `POST application/json`，限制请求体大小，并校验请求 Origin 与 `ARIEL_PUBLIC_ORIGIN` 精确相等。

### Cookie

- 会话 Cookie：`__Host-ariel_session`；`Secure`、`HttpOnly`、`SameSite=Strict`、`Path=/`、无 `Domain`，有效期 24 小时。
- Ceremony Cookie：`__Host-ariel_webauthn`；同样使用 `__Host-` 约束，有效期 5 分钟，验证后清除。
- Cookie 内容以 AES-256-GCM 加密并认证，包含用途、签发时间、过期时间和必要 payload；不同用途使用独立 associated data。
- Relay 不保存活动 Web Session。登出只删除当前浏览器 Cookie；需要全局注销时轮换 `ARIEL_SESSION_KEY`。

### WebSocket

- Agent 继续在 `hello.role=agent` 中提交 `ARIEL_TOKEN`。
- PIN 模式保持现有 PIN、`s_` Session 和一次性 `p_` 配对行为。
- Passkey 模式的浏览器仍发送协议兼容占位 token，但 Relay 只认可 WebSocket Upgrade 请求中的有效会话 Cookie；PIN、`s_` 和 `p_` 都不得通过。
- 所有 Web 连接继续要求精确 Origin allowlist；Passkey Cookie 不能绕过 Origin 校验。
- Passkey 模式禁用 `auth.pair.create` / `auth.pair.cancel`。

## 凭据文件

- JSON 文件带显式 `version`，保存固定单用户身份和 `webauthn.Credential` 列表。
- 支持多个 Passkey，便于用户在手机和电脑分别登记。
- 文件缺失等价于尚未登记；语法错误、未知版本或损坏内容必须阻止 Relay 启动，不得静默覆盖。
- 写入采用同目录临时文件、`0600` 权限与原子 rename；写入失败时不签发会话。
- setup token、session key 和 Agent token 不写入凭据文件、不写日志、不进入镜像。

## Web 交互

1. 页面首先请求 `/api/auth/status`，在结果返回前显示中性的认证加载态，不能短暂闪现错误登录方式。
2. `pin` 模式继续展示现有 6 位 PIN 输入。
3. `passkey` 模式：
   - 尚未登记时展示 setup token 输入和“创建 Passkey”；
   - 已登记但未登录时展示“使用 Passkey 登录”；
   - 已登录时直接连接 WebSocket；
   - 已登录时可从会话侧栏登记额外 Passkey；
   - 不展示二维码配对入口；
   - 断开操作同时调用 logout，并回到 Passkey 登录态。
4. WebAuthn 二进制字段使用 base64url 在 JSON 与 `ArrayBuffer` 之间转换；仅将浏览器实际返回的 credential 发送给 Relay。
5. 用户取消系统 Passkey 对话框时停留在登录页，给出可重试的非破坏性提示。

## 安全边界

- 公网部署必须由反向代理提供 HTTPS/WSS；Relay 自身可以继续监听内网 HTTP。
- PIN 只适合可信局域网，文档不得把它描述为公网安全方案。
- Passkey 抵抗共享密码泄露和常规钓鱼，但不解决终端被控制、Relay 主机被攻陷或反向代理配置错误。
- 无数据库方案不提供逐会话服务端撤销、审计日志和多用户恢复流程；这些能力若成为需求，再引入持久化会话或外部身份系统。
- 首次 setup token 是一次性 bootstrap 能力：一旦首个 Passkey 写入，未登录请求不能再用它登记；管理员应随后从部署 Secret 中移除。

## 验收

1. 配置测试覆盖两种模式、默认兼容、互斥变量、HTTPS Origin、RP ID、32-byte session key 与首次 setup token。
2. Cookie 测试覆盖 round-trip、篡改、过期和用途隔离；响应 Cookie 具备全部安全属性。
3. 凭据文件测试覆盖首次创建、多凭据、损坏文件拒绝、`0600` 与原子更新。
4. Relay 测试证明 Passkey 模式拒绝 PIN / `s_` / `p_`，只有有效 Cookie 能建立 WebSocket；Agent token 行为不变。
5. API 测试覆盖错误 Origin、错误 Content-Type、错误 setup token、重复/过期 ceremony 和 logout。
6. Web 单元测试覆盖 base64url 转换、登记、登录、取消、断开和 PIN 回归。
7. 使用真实 Chromium + virtual authenticator 完成“首次登记 → 退出 → 再登录 → WebSocket ready”的端到端验收；另保留 PIN 模式的真实浏览器回归。
8. 运行 Go 全量测试、Web lint / 单测 / production build、Playwright，以及 Docker 构建契约测试。

## 实现结果

Relay 现在以 `ARIEL_WEB_AUTH` 在 PIN 与 Passkey 间显式选择。Passkey 模式由 `internal/webauth` 提供 WebAuthn ceremony、AES-256-GCM `__Host-` Cookie、单次 ceremony replay 防护和原子 JSON credential store；Relay WebSocket 只接受经 Cookie verifier 确认的浏览器请求，并继续独立验证 Agent token。Web 根据 Relay 注入的认证模式同步选择登录界面，支持首次登记、日常登录、logout 和自动恢复，不暴露 PIN 或二维码降级入口。

部署与恢复流程见[公网 Passkey 部署](../operations/public-passkey-deployment.md)，完整证据见[0059 验收记录](../testing/2026-10-08-0059-native-passkey-auth.md)。
