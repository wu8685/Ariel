# 0125：Markdown 本地 SVG 插图的受限显示

- 状态：Implemented；用户于 2026-10-28 明确确认后按 TDD 完成，未部署；物理 iPhone／Safari 待复验。证据见[验收记录](../testing/2026-10-28-0054-document-image-feedback.md)。
- 日期：2026-10-28。
- Issue：#54。
- 关联：0118／0119／0124。取代这些规格中对本地 SVG 图片的整体排除；不开放任意 SVG 文档或 HTML 执行。

## 1. 已证实的原因与用户行为

- 用户指定文档使用混合 PNG／SVG 插图。失败位置对应标准 Markdown 引用的 SVG；SVG 根包含宽高／viewBox，图形主要为 path／g／defs／clipPath／use。仅做本地只读核对，不提交私人文档、原图或其中的文字。
- 内置 AssetRead 当前只支持 PNG／JPEG／GIF／WebP，SVG 在图像解码阶段返回 UNSUPPORTED_FILE_TYPE。Web 又复用文本错误映射，误显示 UTF-8 限制；不是图片需要转换为 UTF-8，也不是本次证实的父文档编码错误。
- 文案维护按 0124 执行；真正让 SVG 插图显示涉及本规格的新格式与安全边界，须批准后实施。
- 批准后：用户仍只需打开 Markdown 阅读模式，靠近视口的合规静态 SVG 自动按需加载；保留图形和缩放清晰度。失败只影响该图，不清空正文／草稿、不重新登录／刷新会话。

## 2. 方案与输入输出

1. 复用 Ariel 内置 file-context 的父文档来源、SHA、实际 Markdown 引用、workspace no-follow 文件句柄；不依赖 Chilling，不修改原图、不新增外网代理或公网文件服务。
2. SVG 输入仍最多 4 MiB。Agent 用标准 XML parser 验证受限静态子集并产生规范化的显示副本，保留原文件不变；输出为经过验证的 image/svg+xml、规范尺寸、显示副本 SHA 和既有顺序分块。
3. Web 验证预算、分块、digest 与 SVG 类型／尺寸，仅创建 Blob URL 作为 `<img>` 的 src。不用 inline SVG、dangerouslySetInnerHTML、iframe／object／embed，也不提供把该 Blob 作为交互文档打开的入口。
4. 不引入本机 SVG 转图命令、第二个进程或云端转换服务。允许的图形通过浏览器图片解码器显示；加载成功须验证 naturalWidth／naturalHeight，不把收到字节视作已显示。

## 3. 静态子集与安全不变量

- 仅同一文档内的基本形状、path、分组、defs、clipPath、渐变、文字与同文档 use 引用；实际元素／属性允许表须作为代码和测试显式列出。保留 PDF 导出常用的 pt 宽高、viewBox、transform 与本地 glyph use；数字必须有限。
- 拒绝 script、事件属性、foreignObject、HTML 命名空间、动画、外链 a、image／feImage、filter、XML DTD／ENTITY／样式处理指令、xml:base 和任何可执行或外部资源载入能力。不得默默显示经删除关键内容的不完整图形。
- href／xlink:href 及 paint／clip 引用只允许已存在的同文档 `#id`，不允许相对文件、file、HTTP(S)、data 或其他 scheme。不解析 CSS import／外部 font，不依赖浏览器碰巧阻止外网。
- 不接受任意 style sheet；style 属性仅在有限的展示属性允许表中解析后重建，拒绝外部 URL／表达式／变量等超出子集的值；同文档 paint／clip 引用仍须通过 `#id` 校验。未知或危险内容明确局部拒绝，不把原字符串直接复制入 HTML。
- 节点最多 20,000、XML 深度 64、单节点属性最多 64、单属性最多 64 KiB；引用深度最多 16、展开节点最多 100,000，拒绝环、重复 id 与未解析引用。除节点展开预算外，原始几何数字 token 最多 1,000,000；展开渲染工作量（元素、数字 token、文本 UTF-8 字节）合计最多 1,000,000，不能让复杂 path 或长文字借多重 use 绕过预算。预算分别计量，避免把正常 PDF 导出路径中的坐标数量误算成 XML 节点；实际用户指定的大图约 2.2 MiB／6,058 节点／24 万坐标 token，仍位于原有 4 MiB／12 MP 边界内。解析／验证周期检查 context，超限局部失败，不阻塞会话锁。
- 输出规范尺寸仍单边 ≤8192px、面积 ≤12 MP。宽高使用明确的有限绝对单位或合法 viewBox 推导；不明确／非正／无限尺寸拒绝。规范化后的显示字节也须 ≤4 MiB。
- 原有两张并发、32 项等待、16 MiB cache／lease、256 KiB chunk、384 KiB frame、30 秒 TTL、取消／epoch／SHA fencing 及 Blob URL 生命周期不放大。SVG 不进入 Relay hot cache、磁盘或 checkpoint。
- 图片本身不能形成文件内容选区；不把 SVG 字形、alt 或生成 DOM 当作原 Markdown 原文引用。

