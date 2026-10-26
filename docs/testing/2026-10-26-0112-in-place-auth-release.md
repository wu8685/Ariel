# v0.9.0 合并验收：原地续登录、模型设置与紧凑顶部

## 发布范围

- Issue #13／spec 0112：认证失效保留当前会话，不跳整页登录。
- Issue #7／PR #8／spec 0110：按会话选择模型、reasoning effort、速度。
- Issue #9／PR #10／spec 0111：52px header，44px touch target。
- Issue #11／PR #12／spec 0108：旧 WebSocket 回调与 deadline 代次隔离。
- 未纳入未批准／未完成的 0106 草案；Web 看板继续关闭。

## Red → Green

1. 实现前实际运行四项 App 测试：PIN 失效后的正文消失；Passkey Cookie 不会原位刷新；没有原位登录／取消重试入口；晚到的 Cookie 检查没有生命周期栅栏。四项失败。
2. 实现前真实 Chromium 手机 fixture 在 unauthorized 后找不到原位“恢复登录”区域；原行为展示登录整页，测试失败。fixture 不连接真实业务 Codex。
3. 追加状态请求 deadline 测试，旧实现 15 秒后仍 pending，先 red；加入 AbortController 后 green。
4. 修复后覆盖 Cookie 自动恢复有界、手动 Passkey 取消／重试、状态检查网络失败、Cookie／手动认证晚到与显式退出隔离、PIN 原位恢复、最新 snapshot 前禁发、不自动重放 turn.start。
5. 手机 viewport／touch 回归保留原正文和可编辑草稿，断言 header 52px、提示不遮挡 transcript／composer、320px 不横向溢出，并保存新的 390px screenshot baseline。既有普通状态截图未为续登录重录。

## 合并回归

| 验证 | 结果 |
| --- | --- |
| Web unit | 32 文件、286 项通过 |
| Chromium UI | 56 项通过，1 worker；含真实 Relay cache 完成态恢复与真实 WebAuthn virtual authenticator |
| `go test ./...` | 通过 |
| `go vet ./...` | 通过 |
| modelsettings／Desktop IPC／Desktop Agent／Relay／protocol `-race` | 通过 |
| Web production build | 通过，保留既有 Mermaid 等 chunk-size 非阻断提示 |
| 真实 Codex gated settings | 两项通过，包含真实 Relay／Agent／native 双向设置推送、下一轮参数与 bootstrap 后 Desktop 接管 |
| 物理 iPhone／Safari | 未验证，发布后由用户复验 |

真实 native 测试只写新建的 guard／no-tools fixture 并在结束时归档，不向既有业务会话发送内容；报告不保存 thread／owner 标识、凭据或真实正文。

## 发布与回滚检查

README 已先更新；本次发布 tag 为 v0.9.0。部署顺序为公网 Relay／Web，然后本机 launchd Desktop Agent。部署需验证相同 git revision、新 asset、health、Agent ready marker／连接、1 GiB container hard limit、768 MiB GOMEMLIMIT 与任务开关；保留旧容器和旧二进制，复用原有密钥／Passkey 数据／设备身份，不迁移业务会话。

现有浏览器标签页需主动刷新一次才能取得新 JS；新版本不承诺修补已在内存中运行的旧客户端。认证更新后仍需等新 watch snapshot 确认，不能把 Cookie 或 hello receipt 当作 owner authority。
