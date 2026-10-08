# 用 Docker 部署 Relay + Web

部署到单台 Linux ECS 时，优先使用[ECS 单机一键部署指南](ecs-single-node-deployment.md)；需要集群化交付时，使用[Kubernetes 部署指南](kubernetes-deployment.md)及仓库中的 Kustomize overlays。

Ariel 的镜像只包含 **Relay + Web**。Desktop Agent 仍要运行在能访问目标 Agent App 的桌面宿主机上，再主动连接容器里的 Relay；当前 Codex Adapter 依赖 macOS 本机能力，不能放进 Linux 容器。

## 适用拓扑

```text
手机或浏览器 ── HTTP / WebSocket ──> Docker: Relay + Web
                                             ↑
                                             │ WebSocket
                                             │
桌面宿主机: Desktop Agent ── 本机 Adapter ──> Agent App
```

Relay 不保存第二份会话正文。重启容器会断开当前浏览器 session 和 Desktop Agent 连接，客户端按现有协议重新连接并从 Agent App 恢复当前视图。

## 构建镜像

在仓库根目录执行：

```sh
docker build -t ariel-relay:local .
```

镜像构建分为 Web、Relay 和最小 runtime 三个 stage。最终镜像不包含 Node.js、Go toolchain、Desktop Agent、Codex Adapter、本机 `.local` 数据或开发依赖。

## 在可信局域网启动 Relay（PIN）

先在当前 shell 生成 Agent 长口令，并由你输入一个六位 Web 连接码。浏览器 Origin 必须和实际访问地址完全一致；如果需要接受多个入口，用英文逗号分隔。

```sh
export ARIEL_TOKEN="$(openssl rand -hex 32)"
read -r -p '请输入六位 Web 连接码：' ARIEL_WEB_PIN
export ARIEL_ORIGIN='http://<RELAY-LAN-IP>:8080'

docker run -d \
  --name ariel-relay \
  --restart unless-stopped \
  -p 8080:8080 \
  -e ARIEL_TOKEN \
  -e ARIEL_WEB_PIN \
  -e "ARIEL_ORIGINS=${ARIEL_ORIGIN}" \
  ariel-relay:local
```

`ARIEL_TOKEN`、`ARIEL_WEB_PIN` 和 `ARIEL_ORIGINS` 是 PIN 模式必填项。`ARIEL_WEB_AUTH` 未配置时默认使用 `pin`，兼容既有部署。镜像内已把 `ARIEL_LISTEN` 默认设为 `0.0.0.0:8080`，把 `ARIEL_WEB_DIST` 设为 production Web 目录；需要时仍可用 `-e` 覆盖。

不要把真实 token 或连接码写入 Dockerfile、镜像 build args、仓库文件或命令示例。上述 `-e NAME` 形式从当前 shell 传值，不会把值直接写入 shell history；但具有 Docker daemon 管理权限的同机用户仍可能检查容器环境变量。

## 在桌面宿主机连接 Desktop Agent

在运行目标 Agent App 的桌面宿主机克隆同一版本的 Ariel。如果 Relay 与 Desktop Agent 不在同一台机器，请通过受保护的渠道把同一个 Agent 长口令同步到桌面宿主机，并先在该终端设置 `ARIEL_TOKEN`；下面命令假定该变量已存在。随后把它写入临时的私有文件，再启动 host-side Desktop Agent：

```sh
ARIEL_TOKEN_FILE="$(mktemp)"
chmod 600 "$ARIEL_TOKEN_FILE"
printf '%s' "$ARIEL_TOKEN" > "$ARIEL_TOKEN_FILE"

./scripts/ariel.sh up agent \
  --relay-url 'ws://<RELAY-LAN-IP>:8080/ws' \
  --token-file "$ARIEL_TOKEN_FILE" \
  --device-id 'my-desktop' \
  --device-name '我的桌面设备' \
  --allow-insecure-ws

rm -f "$ARIEL_TOKEN_FILE"
unset ARIEL_TOKEN
```

`--allow-insecure-ws` 只适用于可信局域网内的明文 WebSocket。Desktop Agent 会把所需凭据复制到仓库私有的 `.local/runtime/`，因此临时文件可在启动成功后删除。

## 检查与停止

```sh
curl --fail "http://<RELAY-LAN-IP>:8080/healthz"
docker inspect --format '{{.State.Health.Status}}' ariel-relay
docker logs ariel-relay
docker stop ariel-relay
docker rm ariel-relay
```

`/healthz` 应返回 `ok`，容器随后进入 `healthy`。打开 `http://<RELAY-LAN-IP>:8080/`，输入启动时设置的六位连接码；Desktop Agent 握手成功后，设备与原始会话会出现在 Web 中。

## 网络与安全边界

- PIN 模式只在单用户、可信局域网内直接发布 8080 端口，**不要把 PIN 模式直接暴露到公网**。
- 公网部署在 Relay 前使用支持 WebSocket 的 TLS 反向代理，并按[公网 Passkey 部署指南](public-passkey-deployment.md)切换到 `ARIEL_WEB_AUTH=passkey`。
- 不要把 `ARIEL_ORIGINS=*` 当作简化配置；Relay 要求明确白名单。
- 容器只解决 Relay + Web 的交付，不改变当前 Adapter 的平台与版本兼容范围。
