# 0055：Relay + Web 容器镜像

- 状态：Implemented（契约测试、默认多阶段 build、隔离容器 smoke、全量 Go/Web 回归已验）。
- 范围：根目录 Dockerfile、Docker build context、容器部署文档、README 入口与自动验收。
- 不涉及：将 Desktop Agent 或当前 Codex Adapter 放入 Linux 容器、替代现有本机一键启动脚本、公开互联网部署方案。

## 问题

当前 Ariel 只能通过本机脚本构建和启动。Relay 与 Web 本身不依赖桌面 GUI，适合打包成一个可复现镜像；但 Desktop Agent 必须访问宿主机上的 Agent App、私有 IPC 与系统能力，若把整套本机进程误装进 Linux 容器，会得到可以构建却无法工作的部署方式。

## 用户结果

用户可以用标准 `docker build` 生成同时包含 Ariel Relay 与已构建 Web 的镜像，并把镜像运行在局域网内的容器主机上。桌面设备继续在宿主机运行 Desktop Agent，通过 WebSocket 主动连接容器中的 Relay。

## 输入与输出

1. 构建输入是仓库源码、Go module 依赖、Web package lock 与正式品牌静态资源。
2. 构建输出是单个 Linux 镜像，只包含 Relay 可执行文件、Web production assets 和最小运行时依赖。
3. 容器运行时接收 `ARIEL_TOKEN`、`ARIEL_WEB_PIN`、`ARIEL_ORIGINS`；`ARIEL_LISTEN` 与 `ARIEL_WEB_DIST` 由镜像提供容器内默认值，但仍允许显式覆盖。
4. 镜像暴露 TCP 8080，并复用 Relay 的 `/healthz` 作为容器健康检查。

## 行为与不变量

1. Dockerfile 使用独立 Web builder、Go builder 和 runtime stage；Web 必须通过 `npm ci` 与 production build 生成，Relay 必须从 `./cmd/relay` 构建。
2. runtime 以非 root 用户运行；不得包含 Desktop Agent、Codex 私有 Adapter、开发依赖、测试输出或本机 `.local` 状态。
3. token、六位连接码和 Origin 白名单只允许在运行时注入，不得写入镜像层、Dockerfile、仓库示例或构建参数。
4. `.dockerignore` 必须排除 Git 历史、本机运行目录、环境文件、`graphify-out/`、依赖目录、已有构建产物和测试报告。
5. Desktop Agent 始终运行在能够访问目标 Agent App 的桌面宿主机上；容器镜像不宣称提供当前 macOS + Codex Desktop Adapter。
6. Relay 仍是无业务数据库的有界内存中继。容器重启后 Web session 与在线连接按现有语义重建，不增加持久化卷要求。
7. HTTP/WS 只适合可信局域网；跨不可信网络时必须由外部反向代理提供 TLS，并配置精确 Origin，不能把容器端口裸露到公网。

## 错误处理

1. 缺少或传入非法运行配置时，Relay 沿用现有 fail-fast 行为并让容器退出。
2. Web production assets 缺失时，镜像构建失败；不得在运行时回退到开发服务器。
3. `/healthz` 不能返回 `ok` 时，容器健康状态必须转为 unhealthy。
4. Desktop Agent 未连接时，Web 可以加载并登录，但设备列表按现有协议显示离线；容器不得伪造 Agent ready。

## 验收

1. 契约测试先在 Dockerfile、`.dockerignore` 和容器文档缺失时失败，再由实现转绿。
2. `docker build` 在本机 Docker Engine 上成功完成，镜像不包含 `desktop-agent` 可执行文件。
3. 使用隔离的临时 token、PIN 与 Origin 启动容器；`/healthz` 返回 `ok`，根路径返回 Ariel production Web，容器健康状态变为 healthy。
4. 容器进程以非 root UID 运行，停止后不留下测试容器。
5. 运行 Go 全量测试、`go vet`、Web 全量单测与 production build。
6. README 保持精简，只新增容器部署文档入口；详细构建、运行、宿主机 Agent 接入与安全边界放在 `docs/operations/container-deployment.md`。

## 实现结果

1. 根目录 `Dockerfile` 使用 Web builder、Relay builder 与 Alpine runtime 三个 stage；默认 builder 安装 Node.js 24/npm，按 lockfile 构建 Web，并从 `./cmd/relay` 生成静态 Linux binary。
2. Web stage 显式复制 `protocol/v1.schema.json`，保证 `generate:protocol` 不依赖宿主机目录；该缺失曾被真实 Docker build 捕获，并已固化为契约回归。
3. runtime 只保留 Relay 与 `web/dist`，默认监听 `0.0.0.0:8080`，通过 `/healthz` 自检，并以 UID/GID 10001 的 `ariel` 用户运行。binary 与 Web 资产保持 root-owned，运行进程不可写。
4. `.dockerignore` 排除 Git、本机 `.local`、环境文件、`graphify-out/`、已有依赖与测试／构建输出，避免把本机状态或凭据送入 build context。
5. 新增容器部署指南，覆盖构建、运行时配置、host-side Desktop Agent 接入、检查／停止、可信局域网限制和 TLS 反向代理边界；README 只保留入口与支持矩阵更新。
6. 默认参数执行 `docker build` 成功；隔离容器中 production Web 与 `/healthz` 正常，健康状态为 `healthy`，进程 UID 为 10001，最终镜像不含 Node、Go 或 Desktop Agent。
7. 详细证据见[0055 容器镜像验收记录](../testing/2026-10-07-0055-containerized-relay-image.md)。
