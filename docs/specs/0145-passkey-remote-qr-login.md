# 0145：Passkey 远程扫码登录

- 状态：Implemented（2026-11-09，产品负责人明确要求；Go race、Web 434 项与真实 Chromium virtual authenticator 端到端已验。`passkey-login` 的 darwin 截图基线需在 macOS 上重新生成。物理手机扫码待复验。未部署）。
- 关联：[0059 单用户 Passkey](0059-native-single-user-passkey-auth.md)、[0084 滑动会话](0084-passkey-sliding-session-and-durable-resume.md)、[0108 认证与协议失败分离](0108-auth-and-protocol-failure-separation.md)、[0112 原地重登录](0112-in-place-session-reauthentication.md)、[0027 PIN 扫码配对](0027-qr-pairing-login.md)。
- 范围：Passkey 模式下，没有用户 Passkey 的浏览器经一次性挑战获得与普通登录相同的 `__Host-ariel_session` Cookie。
- 不在范围：共享密码、跳过 Passkey 的 magic link、可转让的长期秘密、PIN / `s_` / `p_` 降级、改变 0084 滑动续期或 0112 原地重登录。

## 背景

公网 Web 只有「使用 Passkey 登录」。Passkey 绑定在用户自己的认证器上，云端或远程浏览器无法完成本机 WebAuthn。0027 的方向相反：已登录浏览器展示二维码，手机领取会话，而且 0059 在 Passkey 模式禁用了 `auth.pair.*`。那条禁令继续有效。本规格是另一条路，方向为：未登录浏览器出示挑战，已持有 Passkey 会话的设备批准。

## 用户可见行为

1. 已登记 Passkey、当前未登录、且没有待处理的批准链接时，登录门在「使用 Passkey 登录」之下提供「扫码远程登录」。首次登记页不提供该入口。
2. 点击后，这台浏览器向 Relay 创建挑战，并用本地代码生成二维码。对话框显示二维码、6 位核对码、审批链接、倒计时和状态：等待扫码、已批准、已拒绝、已过期、已取消。
3. 二维码和链接只包含当前 Origin 与挑战 `id`，例如 `https://www.kan.chat/?remoteLogin=<id>`。其中没有会话 Cookie、Poll token、Passkey、setup token 或 Agent token。
4. 用户用已登录 Ariel 的手机或电脑打开该链接，或在能使用 Passkey 的设备上打开。页面先读取并立刻从地址栏移除 `remoteLogin`。
5. 批准设备若尚无有效会话，必须先完成普通 Passkey 登录。登录成功不得自动批准。随后用户核对两边一致的核对码，并明确点击「批准远程登录」或「拒绝」。
6. 等待中的浏览器轮询挑战。只有批准后的下一次正确兑换才设置与 0059 / 0084 相同的会话 Cookie，然后按现有 `cookie` WebSocket 握手进入 Ariel。
7. 关闭或重新生成会取消未兑换的挑战。过期、拒绝、取消、Relay 重启或兑换之后，同一二维码不能再签发会话。
8. 0112 的原地重登录条保持不变，仍只提供本机 Passkey 或 PIN。本规格不延长 Cookie 寿命，也不把协议错误显示成登录失效。

## 输入与输出

| 端点 | 谁可以调用 | 输入 | 输出 |
| --- | --- | --- | --- |
| `POST /api/auth/remote-login/start` | 未登录浏览器；必须已有 Passkey | `{}` | `id`、只返回一次的 `pollToken`、`expiresAt`、核对码。不设置会话 Cookie |
| `POST /api/auth/remote-login/status` | 持有 `id` 的人 | `{id}` | `pending` / `approved` / `rejected` / `cancelled` / `expired`、过期时间、核对码。不返回 `pollToken` |
| `POST /api/auth/remote-login/approve` | 有效 Passkey 会话 | `{id}` | 把 `pending` 标为 `approved`。不设置、不轮换会话 Cookie |
| `POST /api/auth/remote-login/reject` | 有效 Passkey 会话 | `{id}` | 把 `pending` 标为 `rejected` |
| `POST /api/auth/remote-login/poll` | 持有 `pollToken` 的创建者 | `{id,pollToken}` | `pending` 时不设置 Cookie；`approved` 时原子兑换并设置普通会话 Cookie；终态不再次签发 |
| `POST /api/auth/remote-login/cancel` | 持有 `pollToken` 的创建者 | `{id,pollToken}` | 未兑换挑战变为 `cancelled` |

所有 POST 都要求 `Origin` 精确等于 `ARIEL_PUBLIC_ORIGIN`，且 `Content-Type` 为 `application/json`。响应 `Cache-Control: no-store`。

