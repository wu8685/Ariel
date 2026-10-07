# 0056 超高分辨率会话图片预览验收

- 日期：2026-10-08
- 规格：[0056 超高分辨率会话图片按需生成安全预览](../specs/0056-large-conversation-image-preview.md)
- 结论：通过。截图中的真实 PNG 可生成安全预览，手机浏览器可加载和放大；缺失或无效图片会显示明确且不泄露本机路径的原因。

## 根因证据

失败图片不是文件丢失或 PNG 损坏：原图为 `2,038,568 bytes`、`7990 × 7225 RGBA PNG`，总计约 5773 万像素。它低于 4 MiB 压缩数据限制，却超过原会话图片读取路径与上传共用的 2500 万像素上限，因此 Desktop Agent 返回 `INVALID_ARGUMENT`；Web 又丢弃了响应原因，只显示统一的“截图未能加载”。

本记录不保存原会话文件的绝对路径。真实文件验收通过环境变量按需传入，只读运行。

## TDD 与真实文件验收

1. 先新增大图尺寸、缩放格式、来源硬上限与 Web 错误提示测试；Go 因预览函数尚不存在而编译失败，Web 因仍显示通用错误而断言失败。
2. 实现会话预览专用边界：来源不超过 6400 万像素；输出最长边不超过 4096px、总像素不超过 2500 万、压缩数据不超过 4 MiB。
3. 截图中的真实 `7990 × 7225` PNG 在只读集成测试中约 `0.8s` 完成，返回 `4096 × 3704` PNG data URI，像素与数据量均在预算内。
4. 普通图片不重复编码；上传图片继续使用原 2500 万像素限制。图片处理由 Desktop Agent 串行执行，避免多个大图请求叠加解码内存。

## Web 与视觉验收

- `INVALID_ARGUMENT`：显示“图片格式、尺寸或内容不受支持”。
- `NOT_FOUND`：显示“原图片文件已不存在”。
- `HISTORY_TOO_LARGE`：显示“图片超过安全传输上限”。
- 其他已知协议错误复用既有安全文案，未知错误不展示原始服务端消息或本机路径。
- lightbox 使用 React portal 挂载到 `body`，不会在 Markdown `<p>` 内嵌套块级 `<div>`。
- 390×844 真实 Chromium 验证了加载、等比展示、放大、关闭、缺失文件提示和无横向溢出。

视觉证据：`web/e2e/mobile-conversation-image.spec.ts-snapshots/mobile-conversation-image-preview-and-error-darwin.png`。人工查看确认预览完整、`object-fit: contain`、失败按钮可读，输入区与对话气泡没有被撑出视口。

## 全量回归

- 真实原图集成测试：通过。
- `GO111MODULE=on go test ./...`：通过。
- `GO111MODULE=on go test -race ./internal/desktopagent`：通过。
- `GO111MODULE=on go vet ./...`：通过。
- `npm test -- --run`：20 个文件、146 个测试通过。
- `npm run build`：TypeScript 与 Vite production build 通过；保留既有单 chunk 大于 500 kB 警告。
- `npm run test:ui`：12 个真实 Chromium 测试全部通过。
- `git diff --check`：通过。

## 未覆盖边界

- 物理 iPhone/Safari 仍保留用户复验；自动验收使用移动 viewport 的 Chromium。
- 超过 6400 万像素、压缩后超过 4 MiB、格式损坏或非 PNG/JPEG 的来源仍会拒绝，这是有意保留的内存与传输边界。
- 当前预览在请求内即时生成，不写磁盘缓存；若未来需要频繁反复打开同一超大图片，可另行设计受限缓存。
