# 0061 ECS 单机一键部署验收记录

- 日期：2026-10-08
- 范围：`scripts/deploy-ecs.sh`、ECS 运维文档、PIN / Passkey 容器拓扑、状态持久化与回归。
- 结论：仓库内实现与本机 Docker smoke 通过；真实 Linux ECS 包管理器安装、公共 DNS / ACME 和 ECS 安全组仍需在目标主机验收。

## TDD 证据

先添加 `cmd/relay/ecs_script_contract_test.go` 和 Approved spec，再运行：

```text
GO111MODULE=on go test ./cmd/relay -run 'TestECSScript' -count=1
```

首次结果按预期失败：`scripts/deploy-ecs.sh` 和 `docs/operations/ecs-single-node-deployment.md` 尚不存在。实现脚本与文档后，同一测试转绿。

在 HTTPS 反代 smoke 中，已健康的 Relay 因脚本无条件执行 `docker pull caddy:2-alpine`，遇到 Registry `EOF` 而中断。随后新增“本地 Caddy 镜像存在则复用”的失败契约，并修复后重新通过完整 HTTPS 反代验收。

## 静态与参数验收

通过：

```text
bash -n scripts/deploy-ecs.sh
GO111MODULE=on go test ./cmd/relay -run 'TestECSScript' -count=1
git diff --check
```

覆盖点：

- `--help` 在 PATH 不含 Docker 时仍成功；
- `/` 被拒绝作为 `--state-dir`；
- 非法 domain 和 Origin / port 不一致会在创建容器前失败；
- 脚本不含 `curl | sh`、硬编码凭据、状态目录递归删除或命令行明文 `-e ARIEL_*=...`；
- 状态目录为 `0700`，`config.env`、`runtime.env`、Agent token 与 PIN 为 `0600`；
- ShellCheck 在当前环境未安装，因此本次只完成 `bash -n` 与真实 Bash 执行验证。

## PIN 真实 Docker smoke

使用临时目录 `/tmp/ariel-ecs-pin.*`、宿主端口 `18080` 和临时镜像完成：

1. 从当前仓库构建 Relay + Web 镜像；
2. `up --mode pin --origin http://127.0.0.1:18080`；
3. 容器内和宿主机 `/healthz` 均返回 `ok`；
4. `status`、`show`、`logs` 均可用；
5. 第二次 `up` 前后 PIN 完全一致，Agent token 的 SHA-256 完全一致；
6. `down` 删除容器后，Agent token 和 PIN 文件仍存在；
7. 测试容器、镜像和临时目录已清理。

首次 smoke 的验收辅助命令使用了 Linux 专属 `sha256sum`，在 macOS 上中断；容器已经健康，状态目录和凭据没有丢失。改用系统自带 `shasum -a 256` 后从同一状态目录接续并通过，间接验证了部署过程可从验收中断恢复。

## Passkey + Caddy 真实 Docker smoke

使用 `ariel.localhost`、高位宿主端口和临时状态目录完成：

- Relay、Caddy 与 `ariel-ecs` network 都带 `io.github.wu8685.ariel.managed=ecs` label；
- Relay 没有任何宿主机 published port；
- Relay 与 Caddy 都加入私有 network；
- Relay 的 `/data` 是可写 bind mount，容器根文件系统保持只读；
- 容器内 `/healthz` 返回 `ok`；
- Caddy 使用本地证书后，`https://ariel.localhost:<test-port>/healthz` 经真实 TLS 反代返回 `ok`；
- 首个 Passkey 尚未登记时，`disable-bootstrap` 明确拒绝，setup token 文件保持存在；
- `down` 后 Agent token、session key 和 setup token 保持存在；
- 测试容器、network、临时镜像和临时目录已清理。

该 smoke 只验证单机拓扑、TLS 终止和 Relay 配置。真实公网域名的 DNS、ACME 证书、ECS 安全组，以及登记成功后的 `disable-bootstrap` 成功路径，需要在目标 ECS 上结合真实浏览器完成；Passkey ceremony 本身已由 0059 的 Chromium virtual authenticator E2E 覆盖。

## 全量回归

通过：

```text
GO111MODULE=on go test ./...
GO111MODULE=on go vet ./...
cd web && npm test
cd web && npm run build
```

结果：

- Go 全包测试通过；
- `go vet` 通过；
- Web 21 个测试文件、159 项测试全部通过；
- TypeScript 与 Vite production build 通过；
- Vite 仍报告既有的单 chunk 超过 500 kB 警告，本次部署脚本没有新增前端 bundle 内容。
