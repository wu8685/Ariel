# 2026-10-27：内置文件上下文、Markdown 阅读与紧凑排队项

## 范围与交付状态

- Issue #28；用户批准 spec 0117／0118／0119 后按 TDD 实施。
- 文件 changes／read／reference／workspace／prepare 迁入 Ariel `internal/filecontext`，通过已有只读 Codex Adapter 获取来源。核心文件链路不创建 Chilling client、HTTP 或 socket，不增加 follower／mutation owner。
- Web 增加原版／阅读切换和受控 lazy 图片，保留 Diff、内容选区、草稿与返回。排队项正常高度缩至 44–45px，实际操作目标高 44px。
- 本记录是开发验收，不是上线记录。没有重启本机 launchd Agent、替换公网容器、停止 Chilling 或启用任务看板；0106 Draft 不纳入本次改动。

## Red → Green 与复查

| 行为 | 实际 red | 修复与覆盖 |
| --- | --- | --- |
| 紧凑队列 | theme 仍为 6px 上下 padding；手机实测 53px、桌面 49px，非目标 44–45px | 去除纵向 padding，手柄及操作盒均为 44px；320／390／1280px 几何、截图、拖拽和禁止误选 |
| 原版／阅读 | 初始没有阅读入口，Markdown 标题／格式化选区测试失败 | 独立 Markdown/GFM 组件，默认原版；AST offset 映射到连续原文，重复文字不靠搜索定位 |
| 无 Chilling 文件链路 | 没有内置 package／来源接口，Agent 无 endpoint 时无法提供文件能力 | 迁移原 backend 行为／测试，真实临时文件＋fake native 来源；实际 Agent＋Relay WebSocket fixture 覆盖 changes→read→image |
| 中性配置 | 启动脚本拒绝 `--enable-file-context`，新字段未持久化或传给子进程 | 中性 flag、旧名 alias、显式 false／冲突、保存配置、独立 workspace／asset 权限；任务状态不变 |
| 本地图片与旧端 | 初始资源方法／schema／能力和图片 session 不存在，相关 contract／UT 失败 | response.data 双端协商，实际父文档 AST 授权、SHA、顺序分块、digest、局部失败；旧端零未知 asset 请求 |
| 浏览器滚动 | 桌面阅读位置从 450px 被图片锚定移至 511px | 独立位置记录，阅读容器关闭 overflow anchoring；切换原文／阅读／Diff 不额外读文本 |
| 关闭后的迟到读取 | 已回文件树后迟到响应重新打开文件，UT 失败 | 目录／文件 generation fencing，关闭后不写旧 cache／state，相对链接失败保留原文件 |
| Agent transport 重连 | 新 connection 没有旧图片 capability receipt，资源 PROTOCOL_UNSUPPORTED 后图像缺失 | 仅对同 epoch 父文件做 single-flight 能力／SHA 复验，再重试图片一次，不重订阅／刷新会话 |
| StrictMode 目录 | Chromium 预期一次初始列表实际两次；独立 UT 同样复现 | 复用同一 pending Promise，结果仍按各调用 generation 隔离，不用放宽次数断言 |
| StrictMode 缓存文件 | 缓存文件初次挂载的 image session 被 effect replay 提前 dispose，UT 无法找到图片 | session 在 effect 中建立与销毁；identity 不匹配时不沿用旧资源；补缓存文件重开浏览器回归 |
| Git metadata | `.GIT/config` 被接受，单测失败 | 大小写别名一致禁读；目录仍可展示但不能打开，覆盖 macOS 大小写不敏感文件系统 |
| Relay 资源校验 | base64 相同编码长度可携带 256 KiB＋1 字节；非法 cursor 也通过，两个 UT red | 解码后再次核对原始字节上限，严格校验 opaque cursor，超限不转发 |

先前既有安全和业务行为由迁移测试保留：三类来源 union、真正 fileChange／agentMessage 证明、路径／cwd／SHA／mtime／精确文本复验、canonical envelope、不可读／删除文件、截断 Diff、不自动发送或重放。

## 资源与安全验收