## 不变量与安全边界

1. Passkey 仍是唯一认证因素。没有有效 `__Host-ariel_session` 时，`approve` 与 `reject` 失败。Ceremony Cookie、setup token、PIN、`s_` 和 `p_` 都不能批准。
2. 二维码、URL、核对码和 `status` 响应都不足以兑换会话。兑换材料是 32 字节 `pollToken`，只在 `start` 响应里出现一次，Relay 只保存它的 SHA-256。
3. 挑战有效期 3 分钟，只能成功兑换一次。错误的 `pollToken` 不得作废已批准的挑战。并发兑换只有一个响应设置 Cookie。
4. 批准是幂等的；批准之后不能再拒绝。拒绝或取消之后不能再批准。过期后不能批准，也不能得到 Cookie。
5. 内存中最多 32 个未过期挑战。满额返回容量错误。Relay 重启后全部失效。挑战不写磁盘、日志或 Crash dump。
6. 兑换得到的 Cookie 与 `login/verify` 使用同一封装：`__Host-ariel_session`、`Secure`、`HttpOnly`、`SameSite=Strict`、`Path=/`、24 小时滑动窗口的起点。0084 的续期仍只发生在有效会话访问 `/api/auth/status` 时，批准本身不续期。
7. 0059 对 Passkey 模式禁用 `auth.pair.create` / `auth.pair.cancel` 的规定不变。
8. 核对码只用于让用户比对两块屏幕，不是第二个密码。它由挑战 `id` 派生，知道 `id` 的人都能看到。

## 错误处理

- 尚未登记 Passkey：`start` 返回与登录相同的 enrollment 冲突，页面不展示入口。
- 创建失败或二维码渲染失败：对话框给出可重试错误，不显示假二维码。
- 未知、伪造或已兑换的 `id` / `pollToken`：不签发 Cookie。等待页显示失效，不自动改走 Passkey ceremony。
- 批准设备取消 Passkey 对话框：停留在登录页，挑战保持 `pending`，直到过期、取消或用户稍后批准。
- 结果未知的轮询不重放已删除的 `pollToken`；用户关闭对话框后必须重新生成。

## 非目标

- 不把会话令牌放进二维码。
- 不增加密码、邮件链接或可复制到多台设备长期使用的秘密。
- 不改变 Agent token、Codex 权限、原地重登录或协议不兼容提示。
- 不在页面内打开摄像头；扫码由系统相机完成。二维码不请求第三方服务。

## 验收

1. Go 测试覆盖：未登记不能创建；无会话不能批准；Ceremony Cookie 不能批准；错误 Origin 被拒绝；过期不能得到 Cookie；错误 Poll token 不消耗批准；正确兑换只签发一次会话 Cookie；重放、拒绝和取消不再签发；容量上限；`status` 与批准响应不含 Poll token。
2. Web 测试覆盖：地址栏中的 `remoteLogin` 被立即移除；链接只含 `id`；登录门可创建二维码且页面文本不含 Poll token；轮询到批准后以 `cookie` 连接；Passkey 登录本身不调用批准；已登录设备必须再点击批准。
3. 真实 Chromium virtual authenticator：已登记设备退出后打开远程链接，先 Passkey 再批准，另一无认证器的浏览器进入已连接状态；同一链接不能让第三台浏览器直接连接。
4. 0112 原地重登录的交互与几何断言保持通过。物理手机扫码留待发布后复验。

## 实现记录（2026-11-09）

Relay 在 Passkey 模式增加六个 `POST /api/auth/remote-login/*` 端点。挑战只存在进程内存，三分钟过期，全局最多 32 个；Poll token 只以 SHA-256 保存，并只在创建响应中出现一次。批准要求现有 `__Host-ariel_session`，兑换复用原来的会话 Cookie 封装。0027 的 `auth.pair.*` 在 Passkey 模式仍然禁用。

Web 登录门在已登记且未登录时提供「扫码远程登录」。二维码由本地 `qrcode` 生成，内容是 `/?remoteLogin=<id>`。批准页先清掉地址栏中的 id；若尚未登录，Passkey 成功后仍要再点「批准远程登录」。

验证：`go test -race ./internal/webauth/`、`npm test`（434）、`tsc -b`，以及 `passkey-auth.spec.ts` 在 Linux Chromium virtual authenticator 上走完登记、退出、远程挑战、Passkey 后再批准、等待浏览器连接、重放链接不能登录。未替换仓库里的 darwin 截图，也未部署公网。
