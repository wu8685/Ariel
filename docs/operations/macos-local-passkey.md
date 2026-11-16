# macOS 本机 Passkey，与公网入口并存

[文档导航](../README.md) · [安装与排障](agent-install.md) · [公网 Passkey](public-passkey-deployment.md)

本文只描述已经实现的配置：在**不是**生产 Desktop Agent 那台机器上，另起一套只监听 loopback 的 Relay + Web + Desktop Agent，用 Passkey 登录，并使用这台 Mac 自己的 Codex Desktop。公网 `https://www.kan.chat` 与生产 Agent 保持原样。不要用本文去改生产容器、生产 launchd 或生产凭据。

本机一键命令 `scripts/ariel.sh up local` 是另一条路径：它固定使用 6 位 PIN 和明文 HTTP，不能当成这里的 Passkey 入口。

## 结论

可以并存。两套栈不共享 Relay 进程、Agent token、Passkey 凭据、Cookie key、device id、监听端口或 Codex IPC。浏览器靠 Origin 选择入口：

| 用途 | 地址 | 会话在哪 |
| --- | --- | --- |
| 这台 Mac 上的日常 Codex | `https://localhost:8443/` | 这台 Mac 的 Codex Desktop |
| 现有远程入口 | `https://www.kan.chat/` | 生产 Agent 所在机器的 Codex Desktop |

没有应用内开关。两个书签就是两种模式。Passkey、Cookie 和 device id 都按 Origin／Relay 分开，不能把生产 Passkey 挪过来用。

## 不要做的三件事

1. **不要**把这台 Mac 的 Agent 指到 `wss://www.kan.chat/ws`，也不要复制生产 Agent token。Relay 用同一个高熵 token 接纳该 Relay 上的 Desktop Agent；device id 是注册键。后连上的同 device id 会关掉先连上的连接（`internal/relay/server.go` 在替换 `agents[deviceID]` 后关闭旧 socket）。换一个新 device id 也不会变成“不走网络”：浏览器仍要到达公网 Relay，Agent 的 WebSocket 仍依赖这条广域链路，而且这台 Mac 的 Codex 会出现在公网站点上。
2. **不要**在这台 Mac 上再运行第二个 Desktop Agent（包括 `ariel.sh up local` 或 `ariel.sh up agent`）。默认 Codex IPC 是该用户的 `~/.codex/ipc/ipc.sock`，同一用户上的两个 Ariel Agent 会同时附着这个 socket。
3. **不要**在这台 Mac 上运行 `scripts/deploy-ecs.sh`，不要把本机 Origin 配成 `https://www.kan.chat`，不要绑定 `0.0.0.0`、`80` 或 `443`。那份脚本是 Linux 上的 Relay 部署，默认状态目录是 `/opt/ariel`。

本文件不安装 launchd。不要从生产 Agent 主机拷贝 plist、环境变量或程序参数。Mac 重启后需要按下面的启动顺序再拉起；在单独评审之前不要做成开机自启。

## 前提

在目标 Mac 上、用将要使用 Codex 的那个用户执行：

1. 仓库只从 `https://github.com/wu8685/Ariel.git` 克隆到这台机器上的新目录。不要使用生产 Agent 主机上的检出，也不要 `git pull` 覆盖已有工作区。
2. Go 1.26+、Node.js `^20.19.0 || >=22.12.0`（新装建议 22.12+）、以及官方发布的 Caddy 2。确认 `caddy version` 输出 2.x。不要用 `curl | sh` 安装。
3. Codex Desktop 已安装在 `/Applications/ChatGPT.app` 并且正在运行。最低门禁是 Desktop `26.930.31730`、内置 Codex `0.160.0`。更高版本可以启动，但未逐版本验证的私有 IPC 会在使用中显式失败，而不会被猜成 supported。
4. `~/.codex/ipc/ipc.sock` 存在，且是当前用户自己的 socket。不要设置 `ARIEL_IPC_SOCKET`、`ARIEL_CODEX_BINARY` 或 `ARIEL_APP_PATH`，除非 Desktop 不在默认位置；即便那时也只能指向这台 Mac 上的 Codex，不能指向另一台机器上的路径。
5. 这台 Mac 上没有已由 Ariel 管理的 Desktop Agent。`lsof` 只能用来确认端口无人占用，不能按端口杀进程。

固定绑定：

