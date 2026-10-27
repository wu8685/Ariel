# 0094：会话内 Workspace 文档引用与 Codex 文件入口

> 后端归属已由 [0119](0119-self-contained-desktop-file-context.md) 取代：文件引用验证与读取内置到 Ariel Desktop Agent。以下保留原设计与既有来源、安全和交互规则。

- 状态：Implemented（2026-10-25；Markdown／裸路径文件入口、受控 Chilling 读取、行定位与选区引用已完成）
- 日期：2026-10-17
- 影响范围：Ariel Web、公共协议、Desktop Agent 的 Chilling gateway
- 配套后端规格：`chilling/docs/specs/0003-inline-workspace-document-references.md`
- 前置规格：0089、0090、0100

## 1. 背景

Codex 回复经常引用 workspace 文件。Codex App 会把这类引用显示成可点击的文件入口；目前已确认至少存在两种可见形态：

1. Markdown link，例如：

```markdown
[查看原始记忆](/workspace/Memory/example.md:37)
```

2. 回复正文中的裸文件名／相对路径，例如：

```text
文件：ops-2026-10-25.md
```

截图实证中，Codex App 将 `ops-2026-10-25.md` 显示为带文件图标的蓝色入口，并同时展示“已编辑”文件卡；Ariel 只显示普通文字。Ariel 当前的安全 Markdown renderer 只允许 `https`、`http`、`mailto` 和页内 anchor，本机路径会被降级为无行为文字；既有 turn 文件卡又依赖 Chilling capability 与 native `fileChange` 查询结果。因此同一条原生回复在 Codex App 和 Ariel 中语义不一致。

本规格让 Ariel 保留这类引用的文档语义，并复用已有文件内容页、选区与“引用到会话”能力。正文文件入口不依赖 turn 文件变更卡是否存在。任意文件读取仍被禁止；候选引用必须由 Chilling 结合原始 Codex item、thread 权威 cwd 和文件系统边界重新验证。

## 2. 输入与输出

### 2.1 输入

- `agentMessage.text` 中的 Markdown link destination；
- `agentMessage.text` 中边界明确的裸文件名、相对路径或 workspace 内绝对路径；
- candidate 为绝对路径或 workspace 相对路径，可带行号：
  - `path/to/file.md`
  - `path/to/file.md:37`
  - `path/to/file.md:37:5`
  - `path/to/file.md#L37`
  - `path/to/file.md#L37-L45`
- 当前 `threadId`、`turnId`、`itemId` 和 Desktop Agent device。
- 若 native Codex item 另有版本化、公开的结构化文件 reference metadata，可在 compatibility probe 证明 shape 后作为第三种输入；未验证前不得猜字段。

### 2.2 输出

- Ariel 将已识别的本地引用显示为带文档图标的可点击引用：Markdown link 保留 label，裸引用保留原文件名／路径文字；
- 点击后进入现有文件内容页，展示项目相对路径与当前 workspace 版本；
- 若引用带行号，文件内容页首次打开时滚动到目标行并高亮目标行／范围；
- 用户可以选择任意文本并点击“引用到会话”，行为与 0090 一致；
- 引用无效、越界或后端能力未启用时，回复正文仍完整可读，不产生外部导航；
- 同一 turn 存在可验证 native `fileChange` 时，继续显示 0089 的“修改的文件”卡；正文引用入口与该卡互不替代。

## 3. Web 识别与呈现

### 3.1 分类顺序

Markdown link 按以下顺序分类：

1. `http`／`https`／`mailto`／页内 anchor：保持 0019 的现有行为；
2. 语法合法的 workspace document candidate：交给文档引用组件；
3. 其他 scheme、控制字符、ambiguous URL 或非法路径：保持普通文字，不可点击。

Markdown 渲染完成后，再对不位于 link、inline code、code block、HTML、URL 或 diff 中的普通文本做有界裸引用识别。裸引用必须满足明确 token 边界与本地文件形态；自然语言中的普通点号、域名、版本号和不存在的文件不能升级成入口。首版至少支持：

- `name.ext`；
- `dir/name.ext`、`./dir/name.ext`；
- thread cwd 内绝对路径；
- 上述形式附带 `:line[:column]` 或 `#Lx[-Ly]`。

裸 basename 只按 thread 权威 cwd 根目录解析，不递归搜索同名文件；相对路径按 cwd 解析。若路径不唯一、无法证明或文件不存在则 fail closed。

不得放宽全局 `safeHref` 来允许 `file://` 或任意绝对路径导航。文档引用必须走 Ariel 的受控 click handler。

### 3.2 文档引用组件

