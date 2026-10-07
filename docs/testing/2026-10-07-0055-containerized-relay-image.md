# 0055 Relay + Web 容器镜像验收

- 日期：2026-10-07
- 环境：macOS arm64、OrbStack Docker Engine 29.4.0、Linux arm64 image
- 结论：通过。默认 Dockerfile 可构建，最终容器只运行 Relay + production Web；Desktop Agent 保持在桌面宿主机。

## TDD 记录

1. 先新增 `cmd/relay/container_contract_test.go`，在 Dockerfile、`.dockerignore` 和部署文档不存在时得到预期失败。
2. 第一轮真实 Docker build 暴露 Web stage 缺少 `protocol/v1.schema.json`，导致 `generate:protocol` 失败；先新增失败断言，再显式复制 schema 后转绿。
3. 新增运行文件不得归 `ariel` 用户所有的失败断言，再改为 root-owned、非 root 只读运行。

## 构建验收

```text
docker build -t ariel-relay:test-0055-default .
Node.js v24.18.1
npm 11.12.1
Web: 402 modules transformed
Relay: CGO_ENABLED=0 GOOS=linux go build ./cmd/relay
结果：成功
```

首次拉取 Dockerfile frontend 与 Node 镜像时遇到 Docker Hub 临时 EOF。最终移除非必需的外部 Dockerfile frontend，并让默认 Web builder 复用 Go/Alpine 基础层安装 Node/npm；默认 build arguments 已完整构建成功。apk 与 npm 使用 BuildKit cache mount，后续同版本构建可复用下载缓存。

Vite 仍报告既有的单 chunk 大于 500 kB 警告，本次未改变前端打包边界；警告不影响镜像生成或运行。

## 隔离容器 smoke

使用随机临时 Agent token、随机六位 PIN、loopback Origin 与未占用端口运行最终默认镜像，没有连接真实 Desktop Agent 或修改真实会话。

| 检查 | 结果 |
| --- | --- |
| `GET /healthz` | `ok` |
| Docker health | `healthy` |
| 根页面 | production Web，标题为 `Ariel — Agent 联络中继器` |
| 运行 UID | `10001` |
| Relay binary | `root:root`、`0755`、运行用户不可写 |
| Web `index.html` | `root:root`、`0644`、运行用户不可写 |
| Node／Go toolchain | 最终镜像中不存在 |
| Desktop Agent 文件 | 最终镜像中不存在 |
| 临时容器 | 验收后已删除 |

本机 arm64 最终镜像约 18.5 MB。该数字只用于本次证据，不作为跨架构固定预算。

## 全量回归

- `GO111MODULE=on go test ./...`：通过。
- `GO111MODULE=on go vet ./...`：通过。
- `npm test -- --run`：20 个文件、145 个测试通过。
- `npm run build`：通过。
- 容器契约测试：通过。
- 默认 Docker build 与最终镜像 smoke：通过。

## 未覆盖边界

- 未在 CI 中发布多架构 manifest，也未推送任何 registry image。
- 未配置公网 TLS／反向代理；文档明确禁止把明文 HTTP/WS 端口直接暴露到公网。
- 未把 Desktop Agent 容器化；当前 Adapter 仍需在能访问 Agent App 的桌面宿主机运行。
