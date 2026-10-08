# 在单台 ECS 上一键部署 Ariel

本指南用于把 **Relay + Web** 部署到一台 Linux ECS。Desktop Agent 仍运行在目标 Agent App 所在的桌面设备上，并主动连接云端 Relay。ECS 不需要访问桌面 App，也不保存第二份会话历史。

仓库提供 `scripts/deploy-ecs.sh`，负责镜像、凭据、Docker 容器、持久目录和可选的 Caddy HTTPS。脚本不会通过 SSH 操作远程机器；请先登录 ECS 并克隆仓库。

## 选择认证模式

| 场景 | 模式 | 对外入口 | 适用边界 |
| --- | --- | --- | --- |
| 私网、VPN、临时受限测试 | PIN | Relay 直接发布一个 HTTP 端口 | 安全组必须限制来源；**不要把 PIN 模式直接暴露到公网** |
| 公网长期访问 | Passkey | Caddy 发布 HTTPS / WSS | 推荐；需要域名、DNS 和 80 / 443 TCP、443 UDP |

两种模式都保持单用户语义：能登录的用户可操作所有会话。Agent 与 Relay 之间始终使用独立高熵 token；Passkey 只替代浏览器侧的低熵 PIN，不替代 Agent token。

## 前置条件

1. 一台能运行 Docker Engine 的 Linux ECS，建议至少 1 vCPU / 1 GiB 内存。
2. 已安装 Git；若 Docker 尚未安装，可以让脚本通过系统包管理器安装。
3. 公网 Passkey 模式还需要：
   - 一个已经把 A / AAAA 记录指向 ECS 的域名；
   - 安全组和主机防火墙放行 80 / 443 TCP；
   - 为 HTTP/3 可选放行 443 UDP；
   - 80 和 443 没有被其他进程占用。
4. PIN 模式只放行选定的 Relay 端口，并尽可能用安全组限制为你的固定 IP、私网或 VPN 网段。

## 获取代码

```sh
git clone https://github.com/wu8685/Ariel.git
cd Ariel
```

默认脚本会在 ECS 本机构建 `ariel-relay:ecs`。如果 Docker 尚未安装，使用 root 让脚本通过 `apt-get`、`dnf` 或 `yum` 安装；它不会执行远程 `curl | sh` 安装器：

```sh
sudo ./scripts/deploy-ecs.sh up \
  --install-docker \
  --mode passkey \
  --domain ariel.example.com
```

## 方案一：可信网络 PIN

假设 ECS 私网地址为 `10.0.0.8`，发布端口为 `8080`：

```sh
sudo ./scripts/deploy-ecs.sh up --mode pin \
  --origin http://10.0.0.8:8080 \
  --port 8080
```

启动成功后脚本会显示：

- 浏览器地址；
- 当前 6 位连接码；
- Desktop Agent 应连接的 `ws://.../ws` 地址；
- Agent token 的私有文件位置 `/opt/ariel/agent-token`。

重复运行同一条 `up` 或省略已经保存的参数，不会重新生成 token 和 PIN：

```sh
sudo ./scripts/deploy-ecs.sh up
sudo ./scripts/deploy-ecs.sh show
```

明文 WebSocket 只允许在可信网络使用。桌面主机上的当前 Adapter 需要显式确认这一点：

```sh
./scripts/ariel.sh up agent \
  --relay-url 'ws://10.0.0.8:8080/ws' \
  --token-file '/path/to/safely-copied-agent-token' \
  --device-id 'my-desktop' \
  --device-name '我的桌面设备' \
  --allow-insecure-ws
```

不要把 `/opt/ariel/runtime.env` 复制到桌面设备。只通过受保护渠道传递 `agent-token`，并在落地后把文件权限保持为 `0600`。

## 方案二：公网 Passkey + 自动 TLS

先确认 `ariel.example.com` 已解析到 ECS，然后执行：

```sh
sudo ./scripts/deploy-ecs.sh up --mode passkey \
  --domain ariel.example.com
```

脚本将：

1. 构建 Relay 镜像；
2. 生成 Agent token、64 位十六进制 session key 和首次登记 setup token；
3. 把 Relay 放到私有 Docker network，不映射 Relay 的 8080；
4. 把 `/opt/ariel/data` 挂载到容器 `/data`，长期保存 `/opt/ariel/data/auth.json`；
5. 启动 Caddy，发布 80 / 443 TCP 与 443 UDP，并自动申请 TLS 证书；
6. 输出浏览器地址、Agent WSS 地址和 setup token 文件的读取命令。

打开 `https://ariel.example.com`，读取 setup token 并完成第一个 Passkey 登记：

```sh
sudo cat /opt/ariel/passkey-setup-token
```

登记成功后立即关闭 bootstrap：

```sh
sudo ./scripts/deploy-ecs.sh disable-bootstrap
```

这个命令只有在 `/opt/ariel/data/auth.json` 已经存在时才执行；它删除 setup token、重写容器 env-file 并只重建 Relay，不会删除已有 Passkey 或轮换 session key。

桌面设备使用同一份 `/opt/ariel/agent-token` 连接 WSS：

```sh
./scripts/ariel.sh up agent \
  --relay-url 'wss://ariel.example.com/ws' \
  --token-file '/path/to/safely-copied-agent-token' \
  --device-id 'my-desktop' \
  --device-name '我的桌面设备'
```

