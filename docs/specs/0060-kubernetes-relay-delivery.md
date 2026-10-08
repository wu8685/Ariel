# 0060：Kubernetes 快速交付 Relay + Web

- 状态：Implemented（契约测试、双 overlay 渲染、client dry-run、全量回归和受限容器 smoke 已验）。
- 范围：Kustomize base、可信局域网 PIN overlay、公网 Passkey overlay、Secret 注入、Ingress、持久卷、探针、安全上下文、部署文档与静态验收。
- 不涉及：把 Desktop Agent 放入集群、自动发布容器镜像、云厂商专属 LoadBalancer、自动签发 TLS 证书、Helm Chart、GitOps 控制器或多副本 Passkey 存储。

## 问题

Dockerfile 解决了镜像构建，但还没有把 Relay 的端口、健康检查、认证模式、Secret、Ingress 和 Passkey 凭据持久化固化为 Kubernetes 可交付单元。人工拼装这些对象容易把 PIN 暴露到公网，或让多个 Pod 并发写同一个 `auth.json`。

## 用户结果

用户可以选择 PIN 或 Passkey overlay，用标准 `kubectl apply -k` 部署 Ariel Relay + Web。两种 overlay 复用同一个安全 base，但只注入本模式需要的配置。Secret 不进入仓库；Desktop Agent 继续从桌面宿主机主动连接 Relay。

## 目录与输出

```text
deploy/kubernetes/
├── base/
│   ├── kustomization.yaml
│   ├── namespace.yaml
│   ├── deployment.yaml
│   └── service.yaml
└── overlays/
    ├── pin/
    │   ├── kustomization.yaml
    │   ├── configmap.yaml
    │   └── ingress.yaml
    └── passkey/
        ├── kustomization.yaml
        ├── configmap.yaml
        ├── deployment-patch.yaml
        ├── ingress.yaml
        └── pvc.yaml
```

详细操作写入 `docs/operations/kubernetes-deployment.md`，README 只增加入口。

## 输入

### 共同输入

- 已推送到目标集群可拉取 Registry 的 Ariel Relay 镜像；生产部署应使用不可变 tag 或 digest。
- Secret `ariel-relay-secrets` 中的高熵 `ARIEL_TOKEN`。
- 实际浏览器入口对应的精确 Origin。

### PIN overlay

- ConfigMap：`ARIEL_WEB_AUTH=pin`、可信局域网的 `ARIEL_ORIGINS`。
- Secret：`ARIEL_TOKEN`、恰好 6 位数字的 `ARIEL_WEB_PIN`。
- Ingress host 必须和 `ARIEL_ORIGINS` 一致；该 overlay 只能用于可信网络。

### Passkey overlay

- ConfigMap：`ARIEL_WEB_AUTH=passkey`、`ARIEL_ORIGINS`、`ARIEL_PUBLIC_ORIGIN`、`ARIEL_WEBAUTHN_RP_ID`、`ARIEL_WEBAUTHN_CREDENTIALS_FILE=/data/auth.json`。
- Secret：`ARIEL_TOKEN`、64 个十六进制字符的 `ARIEL_SESSION_KEY`；首次登记前还需要至少 32 字符的 `ARIEL_PASSKEY_SETUP_TOKEN`。
- TLS Secret `ariel-tls`，以及承载 `/data/auth.json` 的 PVC。

## 行为与不变量

