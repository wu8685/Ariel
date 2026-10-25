# 0105 工作区文件浏览器验收记录

- 日期：2026-10-25
- 规格：[0105 当前工作区文件浏览器](../specs/0105-workspace-file-browser.md)

## 自动化结果

- Ariel：`go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...` 通过。
- Web：31 个 test file、258 个测试通过；production build 通过。
- Chromium：`workspace-browser.spec.ts` 与 `mobile-file-context.spec.ts` 共 4 项通过。
- Chilling：全量 Go、race 与 vet 通过。

## 已验证行为

- capability 未协商时不显示入口；协商后只 lazy 读取当前目录。
- 手机端使用全屏页，桌面端使用不超过 360px 的右侧 drawer。
- `.git` 和 symlink 不可展开；路径、响应大小和文件大小均有硬限制。
- 工作区文件只有原文视图，可选中精确文本并引用到当前会话，不伪造 turn/item identity。
- 目录 cache 按 revision 区分并受 32 页／2 MiB LRU 限制；正文复用 10 文件／8 MiB LRU。
- 当前 turn 的 file change 会失效对应父目录与正文 cache。
- 原有 turn 修改文件、原文／Diff、message reference 与引用发送的移动端回归继续通过。

## 未覆盖边界

- Chromium 手机 viewport／touch 回归不能等同于物理 iPhone Safari；该项保留用户实机复验。
