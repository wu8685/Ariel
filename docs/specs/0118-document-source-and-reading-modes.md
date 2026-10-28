# 0118：文档原版／Markdown 阅读模式与图片

- 状态：Implemented（2026-10-27；与 0117／0119 联合实施，自动化验收通过，未部署）。
- 日期：2026-10-27。
- 范围：Ariel 文件查看页、Markdown 展示／选区映射、Desktop Agent 内置文档图片按需读取与受限转发。
- 关联：0089、0090、0094、0105、0116；本地文件与资源后端迁移规格为 Ariel [0119](0119-self-contained-desktop-file-context.md)。
- 架构修订（2026-10-27）：按用户要求，取消本项对 Chilling 的依赖；其原 Draft 0005 不进入实现，资源契约收回 Ariel。本规格与 0119 已联合批准。
- 证据：[联合测试记录](../testing/2026-10-27-0117-0119-self-contained-files.md)；物理 iPhone／Safari 待用户复验。

## 1. 用户体验

1. 从 turn 文件变更、对话文档引用、workspace 文件树打开的 Markdown 文件，均提供“原版 / 阅读”切换。
2. **原版**：保留当前 UTF-8 原文、缩进、空行、行号与软换行，不执行 Markdown 或 HTML，不格式化文件、不改写内容。
3. **阅读**：把 Markdown 呈现为适合连续阅读的标题、段落、列表、引用、链接、代码块、GFM 表格／任务列表及图片。使用现有深色主题，正文不沿用聊天气泡，长文和图片不撑宽页面。
4. 首次打开默认原版，保持现有行为；同一个文件页切换 Diff 再返回时，恢复其上次原版／阅读选择。模式是当前查看器的展示状态，不修改会话、文件或全局偏好。
5. `.md`、`.markdown` 大小写不敏感地识别为 Markdown；其他文件只开放原版。MDX 不执行 JSX，不能当作可执行页面。
6. 保留“查看 Diff”的现有入口、当前工作区版本／本次修改标识以及删除文件和 diffTruncated 的规则；阅读仅作用于当前原文，不渲染 diff 为 Markdown。
7. 切换只重用已读取的文本，不重新订阅会话、不重新拉全文、不离开文件页；各模式独立保留本次阅读位置，文件 SHA 变化后清理旧选区与派生状态。

## 2. 输入、输出与架构归属

- 输入：已有 OpenFile 的 content／path／language／sha256、文件来源证明、当前设备及 Agent epoch、用户模式选择。
- 输出：原版或阅读展示；经校验的图片资源；仍对应原始文件的合法引用。原文是 SSOT，渲染 DOM 不是第二份文件。
- Ariel Web 负责模式、布局、选区映射、图片展示及有界内存；Desktop Agent 使用 0119 内置 file-context 执行读取和验证；Relay 只做认证／schema／路由及瞬时转发。
- 内置 file-context 负责重新验证父文档访问权限、Markdown 图片引用、workspace 路径、文件类型／大小与资源版本；Codex 身份与来源证明仍经版本化 Adapter 获取。Web 和 Relay 不直接读取本机路径，不建立通用文件下载／URL 代理。
- 本项不依赖新 Codex 私有 API，不新增 WebSocket；文件图片与 turn streaming／hot cache 分开读取，不嵌入会话快照或缓存正文。

## 3. Markdown、链接与引用

1. 复用项目已有 Markdown／GFM 依赖，使用文件阅读专用组件；不放宽现有聊天 Markdown 安全规则，不启用原生 HTML、script、iframe、表单、JSX、事件属性或不受信任的执行插件。
2. 保留文本解析错误边界；失败或超出渲染预算时显示明确提示并保留完整原版入口，不白屏、不静默截断文件。文本读取上限仍为既有 1 MiB；阅读渲染预算初始沿用 256 Ki UTF-16 code unit。
3. 阅读页中的外链只能由用户主动打开，保留协议白名单和 noopener／noreferrer。相对文档链接按当前文档目录解析并走现有受限文档读取，不拼成公网 Web 路径；无可靠原文证明时不冒充已有 native message reference。
4. 原版的内容级选区和显式“引用到会话”保持可用；选择本身不能跳回会话，返回／关闭不能提交选区或发送消息。
5. 阅读选区用 Markdown AST 的 source position 映射回同一原文连续切片，沿用路径、SHA、行范围和内置 file-context prepare 复验。不能用 `indexOf(selectedText)` 搜索重复文字，也不能把渲染后的空白、图片 alt、行号或代码复制按钮拼成假原文。
6. 跨格式或转义选区可能对应包含 Markdown 标记的原始片段，明确标示“引用对应原文片段”。无法可靠映射的选区不生成引用，保留阅读位置并提示切到原版精确选择；不自动扩大为整行／整段。
7. 模式切换清理旧 DOM 选区及可提交状态，不重用上一模式的陈旧 Range；会话切换／Agent epoch 变化和文件版本变化均按现有 identity fencing 处理。