| 项 | 值 |
| --- | --- |
| 代码 SHA | `b9623f6c59e78628db6d23f39e6c223891dbc553` |
| 源码目录 | `~/src/Ariel-local` |
| 状态目录 | `~/Library/Application Support/ArielLocal` |
| Relay | `127.0.0.1:18080`，仅 loopback |
| 浏览器入口 | `https://localhost:8443/` |
| RP ID | `localhost` |
| device id | `work-mac`（稳定值；不要用生产 Agent 的 id） |

`localhost` 是 WebAuthn 允许的 RP ID。不要改用 IP：Relay 拒绝把 IP 当作 `ARIEL_WEBAUTHN_RP_ID`。不要改用这台机器的 `.local` 主机名；那会变成另一个 Origin，手机上的 `localhost` 也不是这台电脑。

## 1. 取出指定提交

```sh
git clone https://github.com/wu8685/Ariel.git "$HOME/src/Ariel-local"
cd "$HOME/src/Ariel-local"
git checkout b9623f6c59e78628db6d23f39e6c223891dbc553
test "$(git rev-parse HEAD)" = "b9623f6c59e78628db6d23f39e6c223891dbc553"
```

## 2. 状态目录与新凭据

下面的口令只在这台 Mac 上生成。不要从生产主机、容器、`runtime.env`、launchd 或聊天记录里复制。

```sh
umask 077
STATE="$HOME/Library/Application Support/ArielLocal"
install -d -m 0700 "$STATE" "$STATE/bin" "$STATE/logs"
openssl rand -hex 32 > "$STATE/agent-token"
openssl rand -hex 32 > "$STATE/session-key"
openssl rand -base64 32 | tr -d '\n' > "$STATE/passkey-setup-token"
chmod 0600 "$STATE/agent-token" "$STATE/session-key" "$STATE/passkey-setup-token"
```

生成后只在本机终端检查长度，不要把内容打印到工单、聊天或仓库：

```sh
test "$(wc -c < "$STATE/agent-token" | tr -d ' ')" -ge 64
test "$(wc -c < "$STATE/session-key" | tr -d ' ')" -ge 64
test "$(wc -c < "$STATE/passkey-setup-token" | tr -d ' ')" -ge 32
```

`auth.json` 由首次登记时的 Relay 创建。事先不要从任何地方放进这个目录。

## 3. 构建

```sh
cd "$HOME/src/Ariel-local/web"
npm ci
npm run build
cd "$HOME/src/Ariel-local"
GO111MODULE=on go build -o "$STATE/bin/ariel-relay" ./cmd/relay
GO111MODULE=on go build -o "$STATE/bin/ariel-desktop-agent" ./cmd/desktop-agent
```

二进制放在状态目录，不放进检出的 `.local/bin`，这样 `ariel.sh` 不会把它们当成自己托管的进程。

## 4. 确认端口空闲

```sh
nc -z 127.0.0.1 18080 && echo '18080 occupied' || echo '18080 free'
nc -z 127.0.0.1 8443 && echo '8443 occupied' || echo '8443 free'
nc -z ::1 8443 && echo '8443 ipv6 occupied' || echo '8443 ipv6 free'
```

任一端口已占用就停止并查明占用者。不要杀进程，也不要改去抢 `8080`、`80` 或 `443`。

## 5. 本机 TLS

Relay 自己只提供 HTTP。Passkey 要求浏览器 Origin 为 HTTPS，所以由 Caddy 在 loopback 终止 TLS。把下面内容写入 `"$STATE/Caddyfile"`，权限 `0600`：

```caddyfile
{
	admin off
	auto_https disable_redirects
}
https://localhost:8443 {
	bind 127.0.0.1 ::1
	tls internal
	reverse_proxy 127.0.0.1:18080
}
```

同时绑定 `127.0.0.1` 和 `::1`，避免浏览器把 `localhost` 解析到另一族地址。然后：

```sh
caddy validate --config "$STATE/Caddyfile"
caddy trust
```

`caddy trust` 会把 Caddy 的本机 CA 装进这台 Mac 的信任库，可能要求本机管理员确认。这个 CA 只服务本机 `localhost`，不要把它或 `caddy-data` 拷到别的机器，也不能代替 `www.kan.chat` 的证书。信任提示出现时，证书颁发者应是 Caddy 的本地 CA；如果是公司代理的拦截证书，停下来，不要继续登记 Passkey。

## 6. 启动 Relay

在同一个终端里读取凭据并启动。这些 `export` 不会把口令写进命令参数。同一 macOS 用户仍可能读取自己进程的环境，这是现有 Relay 的限制；保护这个用户会话即可。

