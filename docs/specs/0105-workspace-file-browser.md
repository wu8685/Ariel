# 0105：当前工作区文件浏览器

> 后端归属已由 [0119](0119-self-contained-desktop-file-context.md) 取代：目录与文件读取内置到 Ariel Desktop Agent，不再依赖 Chilling。以下保留原设计，显式 workspace 授权和分页限制继续有效。

- 状态：Implemented（用户于 2026-10-25 确认；自动化验收已通过，物理 iPhone／Safari 待用户复验）
- 日期：2026-10-25
- 关联：[0089 文件上下文前端与可选接入](0089-turn-file-links-and-selection-context.md)、[0090 文件查看器导航与片段引用](0090-file-viewer-wrap-and-text-selection.md)、Chilling `0004-workspace-file-browser-backend.md`

## 1. 背景与范围变化

Ariel 目前只能打开 Codex turn 明确修改或 assistant message 明确引用的文件；无法像 Codex App 一样浏览当前 thread `cwd` 下的目录并按需打开任意文本文件。

本规格新增一个显式授权、只读、按目录 lazy load 的工作区文件浏览器。它取代 0033 中“Web 不列出目录内容”的旧范围边界，但只在新 capability 生效时开放；默认关闭时保持原行为。

## 2. 架构与安全边界

1. Codex App Server 提供 thread 身份及权威 `cwd`；Chilling 在 Desktop 主机本地执行路径校验、目录枚举、文件读取和引用复验。
2. Ariel Web 负责 UI、内存 cache 和请求；Desktop Agent 只通过版本化 Chilling client 转发固定 contract；Relay 只做 schema 校验与瞬时转发。
3. Relay 不枚举主机文件系统，不保存目录树或文件正文，不写内容日志、数据库、volume 或 crash dump。
4. 不新增 WebSocket。目录与正文请求复用现有 multiplexed WebSocket。
5. 功能不提供编辑、保存、创建、删除、移动、上传、下载或 shell；也不暴露绝对路径、任意本机目录和通用 filesystem proxy。
6. Chilling 必须显式开启 `--enable-ariel-workspace-browser`，且现有 `--enable-ariel-file-context` 同时开启；旧 Chilling 或 capability 缺失时 Web 不显示入口。

## 3. 能力与协议

### 3.1 Capability

Agent hello 增加可选能力：

```text
chillingWorkspaceBrowser: boolean
```

Web 只有在自身既有 file-context flag 开启、目标设备在线、`chillingFileContext=true` 且 `chillingWorkspaceBrowser=true` 时显示文件入口。

### 3.2 目录请求

新增 `workspace.files.list`：

- 输入：`threadId`、项目相对目录 `path`（根目录为空字符串）、可选 `cursor`、`limit`；
- `limit` 默认 100、最大 200；一次只枚举一个目录，不递归；
- 输出：项目相对 `path`、`entries[]`、`nextCursor`、`revision`；
- entry 只包含 `name`、项目相对 `path`、`kind=file|directory|symlink`、`sizeBytes`、`modifiedAtMs`、`readable`；
- directories 在前，其余按稳定的 Unicode name 顺序排列；分页 cursor 必须绑定同一目录 revision，目录变化返回 `DIRECTORY_CHANGED`，Web 清页后由用户或后台 revalidate 重读；
- 单响应最多 256 KiB。`.git` 目录可见但不可展开；其他 dotfile 正常显示。首版不解析 `.gitignore`，`node_modules` 等大目录仍只按单层分页读取。

### 3.3 文件请求

新增 `workspace.file.read`：

- 输入：`threadId`、项目相对 `path`；
- 输出复用现有 `OpenFile` 的 `path/content/sha256/modifiedAtMs/lineCount/language/version=current-workspace`；
- 仅支持普通 UTF-8 文本文件，解码后最大 1 MiB；binary、目录、symlink、设备文件、workspace escape 和超限内容明确拒绝；
- 读取时重新获取 thread `cwd`，不能用 Web 提供的 root，也不能复用另一 thread 的 cwd。

### 3.4 工作区选区引用

浏览器打开的原文继续支持 0090 的原生文本选区与“引用到会话”，但来源改为显式 union：

- `source=turn_change|message_reference|workspace_browser`；
- `workspace_browser` 只携带 `threadId/path/sha256/modifiedAtMs/startLine/endLine/text`，不伪造 `turnId/itemId`；
- 发送前 Chilling 以当前 thread cwd 重新读取同一路径，核对 digest、mtime、行范围与精确文本；任何变化返回 `FILE_CHANGED`；
- 仍遵守每条消息最多 5 个选区、单个 8 KiB、总计 24 KiB。

## 4. Web 交互与视觉

