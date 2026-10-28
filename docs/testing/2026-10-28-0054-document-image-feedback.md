# Issue #54：Markdown 图片误报 UTF-8 限制

- 日期：2026-10-28。
- 范围：落实已批准 0124 的图片局部错误提示，以及用户已明确确认的 0125 受限静态 SVG。
- 用户指定文档只在本地做只读核对；未修改、上传或提交私人 Markdown／SVG／PDF。

## 根因

1. 用户所指位置为标准 Markdown 相对引用的 SVG，另有 PNG 插图，解释了为什么只有部分插图失败。
2. `internal/filecontext/assets.go` 用 image.DecodeConfig 支持 PNG／JPEG／GIF／WebP；按 0118／0119 原限制拒绝 SVG 并返回 UNSUPPORTED_FILE_TYPE，不是把 SVG 错当成文本读取。
3. Web 的 requestDocumentAsset 调用全局文本 errorText，于是同一 wire code 显示“当前只支持 UTF-8 文本文件”。这是错误语境混用。

## TDD 证据

- 先新增 11 个单测：错误映射尚不存在，实际执行 11 项失败。
- 先新增真实 Chromium 390px 的四类图片错误：UNSUPPORTED_FILE_TYPE／FILE_TOO_LARGE／FILE_NOT_FOUND／PATH_NOT_ALLOWED，旧实现全部在期待图片专用提示处失败；格式用例复现 UTF-8 文本提示。
- 再加入 documentAssetErrorText 并仅替换文档图片请求的错误映射；格式／4 MiB、8192px、12 MP 限制／缺失／路径／版本／能力／超时／容量使用图片语境，不展示未识别的后端 detail。普通文件读取的 UTF-8 行为和 native 图片 API 不变。
- 修复后 11 项单测与四类浏览器用例通过；扩展至 320／390／1280px 共 12 项，真实解码重试成功、零重读父文档、单会话 watch、主草稿保留和页面不横向溢出均验证。
- 三种宽度的格式提示截图已人工核对；没有更改 CSS 或既有截图。错误提示 fixture 全部合成，重试的成功响应为合法 PNG；SVG 的真实解码由下面的独立用例证明。
- 完整回归发现存量 file-workspace 选区恢复测试的前提竞争：它要求刷新后无主会话，却未隔离原先已允许的 IndexedDB 主会话恢复；历史写入是否先于 reload 提交会改变引用按钮的合法状态。仅在此 fixture 禁用独立的 IndexedDB 历史缓存，继续保留 sessionStorage 文件现场、SHA、选区、滚动、禁止跨会话引用和正文不持久化的原断言；不修改产品恢复逻辑或放宽授权。
- 修复该 fixture 后独立重复 10 次通过，完整 148 项回归通过；随后加入 SVG 用例，再进行最终全量回归。

## 静态 SVG：red → green → review

1. Agent 合成 SVG 先失败于缺失能力和格式；protocol／Relay 先失败于未允许 opt-in 和缺少扩展声明；Web 先失败于拒绝 SVG MIME；Chromium 390px 先失败于无法解码。之后才实施规范化静态 XML、能力协商与 Blob img 链路。
2. 合成的 path、PDF glyph use、clipPath、渐变、pt 单位和有限 style 均成功；workspace／turn change／message reference 三种父文档来源，以及真实 Agent＋Relay（无 Chilling）的 PNG＋SVG wire 链路通过。SVG 分块的 digest 覆盖规范化显示字节，cursor 绑定 opt-in；Agent 连接替换后不继承旧能力。
3. 脚本、事件、foreignObject、HTML、动画、image、filter、样式表、DTD／ENTITY、外部／data／file 引用、xml:base、命名空间混淆、未知引用、重复 id、环和过量展开均拒绝。Web 在创建 Blob 前再验证，浏览器断言零外部请求、零脚本执行、零 inline SVG／object／iframe／embed。
4. 代码检查又复现并修复四个问题：十六进制颜色 `#5e55ff` 被数值扫描误当指数；只数节点无法限制 use 放大的 path 与长文本；一张 SVG 在 Web 侧重复解析 XML；viewBox 错误修复显式零／负数／百分比尺寸。相关 UT 均实际 red 后再 green。最终分别限制 100,000 展开节点和 1,000,000 渲染工作量；一张图只解析一次 XML，不以放宽 4 MiB／12 MP 边界解决性能问题。
5. Chromium 320／390／1280px 验证 Blob 图像真实解码尺寸、离屏 SVG 零读取、模式恢复复用缓存、旧 Relay／旧 Agent 不发送未知 opt-in、恶意响应局部失败；三张新增 SVG 截图已核对。

## 用户指定文档：本地只读 smoke

实际 5 张 SVG 均经公开 file-context Service 的父文档 SHA／真实图片引用验证、Agent 规范化、生产 Web `staticSVGInfo` 校验，再由独立 Chromium Blob img 解码。只读 helper 不替代完整 native 兼容性测试，也不对真实 Codex 会话执行写操作。

| 插图 | 规范化字节 | Chromium 解码尺寸 | 外部请求 |
| --- | ---: | --- | ---: |
| 1 | 2,181,649 | 1704×600 | 0 |
| 3（报错位置） | 131,846 | 816×653 | 0 |
| 4 | 392,809 | 417×314 | 0 |
| 5 | 387,512 | 405×311 | 0 |
| 6 | 417,267 | 406×311 | 0 |

原文、SVG、绝对路径及图片内容未提交、未上传；显示副本和浏览器 Blob 仅在本机进程内存中使用，未保存私人截图。公开测试与截图全部为合成 fixture。

## 验收命令与边界

```sh
cd web
npm test
npm run build
npm run test:ui -- --workers=1
```

```sh
go test ./...
go vet ./...
go test -race ./internal/filecontext ./internal/relay ./internal/desktopagent
go test ./internal/filecontext -run '^$' -fuzz FuzzStaticSVGImage -fuzztime=10s
git diff --check
```

最终 Web 384 项单测、生产构建、完整 Chromium 154 项、Go 全量／vet，以及 filecontext／relay／desktopagent race 均通过。最终 parser fuzz 10 秒完成 258,280 次执行，无 panic 或预算不变量失败。Vite 仍有既有的大 chunk 构建提示，本次没有引入新依赖或额外外部运行时。

## 未验与交付边界

不承诺支持任意 SVG；主动／外部资源及超过预算的图仍局部拒绝，SVG data URI 和 HTML img 不新增支持。原版／Diff／精确引用、Tab／草稿／恢复等原有用例须随完整浏览器回归通过。本轮没有部署或重启服务；启用 SVG 需同时升级 Relay/Web 与目标 Desktop Agent，不能仅刷新旧部署。物理 iPhone／Safari 待用户复验。
