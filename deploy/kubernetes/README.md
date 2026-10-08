# Ariel Kubernetes manifests

- `base/`：Relay + Web 的单副本安全运行基线。
- `overlays/pin/`：仅供可信局域网使用的 PIN 示例。
- `overlays/passkey/`：带 TLS Ingress 和凭据 PVC 的公网 Passkey 示例。

部署前必须替换镜像与示例域名，并在集群外创建 Secret。完整步骤、安全边界和排障方法见 [`docs/operations/kubernetes-deployment.md`](../../docs/operations/kubernetes-deployment.md)。