```sh
cd "$HOME/src/Ariel-local"
export ARIEL_TOKEN="$(tr -d '\n' < "$STATE/agent-token")"
export ARIEL_SESSION_KEY="$(tr -d '\n' < "$STATE/session-key")"
export ARIEL_PASSKEY_SETUP_TOKEN="$(tr -d '\n' < "$STATE/passkey-setup-token")"
export ARIEL_WEB_AUTH=passkey
export ARIEL_PUBLIC_ORIGIN='https://localhost:8443'
export ARIEL_ORIGINS='https://localhost:8443'
export ARIEL_WEBAUTHN_RP_ID=localhost
export ARIEL_WEBAUTHN_CREDENTIALS_FILE="$STATE/auth.json"
export ARIEL_WEB_DIST="$HOME/src/Ariel-local/web/dist"
export ARIEL_LISTEN='127.0.0.1:18080'
unset ARIEL_WEB_PIN ARIEL_PUCK_ENABLED ARIEL_ENABLE_TASKS \
  ARIEL_ENABLE_FILE_CONTEXT ARIEL_ENABLE_CHILLING_FILE_CONTEXT \
  ARIEL_CHILLING_ENDPOINT ARIEL_CHILLING_TASK_ENDPOINT
nohup "$STATE/bin/ariel-relay" >> "$STATE/logs/relay.log" 2>&1 &
echo $! > "$STATE/relay.pid"
```

不要设置 `ARIEL_WEB_PIN`。Passkey 模式遇到 PIN 会拒绝启动。不要打开 Puck、任务或文件开关；它们不是这条并存路径的一部分，任务开关尤其不应指向任何已有任务库。

```sh
curl -fsS http://127.0.0.1:18080/healthz
```

期望输出 `ok`。然后启动 Caddy：

```sh
caddy start --config "$STATE/Caddyfile" --pidfile "$STATE/caddy.pid"
curl -fsS https://localhost:8443/healthz
```

第二次也应是 `ok`。浏览器只打开 `https://localhost:8443/`。不要把 `http://127.0.0.1:18080/` 存成书签：那个 Origin 不是 Passkey 配置的 HTTPS Origin，Secure Cookie 也不会留在 HTTP 上。

## 7. 登记 Passkey

1. 用这台 Mac 上的浏览器打开 `https://localhost:8443/`。证书不受信任时先停止，回到第 5 节，不要选择“仍要访问”。
2. 页面应要求首次登记，而不是 6 位 PIN。在终端读取 setup token 并粘贴到页面：`cat "$STATE/passkey-setup-token"`。不要把这行输出贴到聊天或工单。
3. 选择“创建 Passkey”，完成系统确认。这是 RP ID 为 `localhost` 的新 Passkey，不是 `www.kan.chat` 那一把。
4. 进入 Ariel 后退出，再用同一把 Passkey 登录一次。
5. 确认 `"$STATE/auth.json"` 已生成且权限是 `0600`。然后去掉 bootstrap。Caddy 保持运行；此时 Agent 还没启动。在仍保留第 6 节环境变量的同一个终端里重启 Relay。终端如果已经关掉，先按第 6 节重新 export，但跳过 `ARIEL_PASSKEY_SETUP_TOKEN`。

```sh
rm -f "$STATE/passkey-setup-token"
kill -TERM "$(tr -d '[:space:]' < "$STATE/relay.pid")"
unset ARIEL_PASSKEY_SETUP_TOKEN
i=0
while nc -z 127.0.0.1 18080 && [ "$i" -lt 25 ]; do
  i=$((i + 1))
  sleep 0.2
done
nohup "$STATE/bin/ariel-relay" >> "$STATE/logs/relay.log" 2>&1 &
echo $! > "$STATE/relay.pid"
curl -fsS http://127.0.0.1:18080/healthz
```

Relay 在 `auth.json` 已经有凭据时不再要求 setup token。端口若仍被占用，不要再启动第二个进程。
6. 再次打开 `https://localhost:8443/`。应只剩“使用 Passkey 登录”，没有 setup token 字段，也没有 PIN。

凭据文件损坏时 Relay 会拒绝启动，不会悄悄覆盖。丢失全部 Passkey 时，先停 Relay，把 `auth.json` 移到离线备份，再重新生成 setup token 并登记。不要从生产环境拿 `auth.json` 来恢复。

## 8. 启动本机 Agent

先确认 Desktop 正在运行，且 `~/.codex/ipc/ipc.sock` 存在。Agent 连的是本机 Relay 的 loopback HTTP，不经过 Caddy，也不出这台机器：

