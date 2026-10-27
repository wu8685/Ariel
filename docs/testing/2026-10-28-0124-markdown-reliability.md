# 0124：Markdown 公式、代码与图片验收

- 日期：2026-10-28。
- 规格：[0124](../specs/0124-markdown-math-code-and-image-reliability.md)，用户已确认。
- Issue：#52。
- 状态：全部自动验收通过；未部署、未修改真实 Codex 会话或文件。

## 1. 根因与修复

| 项目 | 已证实原因 | 实施 |
| --- | --- | --- |
| 公式原始命令／转义异常 | 两处 Markdown 缺少数学解析；普通 CommonMark 会消费反斜杠定界符 | 在 micromark 阶段识别四种定界符，保留原文位置，按需加载 KaTeX 与本地字体 |
| 伪代码排版 | 围栏内容未丢失，但文件／会话样式不一致、浏览器默认 Tab 为 8 列 | 共用等宽排版、4 列 Tab、保留空行与缩进；代码块内滚动 |
| 会话图片仅占位 | 未匹配 native metadata 的图片没有加载入口 | 公网 HTTPS 显式域名许可、局部加载／失败／重试；未知本地来源明确拒绝 |
| 图片读取被打断 | 每次渲染重新创建 Markdown 图片 component，导致卸载与取消；可见性反复变化也会清空结果 | 稳定 component＋context，进入近视口后完成当前读取，不因无关 rerender／瞬时离屏反复取消 |
| 损坏图片没有反馈 | 创建 Blob URL 被视作完成，没有浏览器解码状态或重试 | 区分读取、解码、成功、失败；重试前仅淘汰已释放的坏 cache 项，不回收仍被使用的 URL |
| 局域网图片校验失败 | 非 secure context 可能没有 `crypto.subtle`，原实现直接访问 `.digest` | 原生 Web Crypto 优先；不可用时 lazy 加载本地 SHA-256 实现，仍拒绝 digest 不符 |
| 模式恢复后图片变占位 | 曾读取的图片已经在内存中，但回到阅读页后离屏等待新的 IntersectionObserver，改变页面高度 | 当前激活文档可直接重获已有 cache lease；未看过的离屏图片仍不请求，缓存容量不扩大 |
| 异步公式与位置恢复竞争 | 初始 TeX 占位比排版结果短，旧位置被过早 clamp | 等待数学 DOM 与字体后再应用原阅读锚点；wheel／touch／pointer／keyboard 输入后取消自动恢复 |

数学节点不参与普通文件文本 offsets 映射，跨公式／图片的选区明确拒绝，原版仍可精确引用。KaTeX 只接收有限 TeX，关闭 trust、限制展开与尺寸，错误保留 React 转义的原文；不启用 raw HTML、任意 URL 下载或执行插件。

## 2. Red 证据

1. `npm test -- src/markdown-reliability.test.tsx`：第一批 10 项全部失败，分别落在无数学 DOM、无图片许可／重试、无解码状态，以及 rerender 丢失 pending 读取。
2. 新增已释放图片 invalidation 测试先失败：原 Session 不存在该操作；随后验证被使用的 URL 不被回收，释放后重试获取新 URL。
3. Chromium 390px 代码测试先失败：computed `tab-size` 为 `8`，预期为 `4`。
4. 补充完整 streaming 原文断言先失败：`\[` 在片段未闭合时被转义消费；补齐后须正常变为公式。
5. 超宽公式浏览器测试先失败：390px 视口内行内公式右界达到约 903px；随后验证容器可独立滚动且页面不溢出。
6. 关闭 Web Crypto 的图片测试先失败：`Cannot read properties of undefined (reading 'digest')`；fallback 成功路径与错误 digest 拒绝路径随后通过。
7. 不触发 IntersectionObserver 的已缓存图片重用测试先失败：仍停在“图片按需加载”，找不到 `<img>`。
8. 刷新时故意阻塞数学 JS chunk 的 Chromium 测试先失败：排版完成后没有恢复原底部锚点；修复后同时验证用户新滚动不会被旧锚点覆盖。
9. 收尾 review 增补 A → B → A 内容变化测试先失败：回到相同旧文字时旧图片许可可能复活。现以真正的消息版本生命周期销毁许可状态，单测与浏览器同时覆盖，内容回退也必须重新允许，不自动请求外站。

没有用户实际异常文档样例，以上为代码事实与隔离合成 fixture 的复现；不能据此宣称所有外站鉴权、Obsidian 图片或用户特定文档均已验证。

## 3. 浏览器与回归范围

- 新增 `web/e2e/markdown-reliability.spec.ts`：320／390／1280px 的公式／伪代码截图、同源字体、精确普通文字引用、慢图＋滚动＋许可 rerender、503 后重试、坏 PNG 真实解码失败后重试、无 Web Crypto、会话图片许可／重试／消息版本重置、超宽公式，以及刷新延迟排版／用户接管滚动。
- 图片成功断言读取 `HTMLImageElement.naturalWidth`，不只验证 DOM 存在。坏 PNG fixture 只保留有效头部，使字节验证与浏览器解码明确分离。
- 原 `document-reading.spec.ts` 的位置用例等待基础图片解码后再测量，保留严格的有效位置／clamp 断言；没有添加任意像素容差。模式恢复的实际缓存问题也由新增失败用例与实现一起修复。
- 原阅读截图仅随明确的图片许可文案更新；新公式／代码截图和既有图片截图均人工查看，不以更新快照替代行为验收。
- 全量测试仍包含原位续登录、重连 fencing、5 秒 streaming、真实隔离 Agent／Relay、150 个文件 Tab、刷新与选区恢复、原版／Diff／native 图片、文件三个入口和精确引用。没有新增 Codex 私有方法或协议，native 能力状态不因 Web 测试变成新的 supported 声明。

## 4. 执行命令

```sh
cd web
npm test
npm run build
npm run test:ui -- --workers=1
```

```sh
go test ./...
go vet ./...
go test -race ./internal/filecontext ./internal/relay
git diff --check
```

最终结果：363 项 Web 单测、136 项 Chromium（包含 20 项文件／Markdown 专项）、build、Go 全量、相关 filecontext／Relay race 与 vet 均通过。全量浏览器在不更新快照的模式下通过，`git diff --check` 无空白错误。

## 5. 未验项与边界

- Chromium 手机 viewport／touch 不是物理 iPhone／Safari，后者待用户复验。
- 公网 HTTPS 图片需显式许可；目标站点的登录／防盗链／CSP／浏览器限制不被代理绕过。未匹配 native 证明的会话本地路径、HTML img、Obsidian wikilink 不新增支持。
- 单文档 256 Ki UTF-16、单公式 16 Ki／256 个／1000 次宏展开，图片 4 MiB／12 MP／8192px、两并发／32 等待／16 MiB 预算不变。文档／图片不写 Relay cache、磁盘或浏览器 checkpoint，不依赖 Chilling。
- 数学 JS／CSS／字体和无 Web Crypto fallback 都随静态资源发布；不是第三方公式服务，也不新增后台同步或业务写入。
- `npm audit` 仍报告基线已有的 fast-uri（ajv，moderate）、undici（jsdom，high）与 Vite（开发依赖，high）告警；本次新增依赖没有新增告警。未用 `audit fix --force` 混入工具链升级，另记维护待办。
- 本轮只实现并提交／push 功能分支；不创建版本 tag、不更新公网容器、不重启本机 Agent。