视觉方向沿用 Ariel 的紧凑 Night 工作台：低对比边框、密集树形列表、蓝色只用于选中项和焦点，不引入新的卡片或渐变体系。

1. 当前会话 header 增加至少 40×40 px 的 folder icon，名称为“浏览工作区文件”；能力未就绪时不显示失效按钮。
2. 桌面端打开约 360px 的侧面 drawer，会话正文保持可见；390×844 手机端使用全屏文件页，并提供“返回会话”。
3. 文件页 header 显示项目名、相对 breadcrumb、刷新和关闭；breadcrumb 每一级可返回，不接受手写绝对路径。
4. 目录按需进入；初次只请求 root。每个目录独立 loading/error/retry，“加载更多”只继续当前目录页，不让整个会话进入 loading。
5. 点击文件复用现有 `FileViewer` 的自动换行、行号、选区、高亮和引用按钮。浏览器文件没有 Diff tab；关闭文件时返回文件树，关闭文件树才返回会话。
6. 打开文件树、文件和返回时保留当前会话滚动位置、composer 草稿、图片和待发送引用；不得重新同步会话。
7. 删除、变更或不可读条目显示局部状态。目录刷新时如果当前文件消失，已打开内容可以继续只读显示，但新引用必须重新校验并失败。

## 5. Cache 与性能

1. Web 目录 cache 只存在内存中，按 `deviceId + agentEpoch + threadId + path + revision` 区分；最多 32 个目录页、总计 2 MiB，LRU 淘汰。
2. 文件正文复用现有最多 10 个文件／8 MiB 的内存 LRU，不进入 IndexedDB、localStorage 或 Service Worker cache。
3. 打开已缓存目录先立即展示，再在后台 revalidate；用户显式刷新、agentEpoch 变化、登出或设备切换时清理相应 cache。
4. 当前 turn 完成且包含 file change 时，仅失效这些文件的父目录和已缓存正文，不递归重扫工作区。
5. Relay 不 cache 目录或文件正文。目录浏览不会影响双 turn 会话 cache budget，也不为后台会话预取。

## 6. 错误与兼容

至少识别：`FEATURE_DISABLED`、`CHILLING_UNAVAILABLE`、`CONTRACT_UNSUPPORTED`、`THREAD_NOT_FOUND`、`DIRECTORY_NOT_FOUND`、`DIRECTORY_CHANGED`、`PATH_NOT_ALLOWED`、`FILE_NOT_FOUND`、`UNSUPPORTED_FILE_TYPE`、`FILE_TOO_LARGE`、`FILE_CHANGED`、`CAPACITY_EXCEEDED`。

旧 Web 忽略新 capability；新 Web 对旧 Agent／Chilling 隐藏入口。目录或文件失败不得清空会话、草稿、历史和 Relay cache，也不得自动重放请求。

## 7. TDD 与验收

1. Protocol red：非法绝对路径、`..`、过大 limit、伪造 entry、超大响应和未知方法被拒绝。
2. Chilling red：root／子目录、稳定排序、分页 revision、目录变化、普通文本读取、UTF-8／binary／超限／symlink／workspace escape／`.git` 限制及 thread cwd 变化。
3. Gateway red：capability 协商、flag 关闭零调用、timeout、epoch fencing、响应上限、错误透传以及无任意 method／endpoint。
4. Web red：入口 gating、root lazy load、breadcrumb、目录分页、局部重试、cache／revalidate、文件返回树、会话与草稿不刷新、引用 union 和 stale file 保留。
5. 真实浏览器：桌面 drawer 与 390×844 手机全屏页；长文件名无横向页面溢出；触控目标、键盘导航、文本选择和返回路径可用；展开前不预取子目录或正文。
6. 跨仓 fixture 只使用临时 workspace，真实 Chilling + fake Codex thread metadata 完成 list → read → select → prepare；不浏览真实业务目录，不产生写操作。
7. Go 全量／race／vet、Web 全量、production build 和 Playwright 全量通过；物理 iPhone／Safari 保留用户复验。

## 8. 验收标准

- 用户可以从当前会话进入其权威 cwd 的文件树，逐层浏览并按需打开文本文件。
- 初次打开只发生一次 root list；未点击的目录与文件零读取。
- 文件内容可以选段引用到当前会话，发送前由本机 Chilling 重新验证。
- 任意 workspace 外读取、symlink 跟随、Relay 持久化和通用 filesystem tunnel 均不可达。
- 文件浏览失败不影响会话浏览、发送、订阅、审批或历史 cache。

## 9. 实现证据

实现与验证记录见 [2026-10-25-0105 工作区文件浏览器验收](../testing/2026-10-25-0105-workspace-file-browser.md)。
