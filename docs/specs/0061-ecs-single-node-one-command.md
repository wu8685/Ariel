# 0061：ECS 单机一键部署 Relay + Web

- 状态：Implemented（契约、参数校验、双模式真实 Docker、持久化、HTTPS 反代与全量回归已验）。
- 范围：单机部署脚本、PIN / Passkey 两种拓扑、Docker 安装前置检查、凭据生成与持久化、Caddy 自动 TLS、状态查看、日志、停机、bootstrap 收口、部署文档与自动验收。
- 不涉及：在 ECS 上运行 Desktop Agent、云厂商专属 API、自动购买域名或修改 DNS、安全组自动放行、多机高可用、数据库、OIDC、证书迁移或远程 SSH 执行。

## 问题

Dockerfile 和 Kubernetes manifests 已经覆盖镜像与集群部署，但单台 ECS 仍需要人工生成多组凭据、拼装 `docker run`、配置 HTTPS 反向代理、挂载 Passkey 凭据目录，并把 Agent token 安全转交给桌面主机。该过程容易误把 PIN 暴露到公网、在重启时重新生成凭据，或丢失 `/data/auth.json`。

## 用户结果

仓库拉取到 Linux ECS 后，用户运行一个脚本即可拉起 Relay + Web：

- `pin` 模式直接发布指定端口，只用于可信私网或受限测试环境；
- `passkey` 模式由 Caddy 发布 80 / 443 并自动申请 TLS，Relay 只加入内部 Docker network；
- 首次运行自动生成 Agent token、PIN 或 Passkey session / setup token；重复运行复用原值；
- 脚本给出浏览器地址、健康状态和私有凭据文件路径，但不把高熵秘密直接打印到日志；
- 停止或重建容器不删除认证文件；首个 Passkey 登记后可用命令移除 setup token 并重建 Relay。

## 命令契约

入口为 `scripts/deploy-ecs.sh`：

```text
deploy-ecs.sh up --mode pin --origin http://10.0.0.8:8080
deploy-ecs.sh up --mode passkey --domain ariel.example.com
deploy-ecs.sh status
deploy-ecs.sh logs [--service relay|proxy]
deploy-ecs.sh show
deploy-ecs.sh disable-bootstrap
deploy-ecs.sh down
```

共同选项：

- `--state-dir`：绝对状态目录，默认 `/opt/ariel`；不得为 `/`。
- `--image`：Relay 镜像名。默认在仓库内构建 `ariel-relay:ecs`；显式指定远端镜像时先拉取。
- `--build`：即使指定 `--image` 也从当前仓库构建。
- `--install-docker`：Docker 缺失时仅通过受支持的系统包管理器安装并启动；不得执行未经固定来源审计的 `curl | sh`。

首次 `up` 必须显式选择模式并提供实际 Origin / domain。重复 `up` 可读取状态目录中的非敏感配置；认证模式或公共 Origin 发生变化时 fail-fast，不得静默迁移已有 Passkey 身份。

## 状态与秘密

默认状态目录内至少包含：

```text
/opt/ariel/
├── config.env                 # 非敏感部署配置
├── runtime.env                # Docker env-file，0600
├── agent-token                # Desktop Agent 使用，0600
├── web-pin                    # 仅 PIN 模式，0600
├── session-key                # 仅 Passkey 模式，0600
├── passkey-setup-token        # 首次登记前存在，0600
├── data/                      # Relay /data，包含 auth.json
├── Caddyfile                  # Passkey 模式
├── caddy-data/                # Caddy 证书状态
└── caddy-config/
```

1. 所有秘密文件和 `runtime.env` 使用 `0600`，状态目录使用 `0700`；写入必须先落临时文件再原子 rename。
2. `ARIEL_TOKEN`、`ARIEL_WEB_PIN`、`ARIEL_SESSION_KEY`、`ARIEL_PASSKEY_SETUP_TOKEN` 可以在首次运行前由同名环境变量提供；缺省值从 `/dev/urandom` 生成。
3. 脚本不在 stdout、shell command line、仓库或 Docker build args 中展开秘密；容器统一用 `--env-file` 注入。
4. `down` 只删除脚本管理的容器和空 network，不删除状态目录、凭据或 Caddy 数据。
5. 已有同名容器或 network 不带 Ariel 管理 label 时脚本必须拒绝接管。

## 运行拓扑

### PIN

```text
Browser ── http://ECS:8080 ──> ariel-relay
Desktop Agent ── ws://ECS:8080/ws ──┘
```

- Relay 映射 `0.0.0.0:<port>:8080`。
- 仅允许精确 `ARIEL_ORIGINS`；文档明确禁止将此模式直接暴露公网。
- Desktop Agent 使用 `agent-token` 中的高熵 token；明文 WS 只允许可信网络并显式传 `--allow-insecure-ws`。

### Passkey

```text
Browser / Desktop Agent ── HTTPS / WSS ──> Caddy :443 ── private network ──> ariel-relay :8080
```