## 4. 图片显示

### 4.1 本地图片

- 支持标准 Markdown 内联和引用式图片，例如 `![说明](images/a.png)`。按父文档目录解析相对路径，包括归一化后仍在同一 workspace 内的 `../assets/a.png`；不得按 Web 当前 URL 解析。
- 只有当前父文档实际引用的资源可读取；用户不能通过 imageReference 参数指定任意本机文件。URL 编码、中文、空格和引用式 definition 均需按 Markdown／URL 语义解析，而不是正则猜路径。
- 首版支持经文件签名与图像头核对的 PNG、JPEG、GIF、WebP；SVG、HTML、任意 file:／绝对本机路径不直接渲染或执行，显示不支持占位。Obsidian wikilink 图片和原生 HTML img 暂不冒充已支持。
- 浏览器可见区域附近才请求，滚出后允许释放资源；原版／Diff／隐藏文件页不预读图片。关闭或切文件时取消本地待办、忽略晚到结果并释放 Blob URL。
- 图片宽度不超过内容区，保留纵横比；有 alt／加载／失败／超限状态，局部失败不能阻断文本和模式切换。

### 4.2 外链及内嵌图片

- 公网 HTTPS 图片提供域名可见的“允许加载外部图片”操作，按当前文档显式允许后再 lazy 展示，避免打开文档即暗中联系第三方。授权不跨文档／版本自动沿用。
- 外部图片由浏览器加载，不由 Relay／Agent 请求外网或转发 Cookie／token；不主动附加 Ariel 凭据或文件／会话正文，设置 no-referrer。浏览器策略／防盗链导致失败时局部提示，不启用万能代理绕过。
- 拒绝 HTTP mixed content、协议相对地址、带用户信息的 URL、javascript:、file:、已知 localhost／内网 IP／本地特殊主机名；未授权时不发起资源请求。
- data: 仅允许经过解码大小、MIME 和图像头校验的上述 raster 类型，纳入相同资源预算；不接受 data SVG／HTML。

## 5. 能力、传输与容量

1. 只在 0119 file-context flag 开启时接入本地图片，且 Desktop Agent 显式开启 `--enable-document-assets`。后端未升级时，阅读文字仍可用；本地图片显示清楚的能力缺失提示，不让整页失败。
2. 必须同时确认 Relay 支持此公共扩展、目标 Agent 已确认内置资源读取能力，才发送新方法。能力缺失不向旧 Relay／Agent 试发未知 request，不导致“协议消息不合法”或登录失效循环。
3. 能力使用既有响应 data 的可忽略扩展协商：Relay 的 device.list 响应声明扩展；Agent 的成功文件读取响应附带 document-assets 能力，Relay 仅在当前 Agent connection 上记录经该响应确认的能力。不得向旧 device／hello 严格结构无条件增加字段。
4. 新增只读 `file.context.asset.read`（最终命名以批准 contract 为准）。请求绑定 device／thread／Agent epoch、父文档来源证明、documentSha256、原始 imageReference 及可选 opaque cursor；不接收客户端指定的 workspace root 或任意绝对资源路径。
5. 内置 file-context 按文档版本和真实引用返回 raster MIME、asset digest／revision、尺寸、总字节及分块 base64。原始 chunk 最多 256 KiB，完整协议帧最多 384 KiB；Agent／Relay 每层均校验，不能为了图片放大所有请求的全局帧或文本响应上限。
6. 原图最多 4 MiB、12 MP、单边最多 8192px；每个查看器同时读取最多两张、最多保留 32 项待处理资源。每张分块顺序读取并校验版本／完整 digest，不混合两个版本的字节。
7. Web 图片字节 cache 默认最多 16 MiB，按 viewport 引用与 LRU 释放，Blob URL 释放必须与 DOM 使用协调；内嵌图片计入预算。超限显示占位而不是无限内存增长。大图解码尺寸也必须受限，不能只限制压缩文件大小。
8. Relay 不把图片放入两 turn hot cache、磁盘、日志或 volume；Agent 可有短时有界资源 lease：30 秒、总字节 16 MiB、最多 32 项，cursor 绑定来源／Agent epoch／SHA／顺序 offset，不混版本、不逐块全图重读。具体约束见 0119。不得因图片读取新增轮询或自动预热整份文档。

