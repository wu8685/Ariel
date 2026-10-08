# 用 Kubernetes 部署 Relay + Web

Ariel 提供 Kustomize `base` 和两个 overlay：可信局域网使用 PIN，公网使用 Passkey。集群只运行 Relay + Web；Desktop Agent 仍运行在能够访问目标 Agent App 的桌面宿主机上。

## 交付结构

```text
deploy/kubernetes/
├── base/               # Namespace、单副本 Deployment、ClusterIP Service
└── overlays/
    ├── pin/            # 可信局域网 HTTP Ingress
    └── passkey/        # HTTPS Ingress、Passkey ConfigMap、PVC
```

两个 overlay 预置 `ghcr.io/wu8685/ariel:latest` 作为目标镜像名，但仓库不承诺该 tag 已公开发布。部署前必须先构建并推送镜像，再在对应 `kustomization.yaml` 的 `images` 段填写实际 Registry；正式环境使用不可变 tag 或 digest，不要依赖会漂移的 `latest`。私有 Registry 还需要给 Pod 配置 `imagePullSecrets`。

## 前置条件

- Kubernetes 集群和有权限的 `kubectl`；
- 支持 `networking.k8s.io/v1` 的 Ingress controller；示例按 NGINX Ingress 编写，其他 controller 需要替换 `ingressClassName` 与超时 annotation；
- 已构建并推送 Ariel 镜像；
- Passkey 模式需要可用的 HTTPS 域名、TLS Secret 和默认 StorageClass（或在 PVC 中显式指定）；
- Desktop Agent 所在机器能访问最终的 `/ws` 地址。

示例构建与推送：

```sh
export ARIEL_IMAGE='registry.example.com/ariel-relay:2026-10-08'
docker build -t "$ARIEL_IMAGE" .
docker push "$ARIEL_IMAGE"
```

随后把目标 overlay 的 `newName` 改为 `registry.example.com/ariel-relay`，把 `newTag` 改为 `2026-10-08`。也可以直接使用镜像 digest。

先渲染检查，不会修改集群：

```sh
kubectl kustomize deploy/kubernetes/overlays/pin
kubectl kustomize deploy/kubernetes/overlays/passkey
```

## 方式一：可信局域网 PIN

只在可信局域网使用。先把下面两个位置的 `ariel.lan` 改成实际内部域名，并保证 Origin 完全一致：

- `deploy/kubernetes/overlays/pin/configmap.yaml` 中的 `ARIEL_ORIGINS`；
- `deploy/kubernetes/overlays/pin/ingress.yaml` 中的 `spec.rules[].host`。

准备 Namespace 和私有 Secret：

```sh
kubectl apply -f deploy/kubernetes/base/namespace.yaml

export ARIEL_TOKEN="$(openssl rand -hex 32)"
read -r -p '请输入六位 Web 连接码：' ARIEL_WEB_PIN

secret_env="$(mktemp)"
chmod 600 "$secret_env"
printf 'ARIEL_TOKEN=%s\nARIEL_WEB_PIN=%s\n' "$ARIEL_TOKEN" "$ARIEL_WEB_PIN" > "$secret_env"
kubectl create secret generic ariel-relay-secrets \
  --namespace ariel \
  --from-env-file="$secret_env" \
  --dry-run=client -o yaml | kubectl apply -f -
rm -f "$secret_env"
unset ARIEL_WEB_PIN
```

应用 overlay：

```sh
kubectl apply -k deploy/kubernetes/overlays/pin
kubectl -n ariel rollout status deployment/ariel-relay
```

PIN overlay 没有 PVC，也没有 Passkey 变量或 TLS 配置。不要把它暴露到公网。

## 方式二：公网 Passkey

先把所有 `ariel.example.com` 替换为真实域名：

- ConfigMap 中的 `ARIEL_ORIGINS` 与 `ARIEL_PUBLIC_ORIGIN` 必须是相同的精确 `https://` Origin；
- `ARIEL_WEBAUTHN_RP_ID` 必须是该 host 或其合法父域，不包含 scheme 和端口；
- Ingress 的 rule host 与 TLS host 必须匹配该域名。

创建 Namespace、TLS Secret 和认证 Secret。TLS 也可以由 cert-manager 管理，但 Secret 名仍需与示例中的 `ariel-tls` 一致：