1. base 只部署 Relay + Web；镜像内既有 `ARIEL_LISTEN=0.0.0.0:8080` 与 `ARIEL_WEB_DIST=/app/web/dist` 保持权威，不在 manifest 重复覆盖。
2. Service 仅在集群内暴露 HTTP 端口；外部访问统一经过 Ingress。公网 Passkey Ingress 必须启用 TLS，PIN Ingress 不得被描述为公网方案。
3. Deployment 固定单副本并使用 `Recreate`。Passkey overlay 不允许水平扩容，因为文件化凭据存储没有跨 Pod 并发协议。
4. Passkey overlay 使用 `ReadWriteOnce` PVC 挂载 `/data`。PIN overlay 不创建 PVC。
5. Pod 使用镜像内 UID/GID `10001`、`runAsNonRoot`、只读根文件系统、`RuntimeDefault` seccomp、drop all capabilities，并禁止自动挂载 ServiceAccount token。
6. startup、readiness、liveness 均复用 `/healthz`；Service targetPort 使用命名端口，避免端口配置漂移。
7. 配置通过非敏感 ConfigMap 和外部创建的 Secret 分离。仓库不得包含 `kind: Secret`、真实 token、PIN、session key、setup token 或 TLS 私钥。
8. WebSocket 与普通 HTTP 使用同一 Ingress host；文档说明 Ingress controller 必须支持 Upgrade 和长连接，并给出 NGINX Ingress 的超时配置。
9. `ARIEL_PASSKEY_SETUP_TOKEN` 在首个 Passkey 登记后从 Secret 删除并触发 Pod 重建；PVC 与 session key 必须保留。
10. Desktop Agent 不进入 Kubernetes Pod，仍通过 `ws://` 或 `wss://` 从目标 Agent App 所在桌面主机连接 Relay。

## 错误与恢复

1. 缺少 ConfigMap、Secret、PVC 或非法环境变量时，Pod 必须保持未就绪或退出，不得回退到另一认证模式。
2. `/healthz` 失败时 startup/readiness/liveness 按各自阈值阻止流量或重启容器。
3. Passkey PVC 丢失且无备份时，需要重新 bootstrap；不得伪造可恢复性。
4. 轮换 `ARIEL_SESSION_KEY` 会让全部浏览器 Session 失效；轮换 `ARIEL_TOKEN` 后必须同步更新 Desktop Agent。
5. Ingress host、Origin 与 RP ID 不一致时 Relay fail-fast；部署文档提供逐项核对方法。

## 验收

1. 先加入 manifest 契约测试并观察缺少 `deploy/kubernetes` 时失败，再实现资源使测试转绿。
2. `kubectl kustomize deploy/kubernetes/overlays/pin` 和 `.../passkey` 均能无警告渲染为合法 YAML。
3. PIN 渲染结果包含 Namespace、Deployment、Service、ConfigMap、Ingress，不包含 PVC、Passkey 配置或 Secret 对象。
4. Passkey 渲染结果额外包含 PVC、TLS Ingress 和 `/data` 挂载，且 Deployment 保持单副本与 `Recreate`。
5. 两种渲染结果都具备三个 HTTP probe、非 root / 只读根文件系统、安全上下文、资源 request / limit 和外部 Secret 引用。
6. 静态扫描确认 manifest 不包含 `kind: Secret` 或任何持久化 Secret 值。
7. 运行 Go 全量测试、`go vet`、Web 单测与 production build，确保部署交付不改变产品行为。

## 实现结果

1. `base` 提供受 Restricted Pod Security 约束的 Namespace、单副本 `Recreate` Deployment 和 ClusterIP Service；三个 probe 均复用 `/healthz`。
2. PIN overlay 仅加入可信局域网 ConfigMap 与 HTTP Ingress，不创建 PVC 或 Passkey 配置。
3. Passkey overlay 加入 HTTPS Ingress、`ReadWriteOnce` PVC 与 `/data` 挂载，凭据文件固定为 `/data/auth.json`。
4. 两种 overlay 均只引用外部 `ariel-relay-secrets`，仓库没有 Secret 对象或凭据模板；TLS Secret 同样由集群外部创建或 cert-manager 管理。
5. 部署指南覆盖镜像发布、域名替换、两种 Secret 创建、首次 Passkey 后移除 setup token、Desktop Agent 连接、诊断、备份与禁止扩容边界。
6. 详细证据见[0060 Kubernetes 交付验收记录](../testing/2026-10-08-0060-kubernetes-delivery.md)。