## 6. 错误与验收

- 父文档已变化、图片已变化／不存在、路径拒绝、类型／大小超限、能力缺失、分块过期或损坏均局部显示原因。不得悄悄请求最新文件后用旧 SHA 继续引用，不自动全量重同步会话。
- 批准后按 red → green → refactor：先覆盖模式／文件识别、Markdown 语义、HTML／危险 URL 拒绝、重复文本和格式化选区映射、lazy／分块／预算／Blob 释放、能力降级及 epoch／晚到回执 fencing。
- Ariel UT／contract 测试先复现相对图片无法加载，再验证标准图片和 reference image、安全路径、digest／cursor／分块及旧端组合；全部文件测试须不依赖 Chilling 进程／仓库。禁止用 mock 通过来声称新的 native API supported；本项不引入 native mutation。
- Chromium 320／390／1280px 验证三种文件入口、原版／阅读／Diff、图片／长表格不撑宽、精确引用与返回不丢草稿、滚动位置和多图 lazy 请求数。保存固定截图；隔离文件与 WebSocket fixture，不读取真实私人文档作测试素材。
- 运行 Ariel 全量 Web／浏览器／build、相关 Go／race／vet 及内置文件后端 contract；物理 iPhone／Safari 未验证部分须明确保留。

## 7. 交付边界

- 不编辑或保存文件，不增加任务／看板功能，不持久化正文／图片，不自动上传知识库。
- 本规格及 0117／0119 已于 2026-10-27 联合批准；严格按 red → green → refactor 实施。
- 本地图片只需升级 Ariel Desktop Agent 与 Relay/Web，不要求安装／启动／升级 Chilling；发布和部署须按当次授权执行，不能仅发布 Web 就宣称图片链路可用。

## 8. 2026-10-28：阅读页视觉维护

用户明确要求参考所给手机截图优化正文底色、字体大小和排版。本项在既有已批准阅读组件范围内调整展示，不增加功能、通信语义或安全边界；不开始仍为 Draft 的 0126 异步问题能力。

- 视觉方向为克制的深灰长文阅读面板：正文底色复用 `--color-panel`（#202020），正文使用 `--color-muted`（#e0e0e0），次要内容使用现有 subtle 色；不改全局主题或会话气泡。
- 仅 Markdown **阅读模式**生效。手机（≤800px）正文从 12px 调为 15px，桌面从 14px 调为 16px，行高 1.75；沿用现有系统/中文字体栈，不下载外部字体。浏览器缩放和辅助功能放大仍可用。
- 手机正文左右留白至少 16px，桌面至少 20px且阅读列最多 720px；不强制两端对齐，不增加字符间距。段落间距约 1em，标题以清晰层级和适量留白区分。
- Markdown soft break 使用正常段落流，显式 hard break、列表与引用层级保持；原文、TeX、代码、空格和文件 SHA 不修改。代码块继续等宽、保留缩进/Tab和内部滚动，阅读代码字号为正文的 .875em，底色比正文更深。
- 原版与 Diff 保持既有行号、软换行、等宽字和增删颜色；会话正文样式保持。默认原版、菜单中的模式切换、多 Tab、全屏/最小化、刷新恢复和按需引用栏不变。
- 继续正确渲染数学公式、受控 lazy 图片和 GFM 表格；宽公式/代码/表格只在内部滚动，不撑宽或裁切正文。不把参考图中原始 TeX 的显示当成目标行为。
- 使用阅读专用 class/token 隔离，切到原版或 Diff 后撤销；不新增请求、watch、socket、轮询、持久化或后端依赖。
- 按 TDD 先运行失败的样式状态单测和 320/390/1280px Chromium computed style/几何用例，再改 CSS。验证颜色、字号、行高、留白、列宽、长文滚动、软/硬换行、精确引用和模式恢复，并审阅更新的合成截图。
- 全量 Web UT/build/Chromium，以及相关 Go 回归通过后交付；截图不能替代行为断言，物理 iPhone/Safari 待用户复验，不自动部署。