```sh
export ARIEL_TOKEN="$(tr -d '\n' < "$STATE/agent-token")"
export ARIEL_RELAY_URL='ws://127.0.0.1:18080/ws'
export ARIEL_DEVICE_ID='work-mac'
export ARIEL_DEVICE_NAME='工作 Mac'
export ARIEL_READY_FILE="$STATE/agent.ready"
export NO_PROXY="${NO_PROXY:+$NO_PROXY,}localhost,127.0.0.1"
export no_proxy="${no_proxy:+$no_proxy,}localhost,127.0.0.1"
unset ARIEL_IPC_SOCKET ARIEL_CODEX_BINARY ARIEL_APP_PATH ARIEL_PUCK_BRAIN_ENABLED ARIEL_ENABLE_TASKS
rm -f "$STATE/agent.ready"
nohup "$STATE/bin/ariel-desktop-agent" >> "$STATE/logs/desktop-agent.log" 2>&1 &
echo $! > "$STATE/agent.pid"
```

`ws://127.0.0.1:18080/ws` 只存在于这台机器的 loopback。不要改成 `wss://www.kan.chat/ws`，也不要加生产 token。公司 HTTP 代理必须绕过 `localhost` 和 `127.0.0.1`，否则 Agent 进程还在，握手却到不了本机 Relay。

就绪时 `"$STATE/agent.ready"` 出现，里面是 Agent PID。日志里应有 Codex 二进制选择记录，不应出现生产主机名或公网 Relay 地址。然后刷新 `https://localhost:8443/`，设备列表里应是“工作 Mac”，而不是生产 Agent。

只做只读确认：设备在线、会话列表能打开。不要向真实会话发送、停止、审批或删除。

## 9. 如何选择本地还是远程

- 要用**这台 Mac 的 Codex**，并且不想依赖广域网：只开 `https://localhost:8443/`。
- 要用**生产 Agent 那台机器的 Codex**，或人在别的设备上：只开 `https://www.kan.chat/`，用原来的 Passkey。
- 两个页面可以同时开着。Cookie 名都是 `__Host-ariel_session`，但没有 `Domain`，不能跨 Origin。`www.kan.chat` 的登录不会授权 `localhost`，反过来也不会。
- 本地页面看不到生产机器上的会话。Ariel 不复制会话，也没有第二份会话数据库；每台 Desktop Agent 只读它所在机器的 Codex。不要为了“同步”而复制 `~/.codex`。
- 手机访问 `localhost` 到不了这台电脑。手机继续用 `https://www.kan.chat/`。那条远程路径仍然到达生产 Agent，不是这台工作 Mac 的 Codex。不要为了手机改成局域网 HTTP 或 PIN。

## 10. 停止

只停止本文件记下的 PID，并先核对命令行里包含本状态目录的二进制。不要按端口杀，不要 `killall`，不要卸载生产主机上的服务。

```sh
STATE="$HOME/Library/Application Support/ArielLocal"
for name in agent relay; do
  pidfile="$STATE/$name.pid"
  if [ -f "$pidfile" ]; then
    pid="$(tr -d '[:space:]' < "$pidfile")"
    cmd="$(ps -p "$pid" -o command= 2>/dev/null || true)"
    case "$cmd" in
      "$STATE/bin/ariel-"*) kill -TERM "$pid"; rm -f "$pidfile" ;;
      "") rm -f "$pidfile" ;;
      *) echo "skip $name: pid is not the local binary" ;;
    esac
  fi
done
if [ -f "$STATE/caddy.pid" ]; then
  caddy stop --config "$STATE/Caddyfile" || true
  rm -f "$STATE/caddy.pid"
fi
rm -f "$STATE/agent.ready"
```

停止后 `curl` 到 `127.0.0.1:18080` 和 `https://localhost:8443/healthz` 都应失败。`https://www.kan.chat/healthz` 不应被这次停止影响。状态目录里的 token、session key 和 `auth.json` 保留，供下次启动使用。

## 绝对不要从生产 Agent 主机复制

- Agent token、`agent-token`、`runtime.env` 或任何包含 `ARIEL_TOKEN` 的文件
- `ARIEL_SESSION_KEY`、Passkey setup token、`auth.json` 和其他 WebAuthn 凭据
- 生产 device id、launchd label、plist 及其环境变量
- `~/.codex`，包括 Codex 登录态、会话和 `ipc.sock`
- 生产 Caddy 数据、容器卷、部署口令或 SSH 凭据

本机 token 若泄露，只轮换 `"$STATE/agent-token"` 并重启本机 Relay 和本机 Agent。不要为此轮换公网 Relay 的 token。