- native metadata 明确 `includeTurns=false`；只读目标 turn items，100／页、最多 32 页／8 MiB；legacy item 和 timeline wrapper 均校验 thread／turn，不接收重复 cursor 或不完整 shape。
- 文件只读通过 no-follow 描述符逐层打开；祖先 symlink、FIFO、越界和 `.git` 拒绝，1 GiB sparse 文件从元信息即拒绝。文本上限 1 MiB，有界读取至上限＋1；目录单层最多扫描 10,000 项。
- 文件工作 2 并发＋8 等待、15 秒预算；取消释放 admission／slot，不持有 Service mutation 锁。目录空页、页响应 256 KiB、changes 合计与文件序列化帧预算均有测试。
- 图片仅认可真实 Markdown inline／reference image，不认可普通链接和 code fence；按父目录解析、仍受 workspace 根与 symlink 约束。PNG／JPEG／GIF decoder、非 raster／SVG／HTML／坏头、超过 8192px、SHA／cursor／TTL／32 lease 容量测试通过；WebP 使用标准 decoder，未补物理设备解码验收。
- 图片单图 4 MiB／12 MP，原始 chunk ≤256 KiB，帧 ≤384 KiB；lease 30 秒／16 MiB／32 项，续读固定同一份字节，不逐块重读。过期释放 body／timer，浏览器关闭后取消后续 chunk 并释放 Blob。
- Web 只允许当前文档显式授权的 HTTPS 外图，不由 Agent／Relay 代理；原始 HTML／JSX／SVG 不执行，已知内网和危险协议拒绝。没有允许外域时零外部图片请求。
- 保留旧 hello／device 的严格字段；协商不向旧端加新 capability key，Agent epoch 变更与旧 cursor 不能跨身份复用。真实 task client 既有测试继续通过。

## 当前真实 Codex gate

- 本机 Codex Desktop `26.930.51102`，使用其 bundled CLI `0.160.0`；不使用 PATH 中其他 CLI。
- 新建受 manifest guard 限制的隔离 seed fixture；gate 仅查询 metadata 和目标 turn items，并在该 fixture workspace 临时创建／删除测试文本，验证 workspace read＋精确 prepare。没有向真实工作会话发送、停止、编辑或审批。
- `TestFileContextInstalledNativeGate` 通过：metadata=true、targetTurnItems=true、agentMessages=1、fileChangeItems=0、boundedWorkspaceRead=true、exactPrepare=true、chillingRequests=0、nativeMutations=0。
- **未验边界**：本次 seed 没有 fileChange，因此当前版本原生 fileChange／Diff／message-reference 全链没有新增实机证据；这些保留已批准行为并由来源 UT、真实文件与 Agent／Relay fixture 回归，不把 mock 当 native supported。probe 对高于既有 profile 基线的 Desktop 仍可报告 unverified，不代表所有私有接口已认证。

可重复执行（manifest 为本地私有 fixture，不提交其 ID／路径内容）：

```sh
ARIEL_TEST_FILE_CONTEXT_NATIVE=1 ARIEL_TEST_MANIFEST='<verified-manifest>' \
  go test ./internal/desktopagent -run '^TestFileContextInstalledNativeGate$' -v -count=1
```

## 最终验证

| 验证 | 结果 |
| --- | --- |
| Go 全量／race／vet | `go test ./...`、`go test -race ./...`、`go vet ./...` 通过；补充 asset／Git／Relay guard 后相关 race 再次通过 |
| Web unit | 40 文件、312 项通过 |
| Production build | 通过；既有 Mermaid 等 chunk-size 警告为非阻断项 |
| Chromium viewport／touch | 全量 84 项通过，包含缓存文档重开与图片 session 回归 |
| Native gate | 上述隔离只读范围通过，具体未验项不隐去 |
| 物理 iPhone／Safari | 未验证，需要用户复验选区、图片、滚动与长按 |

浏览器使用隔离 WebSocket fixture，320／390／1280px 覆盖 turn 文件卡、消息引用和 workspace 三入口、原文／阅读／Diff、精确选区、草稿、lazy 请求数、外图许可、长表格局部滚动和不撑宽。不是物理 iPhone／Safari 验收。

新增 6 张截图（3 种宽度的紧凑队列＋阅读页），仅更新 5 张受预期队列密度／Markdown 模式按钮影响的既有截图；逐项检查变化，不批量接受未知差异。既有发送、历史分页、5 秒 streaming／即时终态、登录恢复、无看板、队列和模型／权限测试继续保留。

## 发布核对

发布须同时升级 Agent 与 Relay/Web，确认原 workspace 能力授权再迁移开关，不能从安装过 Chilling 推断目录权限。默认不开文件、目录、图片或任务功能；保留旧 alias 和配置、设备身份、凭据、任务关闭状态及回滚版本，不为验收停止 Chilling。线上部署与物理手机验收须另记实际结果。