- 视觉上使用紧凑的文档／代码图标、accent 颜色和下划线，整体可点击；
- accessible name 为 `打开文档：<label>`；键盘 Enter／Space 可打开；
- 不在 DOM 的 `href` 中暴露本机绝对路径；路径只作为 React 内部 request data；
- streaming 文本尚未形成完整 Markdown link 时按普通文本显示，形成完整 link 后原位升级，不能闪空整条消息；
- 同一条消息的 Markdown 与裸引用合计最多识别 24 个，超出部分保持普通文字。
- streaming 文本中的裸候选只有在 token 右边界稳定后才升级，避免输入增长时反复吞吐相邻标点。

### 3.3 文件内容页

- 复用 0090 的 `FileViewer`、内存 LRU、自动换行、返回、原文／Diff tabs 和选区引用；
- 普通文档引用没有该 turn 的 file-change diff，因此只提供“查看原文”，Diff tab disabled；若同一路径能与同 turn 的已验证 `fileChange` 精确关联，则可复用该 change 的 Diff，但不能仅按显示文件名猜测关联；
- 首次进入时按引用行号定位；返回会话后保留滚动位置、草稿、附件和选区 chip；
- 已缓存文件可即时显示，但引用校验失败时必须关闭该内容页并显示明确错误。

## 4. 协议

新增独立方法：

```text
file.context.reference.read
```

- request 必须携带 `threadId + turnId + itemId + referenceKind + rawReference`；`referenceKind` 首版为 `markdown_link` 或 `plain_path`，`rawReference` 是 assistant message 中的原始 destination／原始裸 token，不是浏览器 URL；
- 可选 `startLine + endLine` 只是 Web 解析出的定位提示，不参与文件授权，Chilling 必须从原始 destination 重新解析并校验；
- 现有 `file.context.read` 继续只接受已验证 file change 的项目相对路径，不增加 `origin` 分支，也不放宽 `relativeFilePath` schema；
- Relay 只校验有界 schema 并透传，不读取路径、不接触文件系统；
- Desktop Agent 只允许转发到版本化 Chilling contract，不自行读取文件。

Chilling 未声明 `messageReferences=true` 时，Ariel 可以显示文档候选样式，但点击后提示“当前 Desktop 尚不支持打开该文档引用”，不得回退为浏览器本地路径导航。目标设备必须本地运行并启用 Chilling；仅开启公网 Web flag 不足以授权读取。

### 4.1 与既有文件变更卡的组合

1. `file.context.changes` 继续负责 native `fileChange` 卡；只要 top-level turn 中存在已验证 change，卡片必须显示“原文／Diff”入口。
2. `file.context.reference.read` 负责回复正文中的引用；即使 change 卡为空、change 来自 Codex 暂未投影的 subagent shape，正文中已验证的文件引用仍可打开原文。
3. 不得根据“我修改了某文件”一类自然语言伪造 `fileChange` 或 Diff。若 Codex App 的“已编辑”卡来自当前 adapter 尚未覆盖的 native shape，先用隔离 fixture 记录并扩展 Chilling changes adapter，再声称卡片已对齐。
4. 两种入口打开同一个 `FileViewer`，返回、自动换行、原文／Diff、文本选区和“引用到会话”体验保持一致。

### 4.2 与 0100 Hot Cache 的组合

1. Relay 双 turn cache 保留原始 `turnId + itemId + agentMessage.text`，所以 fresh cache 命中后 Web 可以直接渲染文档引用，不需要 `thread.read` 或 `thread.subscribe`。
2. 点击文档引用仍是一次独立的 Agent-forwarded read，不从 Relay cache 读取文件正文；Relay 不新增文件正文或 Diff cache。
3. cache stale／Agent 离线时，引用样式继续保留，但新文件读取 fail closed，并只提示该引用暂不可打开；不得清空或重新同步整条会话。
4. 分页加载的旧 turn 使用同一 renderer 和同一授权方法，不因其不在 Relay 双 turn cache 中而改变语义。

## 5. 安全不变量

1. Chilling 必须回读指定 `threadId + turnId + itemId`，且 item 必须是 `agentMessage`。
2. `markdown_link` 的 `rawReference` 必须先与 item 文本中一个完整 Markdown link destination 逐字节匹配；`plain_path` 必须与 item 普通文本中一个满足同一语法和 token 边界的完整裸 token 逐字节匹配。匹配成功后才能执行一次规范化；仅 label、basename 近似或另一 item 中出现均不构成授权。
3. 绝对路径必须位于 thread 权威 cwd 内，再转换为相对路径；相对路径按 cwd 解析。
4. 禁止 `file://`、网络 share、symlink escape、目录、binary、NUL、超限文件和 workspace 外路径。
5. Web 不把绝对路径放入 DOM `href`；Relay 不记录 request body；浏览器持久缓存和日志不得保存文件正文。当前已打开内容继续只存在有界内存 LRU。Relay hot cache 已按 0100 保存原始消息正文，不能据此绕过逐次 reference read 的授权验证。
6. 行号不改变授权范围，不允许借行号语法拼接第二条路径。
7. 裸引用 parser 必须跳过 Markdown code、已有 link、URL、email、HTML 和 diff fenced block，避免把示例文本或远端地址误作本机授权证据。

