# 2026-10-27：0119 容器交付回归

## 发布前发现

- PR #29 合并为 `d098de5`，README 先于 v0.11.0 tag 更新；该 tag 只推送代码，未创建正式 Release、未替换本机 Agent／公网 v0.10.0 容器。
- 真实 Linux Docker build 报 `no required module provides package .../internal/filecontext`。原因是 Relay 已复用文件开关解析，但 Docker Relay build stage 的显式 COPY 没有跟随新本仓库依赖。
- Issue #33／后续 PR #34 修复交付遗漏；不覆盖 v0.11.0 tag，验证后改发 v0.11.1。原服务保持运行，失败构建没有造成停机。

## Red → Green

1. 补充 0119 已批准的构建交付要求；新增 `TestRelayContainerCopiesEveryLocalDependency`，用 `go list -deps` 的本仓库依赖对照 Relay stage 的路径保留式 COPY。
2. 实际 red：缺少 `internal/filecontext`，测试失败。
3. 仅补一条该 package 的 COPY；Go 全量、vet 及 cmd Relay race 通过。runtime 仍只复制 Relay 二进制和 Web dist，Docker context 继续排除本机状态与凭据，不引入 Desktop Agent 或 Codex。
4. Web 实现未变，保留此前 314 单测／90 Chromium／production build 通过的证据；生产 Linux Docker candidate build 与最终部署结果在 GitHub Release 中核验记录。

## Native 与边界

- 当前安装版本的隔离 native metadata／目标 turn items／workspace read／exact prepare 再次通过；manifest 必须使用绝对路径，不能以 package 测试工作目录下找不到相对 manifest 的失败推断兼容性退化。
- 真实 Agent＋Relay、无 Chilling 的文件与图片 fixture 再次通过，相关 race 通过。
- native seed 没有 fileChange；新增 native fileChange／Diff／message-reference 全链与物理 iPhone／Safari 仍未验证，不把 mock 或浏览器 fixture 当作实机支持证明。
- 部署保留 Passkey／Agent 凭据和身份、1 GiB 容器硬限制与只读 rootfs、任务关闭和旧容器／plist；只迁移既有目录授权并开启已批准文档图片，不停止或重启 Chilling。