## 4. 新旧端兼容

1. 保留现有 raster 的 v1 行为。成功父文档 response.data.documentAssets 增加可选 `svgVersion: 1`；Relay device.list data 增加可选 `documentSvgVersion: 1`。两者都确认后才开放，不往严格 hello／device capability 任意加字段。
2. 请求 params 的可选 opt-in 为 `svgVersion: 1`，仅在 Relay 与当前 Agent connection 均已确认该扩展时发送；Agent 仅在 opt-in 后返回 SVG，旧 Web／旧 Relay 不会意外收到不认识的 MIME。Relay 在当前 Agent connection 的成功父文档响应上记录 SVG 能力，连接替换即重置；未确认或参数不匹配局部拒绝，不终止整个 WebSocket。
3. 规范化 SVG 属于显示副本，asset digest 覆盖实际传输字节；doc SHA 仍绑定原 Markdown。Relay 只验证已协商的 MIME／预算与请求关联，不接触本机路径、不保存图片。
4. 旧端仍能读文字和 raster 图片；本地 SVG 明确显示需要升级／不支持，不触发协议断线或登录循环。schema 保留现有请求字段，并仅增加上述 opt-in；当前宽松 response.data 上的扩展可被旧客户端安全忽略。
5. 本阶段不新增 SVG data URI、任意会话本地图片推断、HTML img 或 Obsidian wikilink；公网 HTTPS 图片仍由浏览器按既有许可加载，不扩成 SVG 转发代理。

## 5. TDD 与验收

批准后先失败测试再实现，不使用私人 SVG 作为提交 fixture：

- 合成 path／glyph use／clipPath／pt 尺寸图，按原文、消息引用、workspace 三种授权入口成功；重新核验父文档 SHA、越界／symlink／伪造引用拒绝。
- script／事件、foreignObject、DTD／ENTITY、外网／data／file 引用、CSS 绕过、动画、命名空间混淆、循环 use／指数展开、超限节点／深度／尺寸均拒绝，验证零外网请求／零脚本执行。
- 新旧 Web／Relay／Agent 的协商矩阵，raster 行为不变；坏 digest、cursor、epoch／版本变化与取消不复用旧图片。
- Chromium 320／390／1280px 真正解码并验证尺寸，保留阅读位置、Tab、最小化、精确 Markdown 引用、原版与 Diff；只加载近视口图片，不重新拉会话正文。
- 全量 Go／race／vet、Web UT／build 与完整浏览器回归；真实环境只做只读 smoke，不对真实 Codex 会话发送测试消息。物理 iPhone／Safari 保留用户复验。

## 6. 依据与交付

- [W3C SVG 2 processing modes](https://www.w3.org/TR/SVG2/conform.html#processing-modes)：SVG 的文档模式与图片模式具有不同能力；本方案额外用严格子集和拒绝外部引用约束输入，不仅依靠浏览器隔离。
- [W3C SVG 2 linking](https://www.w3.org/TR/SVG2/linking.html)：同文档引用与外部文件引用须区分，环形引用无效。
- 本规格批准前不放开 SVG。发布／部署仍须有当次授权，且需更新目标 Desktop Agent 与 Relay/Web，不能只发 Web 就宣称 SVG 读取可用。