## 6. 错误处理

- 引用未在原始 item 中找到或 token 边界不匹配：`REFERENCE_NOT_FOUND`；
- item 不存在或不是 `agentMessage`：`ITEM_NOT_FOUND`；
- workspace 外、遍历、symlink escape：`PATH_NOT_ALLOWED`；
- 文件不存在／已删除：`FILE_NOT_FOUND`；
- binary 或非 UTF-8：`UNSUPPORTED_FILE_TYPE`；
- 超过 1 MiB：`FILE_TOO_LARGE`；
- Chilling 版本不支持：`CONTRACT_UNSUPPORTED`。

任何错误只影响本次打开动作；不得替换整条消息、清空当前会话或把 owner 降为只读。

## 7. TDD 与验收

### 7.1 Red

- Markdown renderer 当前把本地文件 link 降级为普通文字；
- 现有 Chilling read 拒绝非 `fileChange` item；
- 文件页当前不会按引用行号定位。

### 7.2 Green

- parser 覆盖 Markdown link、裸 basename／相对／绝对路径、空格、Unicode、四类行号格式、HTTP link、URL／代码排除和恶意 scheme；
- Web 点击只发一次 `file.context.reference.read`；fresh Relay cache 路径不得额外产生 `thread.read`／`thread.subscribe`；
- Chilling 必须以真实原始 item 证明 link destination 或裸 token 存在后才读取；仅知道 workspace path 不能绕过；
- 使用隔离真实 Codex fixture 分别验证普通文件修改与 subagent 文件修改的 native item shape；已验证 shape 显示 0089 文件卡，未验证 shape 仍至少通过正文引用提供原文入口；
- fresh owner、分页历史和增量 delta 均保留相同引用语义；
- 手机 viewport 验证引用可点击、内容页定位、返回和选区引用，不遮挡正文。

### 7.3 验收标准

1. 截图中的“查看原始记忆”和 `ops-2026-10-25.md` 在 Ariel 中呈现为文档引用，而非普通文字。
2. 点击后打开 Ariel 文件内容页，并定位到引用行。
3. 可从内容页选择片段并引用到当前会话。
4. 外部 URL 及普通 Markdown 行为不回归。
5. 任意未以合法 Markdown destination 或完整裸 token 出现在原始 assistant message 中的 workspace 文件不能通过该接口读取。
6. Go、Web、协议、跨仓 fixture 与真实移动 Chromium 回归通过；物理 iPhone/Safari 由用户最终复验。
7. fresh Relay cache、stale cache、分页旧历史三种入口都保留引用呈现；只有 fresh／在线入口能首次读取尚未进入内存 LRU 的文件正文。
8. native `fileChange` 可用时，Ariel 与 Codex 一样同时展示正文文件入口和 turn 文件变更卡；无 native change 时不得伪造“已编辑”卡。

## 8. 实现与验证记录（2026-10-25）

- Web 会把 assistant message 中已验证形态的 Markdown destination 与裸文件 token 投影为受控文档按钮；单条消息最多 24 个，code、URL、email、raw HTML 与 fenced diff 中的候选保持普通文本。
- `file.context.reference.read` 已加入公共 schema、Web client、Relay 长读分类和 Desktop Agent → Chilling gateway；Chilling 不声明 `messageReferences` 时只保留可读正文并明确提示不支持。
- 文件页复用 0090 的返回、自动换行、内存 LRU、原文／Diff 与选区引用；普通引用只启用原文，带行号引用会滚动并高亮目标范围，引用到会话时携带原始 reference proof 供 Chilling 二次验证。
- `go test ./...`、相关 Go race suite 与 `go vet ./...` 通过；Web 30 个 test files／246 tests、production build 以及 44 个真实 Chromium 浏览器 case 全部通过。
- 使用本机 `codex-cli 0.145.0-alpha.27` 对当前真实 thread 做只读 compatibility probe：native `fileChange` 读取和 `agentMessage` 文档引用读取均通过，未创建或修改 turn。
- 仍未独立证明“subagent 文件修改”在当前 Codex 版本中的 native item shape；因此该情形只保证正文引用入口，不能声称一定生成“已编辑”Diff 卡。
- 完整浏览器回归同时刷新了此前 logo 资源优化后遗留的截图基线和 `-512.png` 路径断言；界面视觉未发生非预期变化。