- domain 必须是无 scheme、path、port 的 DNS host；公共 Origin 固定为 `https://<domain>`，RP ID 默认等于 domain。
- Caddy 同时发布 TCP 80 / 443 与 UDP 443，自动申请和续期证书；使用持久化数据目录。
- Relay 不映射宿主机端口，`/data` 挂载到持久化目录；`auth.json` 在容器重建后保留。
- `disable-bootstrap` 删除私有 setup token 文件、重写 env-file 并仅重建 Relay；该操作不会删除已有 Passkey。

## 容器安全与生命周期

1. Relay 固定为单实例，使用 `unless-stopped`、UID/GID 10001、只读根文件系统、`tmpfs /tmp`、drop all capabilities 和 `no-new-privileges`。
2. Passkey Relay 和 Caddy 进入独立 bridge network。Caddy 只挂载只读 `Caddyfile` 与所需持久目录。
3. 容器、network 均带 `io.github.wu8685.ariel.managed=ecs` label；删除或替换前先核验 label。
4. `up` 在成功前轮询容器内 `/healthz`。失败时保留容器并给出日志命令，不谎报可用。
5. Relay 镜像可本机构建，也可从 Registry 拉取。生产环境推荐传不可变 tag 或 digest。

## ECS 前置条件

- 支持 Docker Engine 的常见 Linux 发行版；脚本在 Debian / Ubuntu 使用 `apt-get`，在 Fedora / RHEL 系使用 `dnf` 或 `yum`。
- Passkey 模式需要域名 A / AAAA 记录已经指向 ECS，安全组和主机防火墙放行 80 / 443 TCP 与 443 UDP。
- PIN 模式只按需要放行配置的端口，并用安全组限制来源。
- 自动 TLS 的外部 DNS / ACME 成功不能由本机 Relay 健康检查替代；脚本应分别报告 Relay 健康和公网证书仍可能等待签发。

## 错误与恢复

1. 非法 Origin、domain、端口、模式、秘密格式或状态目录立即失败，不创建半配置容器。
2. Docker 缺失且未传 `--install-docker` 时给出明确安装提示；安装选项需要 root。
3. Passkey `auth.json` 丢失且无备份时需要重新登记；脚本不得伪造恢复。
4. 轮换 Agent token 后需要同步更新 Desktop Agent；轮换 session key 会让全部浏览器 session 失效。
5. Caddy 证书申请失败不应让内部 Relay 健康状态变成成功的公网部署；文档提供 DNS、安全组、Caddy 日志检查。

## 验收

1. 先添加契约测试，证明脚本和文档缺失时失败，再实现使测试转绿。
2. `bash -n scripts/deploy-ecs.sh` 通过；若环境安装了 ShellCheck，运行静态检查。
3. `--help` 不需要 Docker 即可成功，并列出全部命令、模式和安全边界。
4. 使用临时状态目录完成 PIN 模式真实 Docker smoke：构建镜像、启动、健康检查、重复 `up` 复用凭据、`status` / `show` / `logs` 可用、`down` 后凭据仍在。
5. Passkey 模式至少完成脚本生成配置、容器拓扑与 Relay `/healthz` smoke；DNS / ACME 作为外部依赖单独说明。
6. 静态测试确认脚本不含 `curl | sh`、硬编码凭据、删除状态目录、用命令行 `-e ARIEL_*=` 展开秘密或未核验 label 的强制删除。
7. 运行 Go 全量测试、`go vet`、Web 单测与 production build，确保交付脚本不改变产品行为。

## 实现结果

1. `scripts/deploy-ecs.sh` 提供 `up`、`status`、`show`、`logs`、`disable-bootstrap` 和 `down`，首次部署自动生成凭据，重复部署读取同一状态目录。
2. PIN 模式只发布 Relay 端口；Passkey 模式把 Relay 隐藏在带管理 label 的 bridge network 后，由 Caddy 发布 HTTPS / WSS 和持久化证书状态。
3. 状态目录、Secret 文件、`runtime.env`、Caddyfile 与 `/data/auth.json` 均有明确权限和生命周期；`down` 不删除任何身份数据。
4. Relay 以 UID/GID 10001、只读根文件系统、drop all capabilities 和 `no-new-privileges` 运行；容器和 network 在接管或删除前核验管理 label。
5. 真实 smoke 证明 PIN 的 PIN / Agent token 在重复 `up` 和 `down` 后不变；Passkey Relay 不发布 8080、挂载可写 `/data`、Caddy HTTPS 能反向代理 `/healthz`，未登记时不能误关 bootstrap。
6. Caddy 镜像改为本地存在时复用、缺失时才拉取，避免 ECS 重启因 Registry 短时不可用而失败。
7. 详细证据见[0061 ECS 单机一键部署验收记录](../testing/2026-10-08-0061-ecs-single-node-deployment.md)。