```sh
kubectl apply -f deploy/kubernetes/base/namespace.yaml
kubectl -n ariel create secret tls ariel-tls \
  --cert=/path/to/fullchain.pem \
  --key=/path/to/private-key.pem

export ARIEL_TOKEN="$(openssl rand -hex 32)"
export ARIEL_SESSION_KEY="$(openssl rand -hex 32)"
export ARIEL_PASSKEY_SETUP_TOKEN="$(openssl rand -base64 32 | tr -d '\n')"

secret_env="$(mktemp)"
chmod 600 "$secret_env"
printf 'ARIEL_TOKEN=%s\nARIEL_SESSION_KEY=%s\nARIEL_PASSKEY_SETUP_TOKEN=%s\n' \
  "$ARIEL_TOKEN" "$ARIEL_SESSION_KEY" "$ARIEL_PASSKEY_SETUP_TOKEN" > "$secret_env"
kubectl create secret generic ariel-relay-secrets \
  --namespace ariel \
  --from-env-file="$secret_env" \
  --dry-run=client -o yaml | kubectl apply -f -
rm -f "$secret_env"
unset ARIEL_PASSKEY_SETUP_TOKEN
```

应用 overlay：

```sh
kubectl apply -k deploy/kubernetes/overlays/passkey
kubectl -n ariel rollout status deployment/ariel-relay
```

打开公共 HTTPS 地址，使用 setup token 登记第一个 Passkey。成功后，从 Secret 删除 bootstrap 能力并重建 Pod：

```sh
kubectl -n ariel patch secret ariel-relay-secrets \
  --type=json \
  -p='[{"op":"remove","path":"/data/ARIEL_PASSKEY_SETUP_TOKEN"}]'
kubectl -n ariel rollout restart deployment/ariel-relay
kubectl -n ariel rollout status deployment/ariel-relay
```

不要删除 PVC 中的 `/data/auth.json`，也不要在移除 setup token 时轮换 `ARIEL_SESSION_KEY`。

## 连接 Desktop Agent

把 Secret 中同一个高熵 `ARIEL_TOKEN` 安全提供给桌面宿主机，再启动 Desktop Agent：

```sh
./scripts/ariel.sh up agent \
  --relay-url 'wss://ariel.example.com/ws' \
  --token-file /path/to/private-agent-token \
  --device-id 'my-desktop' \
  --device-name '我的桌面设备'
```

PIN 模式在可信网络可以使用 `ws://.../ws`，但必须显式添加 `--allow-insecure-ws`。公网不得使用明文 WebSocket。

## 检查与排障

```sh
kubectl -n ariel get deployment,pod,service,ingress,pvc
kubectl -n ariel logs deployment/ariel-relay
kubectl -n ariel describe pod -l app.kubernetes.io/name=ariel
kubectl -n ariel port-forward service/ariel-relay 8080:80
curl --fail http://127.0.0.1:8080/healthz
```

- Pod 启动即退出：检查 Secret 是否包含当前认证模式所需的全部键，以及 ConfigMap 的 Origin / RP ID 是否一致。
- Ingress 页面可打开但 WebSocket 断开：确认 controller 支持 Upgrade，并保留示例的长连接超时或采用等效配置。
- Passkey 文件权限错误：恢复文件时确保它由 UID/GID `10001` 访问，且 group / other 没有权限。
- PVC Pending：指定集群可用的 StorageClass。

## 更新、备份与边界

- 更新镜像 tag 后执行 `kubectl apply -k ...`。Deployment 使用 `Recreate`，避免两个 Pod 同时写凭据文件，因此更新时会有短暂中断。
- Passkey overlay 固定 `replicas: 1`，**不要扩容**。需要高可用时，先把 credential store 迁移到具备并发语义的持久层并重新设计 Session 撤销。
- 备份 PVC 中的 `/data/auth.json` 与集群外保存的 `ARIEL_SESSION_KEY`；两者都按认证材料保护。
- 删除 Namespace 会同时删除工作负载；PVC 是否随之删除取决于存储类与 PV reclaim policy，操作前先确认备份。
- Kubernetes manifest 不创建 Secret 对象，仓库中也不保存 token、PIN、session key、setup token 或 TLS 私钥。
