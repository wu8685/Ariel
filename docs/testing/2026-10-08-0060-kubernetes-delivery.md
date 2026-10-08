# 0060 Kubernetes 快速交付验收

- 日期：2026-10-08
- 规格：[0060 Kubernetes 快速交付 Relay + Web](../specs/0060-kubernetes-relay-delivery.md)
- 结论：PIN / Passkey overlays 均可渲染并通过 Kubernetes client-side 解析；安全上下文、模式隔离和 Passkey 持久化契约已验证。

## TDD 记录

先新增 `cmd/relay/kubernetes_contract_test.go`，覆盖：

- 单副本、`Recreate`、三个 `/healthz` probe 和非 root 安全上下文；
- ConfigMap / Secret 分离；
- PIN overlay 不含 PVC、Passkey 变量或 TLS；
- Passkey overlay 含 TLS Ingress、`ReadWriteOnce` PVC 与 `/data` 挂载；
- manifest 不得包含 Secret 对象或持久化 Secret 值；
- 运维文档必须覆盖两种认证模式、镜像、Desktop Agent 和禁止扩容边界。

Red 阶段五个测试均因 `deploy/kubernetes` 和 Kubernetes 运维文档不存在而失败。实现 base、两个 overlay 和文档后转为 Green。

## Kustomize 与 Kubernetes client 验证

```text
kubectl kustomize deploy/kubernetes/overlays/pin
PASS：Namespace / ConfigMap / Service / Deployment / Ingress

kubectl kustomize deploy/kubernetes/overlays/passkey
PASS：Namespace / ConfigMap / Service / PVC / Deployment / Ingress

kubectl create --dry-run=client --validate=false -f <rendered>
PASS：两套渲染结果全部可解析
```

结构断言结果：

- 两套 Deployment 都是 `replicas: 1`、`strategy: Recreate`、只读根文件系统、UID/GID 10001、禁用 ServiceAccount token；
- 两套结果都没有 `kind: Secret` 或 `stringData`；
- PIN 结果没有 PVC，仅挂载 16 MiB `/tmp`；
- Passkey 结果包含 `ariel-auth` PVC，并同时挂载 `/data` 与 `/tmp`；
- Passkey Ingress 使用 `ariel-tls`，host / Origin / RP ID 示例一致。

## 受限容器 smoke

重新构建 `ariel-relay:0060-test`，分别以接近 Pod SecurityContext 的参数启动 PIN 与 Passkey 容器：

- UID/GID `10001:10001`；
- read-only root filesystem；
- drop all capabilities；
- no-new-privileges；
- `/tmp` 使用 16 MiB tmpfs；
- Passkey 使用独立临时 `/data` volume。

两种模式的 `/healthz` 均成功；认证状态分别返回 `mode=pin` 与 `mode=passkey, enrollmentRequired=true`。测试容器、volume 和镜像已删除，没有接触用户数据。

## 全量回归

```text
GO111MODULE=on GOTOOLCHAIN=auto go test ./...
PASS

GO111MODULE=on GOTOOLCHAIN=auto go vet ./...
PASS

npm test
21 files / 159 tests passed

npm run build
PASS；保留既有 >500 kB chunk warning
```

## 证据边界

- 本次完成的是 Kustomize 渲染、Kubernetes client-side 解析和容器运行时验收，没有向用户的真实集群提交对象。
- 未验证特定 Ingress controller、cert-manager、StorageClass、云负载均衡或 Registry 权限；这些依赖必须在目标集群 smoke。
- 默认镜像名只是交付入口，实际部署前仍需推送可拉取的镜像并改为不可变 tag 或 digest。