Passkey 的详细安全边界见[公网 Passkey 部署](public-passkey-deployment.md)。

## 使用 Registry 镜像

不希望在 ECS 编译时，可以提前把 Relay 镜像推到 Registry：

```sh
sudo ./scripts/deploy-ecs.sh up --mode passkey \
  --domain ariel.example.com \
  --image ghcr.io/example/ariel-relay:v0.2.0
```

显式 `--image` 默认执行 `docker pull`。如果希望把该名称作为本机构建目标，同时传 `--build`。生产环境应使用**不可变 tag 或 digest**，并在升级前备份状态目录；Caddy 也可以通过 `--caddy-image` 固定到经过验证的不可变版本。

```sh
sudo ./scripts/deploy-ecs.sh up \
  --image 'ghcr.io/example/ariel-relay@sha256:<digest>'
```

脚本会保存镜像名和来源；后续直接执行 `up` 会沿用同一策略。传入新镜像名可升级容器，但不会改变认证模式、Origin 或凭据。

## 常用运维命令

```sh
# 查看容器状态
sudo ./scripts/deploy-ecs.sh status

# 再次显示地址、PIN 或凭据文件位置
sudo ./scripts/deploy-ecs.sh show

# 查看 Relay 日志
sudo ./scripts/deploy-ecs.sh logs --service relay

# 查看并持续跟随 Caddy 证书日志
sudo ./scripts/deploy-ecs.sh logs --service proxy --follow

# 停止并删除脚本管理的容器；保留状态和凭据
sudo ./scripts/deploy-ecs.sh down

# 用原状态重新拉起
sudo ./scripts/deploy-ecs.sh up
```

脚本只接管带有 `io.github.wu8685.ariel.managed=ecs` label 的固定容器和 network。发现同名但无 label 的资源时会退出，不会强制删除未知工作负载。

## 状态、备份与恢复

默认状态目录是 `/opt/ariel`，权限为 `0700`。Secret 文件和 Docker env-file 为 `0600`：

```text
/opt/ariel/
├── config.env
├── runtime.env
├── agent-token
├── web-pin                    # PIN 模式
├── session-key                # Passkey 模式
├── passkey-setup-token        # 首个 Passkey 登记后删除
├── data/auth.json             # Passkey 公钥凭据
├── Caddyfile
├── caddy-data/                # Caddy 证书与账户状态
└── caddy-config/
```

Passkey 模式备份时至少保留 `agent-token`、`session-key`、`data/auth.json`、`caddy-data/` 和 `config.env`。备份应加密，并限制为管理员可读。恢复时在新主机放回同一路径和权限，再运行 `up`。

- 丢失 `auth.json` 且没有备份，需要重新 bootstrap 和登记 Passkey。
- 轮换 `session-key` 会让全部浏览器 session 失效，但不会删除 Passkey。
- 轮换 `agent-token` 后，所有 Desktop Agent 都必须同步更新。
- `down` 不删除任何上述文件；若确实要销毁身份，请人工备份后针对这个明确目录处理。

## 自定义状态目录与凭据

可用 `--state-dir` 改到独立挂载的数据盘：

```sh
sudo ./scripts/deploy-ecs.sh up --state-dir /srv/ariel \
  --mode passkey --domain ariel.example.com
```

首次运行时也可以从环境变量注入你自己生成的值：

```sh
sudo env \
  ARIEL_TOKEN='<至少 32 字符的高熵值>' \
  ARIEL_SESSION_KEY='<64 个十六进制字符>' \
  ARIEL_PASSKEY_SETUP_TOKEN='<至少 32 字符的一次性值>' \
  ./scripts/deploy-ecs.sh up --mode passkey --domain ariel.example.com
```

这些环境变量只在对应文件尚不存在时使用。脚本不会把高熵秘密打印到 stdout，但具有 root 或 Docker daemon 权限的同机用户仍可能读取容器环境和状态文件。

## 排障

### Relay 健康，但网页打不开

Passkey 模式中，Relay `/healthz` 成功只证明应用已启动，不证明 DNS、ECS 安全组或 ACME 证书已经完成。依次检查：

```sh
sudo ./scripts/deploy-ecs.sh status
sudo ./scripts/deploy-ecs.sh logs --service proxy
dig +short ariel.example.com
curl --fail https://ariel.example.com/healthz
```

确认域名解析到正确公网 IP，80 / 443 TCP 可达，系统时间正确，且 Caddy 数据目录可写。443 UDP 仅影响 HTTP/3，不影响基础 HTTPS。

### Relay 容器反复退出

```sh
sudo ./scripts/deploy-ecs.sh logs --service relay
sudo ls -la /opt/ariel
```

常见原因包括 Origin / RP ID 不一致、session key 格式错误、`auth.json` 损坏，或数据目录权限被人工修改。Relay 对这些问题会 fail-fast，不会回退到 PIN。

### 端口被占用

```sh
sudo ss -lntup | grep -E ':(80|443|8080)\b'
```

Passkey 的公网自动 TLS 预期使用标准 80 / 443。`--http-port` 和 `--https-port` 主要用于受控测试或前面已有四层转发的场景；直接改成非标准公网端口通常会破坏 ACME 或访问 Origin，应优先清理冲突或使用已有的受信任反向代理。
