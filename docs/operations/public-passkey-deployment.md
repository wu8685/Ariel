# 公网 Passkey 部署

使用 Kubernetes 时，可直接采用[Kubernetes Passkey overlay](kubernetes-deployment.md#方式二公网-passkey)；本页保留通用安全模型和 Docker 示例。

Ariel 的公网模式面向单用户：通过 Passkey 登录后即可操作全部会话。它不依赖 OIDC 或数据库；Relay 只持久化一个私有 JSON 凭据文件，活动 Web Session 保存在浏览器的加密 `HttpOnly` Cookie 中。

## 拓扑

```text
Browser ── HTTPS / WSS ──> TLS reverse proxy ── HTTP / WS ──> Ariel Relay
                                                                  ↑
Desktop Agent ───────────────────────── WSS + Agent token ─────────┘
```

公网入口必须是稳定域名。Passkey 与 HTTPS Origin / RP ID 绑定，不能用公网 IP 临时替换域名，也不能在登记后随意切换域名。

## 1. 生成秘密

在安全终端中生成相互独立的 Agent token、Cookie key 和首次登记 token：

```sh
export ARIEL_TOKEN="$(openssl rand -hex 32)"
export ARIEL_SESSION_KEY="$(openssl rand -hex 32)"
export ARIEL_PASSKEY_SETUP_TOKEN="$(openssl rand -base64 32 | tr -d '\n')"
```

- `ARIEL_TOKEN` 只给 Relay 与 Desktop Agent。
- `ARIEL_SESSION_KEY` 必须是 64 个十六进制字符。轮换它会让全部浏览器会话失效。
- `ARIEL_PASSKEY_SETUP_TOKEN` 只用于登记第一个 Passkey；成功后应从部署 Secret 中移除。

不要把这些值写进仓库、Dockerfile、镜像 build args 或日志。

## 2. 启动 Passkey Relay

以下示例使用 `ariel.example.com`，并把 Passkey 公钥凭据持久化在 Docker volume 中：

```sh
export ARIEL_PUBLIC_ORIGIN='https://ariel.example.com'

docker network create ariel
docker volume create ariel-auth
docker run -d \
  --name ariel-relay \
  --restart unless-stopped \
  --network ariel \
  -v ariel-auth:/data \
  -e ARIEL_TOKEN \
  -e ARIEL_SESSION_KEY \
  -e ARIEL_PASSKEY_SETUP_TOKEN \
  -e ARIEL_WEB_AUTH=passkey \
  -e "ARIEL_ORIGINS=${ARIEL_PUBLIC_ORIGIN}" \
  -e "ARIEL_PUBLIC_ORIGIN=${ARIEL_PUBLIC_ORIGIN}" \
  -e ARIEL_WEBAUTHN_RP_ID=ariel.example.com \
  -e ARIEL_WEBAUTHN_CREDENTIALS_FILE=/data/auth.json \
  ariel-relay:local
```

Passkey 模式不得再设置 `ARIEL_WEB_PIN`。Relay 会拒绝同时出现 PIN 和 Passkey 配置，避免公网入口意外保留低熵降级路径。

## 3. 配置 TLS 反向代理

反向代理必须：

- 为公共域名提供有效 HTTPS 证书；
- 保留原始 `Host` 与 `Origin`；
- 支持 `/ws` 的 WebSocket Upgrade；
- 将 `/`、`/api/auth/*`、`/ws` 转发到同一个 Relay；
- 不缓存 `/api/auth/*` 和 HTML 登录页。

以 Caddy 为例：

```caddyfile
ariel.example.com {
  reverse_proxy ariel-relay:8080
}
```

不要直接向公网发布 Relay 的 8080 端口。`ARIEL_ORIGINS` 必须是浏览器实际访问的精确 Origin，例如 `https://ariel.example.com`，不包含路径或结尾斜杠。

## 4. 登记第一个 Passkey

1. 打开 `https://ariel.example.com`。
2. 输入首次登记 token，点击“创建 Passkey”。
3. 完成系统 Passkey 确认；页面会直接进入 Ariel。
4. 退出后再次用同一个 Passkey 登录，确认恢复正常。
5. 从容器或编排平台移除 `ARIEL_PASSKEY_SETUP_TOKEN`，重新创建容器。只要 `/data/auth.json` 仍在，Relay 不再要求它。

已登录后，可在左侧会话栏点击钥匙图标“添加 Passkey”，为另一台设备或安全密钥登记备用凭据。凭据文件支持保存多个 Passkey。

## 5. 连接 Desktop Agent

Desktop Agent 与 Relay 跨公网通信，使用与 Relay 相同的高熵 `ARIEL_TOKEN`：

```sh
./scripts/ariel.sh up agent \
  --relay-url 'wss://ariel.example.com/ws' \
  --token-file /path/to/private-agent-token \
  --device-id 'my-desktop' \
  --device-name '我的桌面设备'
```

公网连接不要使用 `--allow-insecure-ws`。Desktop Agent 与本机 Agent App 仍是本机集成，不经过 Relay 登录。

## 备份、恢复与撤销

- 备份 `/data/auth.json` 与 `ARIEL_SESSION_KEY`。凭据文件权限应保持 `0600`，备份也按秘密材料管理。
- 丢失所有 Passkey 时，没有数据库或 OIDC 恢复流程。停机后安全备份旧文件，移走原凭据文件，重新设置 bootstrap token，再登记新 Passkey。
- 单个浏览器登出只清除该浏览器 Cookie；全局注销通过轮换 `ARIEL_SESSION_KEY` 完成。
- Agent token 泄露时单独轮换 `ARIEL_TOKEN`，并同步更新所有 Desktop Agent。
- 域名或 RP ID 迁移通常需要重新登记 Passkey。

## 明确边界

Passkey 解决 Web 登录的共享密码与常规钓鱼风险，但不解决 Relay 主机失陷、终端被控制、TLS 终止错误或多用户审计。需要多用户、细粒度权限、集中撤销或企业身份治理时，再引入外部 OIDC 与服务端持久化会话，而不是扩展这份单用户文件。
