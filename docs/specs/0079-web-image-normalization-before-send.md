# 0079：Web 发送图片前本地归一化

- 状态：Draft（等待用户确认）。
- 来源：Web 当前只按浏览器声明的 MIME 与文件字节数接收附件，Desktop Agent 发送前还会校验真实 PNG/JPEG 签名与 2500 万像素上限。高分辨率手机图片可能先成功预览、点击发送后才被 `INVALID_ARGUMENT` 拒绝；HEIC/HEIF 也无法进入现有只接受 PNG/JPEG data URI 的协议。
- 范围：在 Web 浏览器内完成附件解码、必要缩放与 PNG/JPEG 归一化；不修改 Codex、Relay 存储边界、Desktop 私有 IPC 或 4 MiB 协议总上限。

## 已确认事实

1. 当前 Codex Desktop 0.160.0 的原 owner 仍能接受图片：隔离 fixture 的 1×1 PNG 与用户此前提供的 1206×2622 PNG 均完成真实发送、回读与图片核对。
2. Web `readScreenshotFiles` 只检查 `file.type`、单文件／累计 4 MiB 与 data URI 前缀，不检查真实格式和像素数。
3. Desktop Agent `validateUploadImages` 会重新解码图片，并拒绝非 PNG/JPEG、MIME 与签名不一致、累计超过 4 MiB或超过 2500 万像素的输入。
4. 这造成“Web 已显示待发送预览，但发送时才失败”的确定性校验差异。

## 实施前可行性验证

2026-10-13 使用一次性隔离诊断链路完成了失败前后对照，未修改生产实现：

1. 构造合法 PNG：`6000 × 5000`（3000 万像素）、1,322,989 bytes。当前 Web `readScreenshotFiles` 接受该文件，但 Desktop Agent `validateUploadImages` 按 2500 万像素上限拒绝，复现了校验差异。
2. 在真实 Chromium 中执行本规格拟采用的 Canvas 路径：等比例缩到 `4096 × 3413`，以 JPEG quality 0.86 编码为 111,304 bytes。
3. 浏览器实际产物通过未修改的生产 Agent 校验，并通过真实 Desktop IPC 发送到隔离 Codex fixture；原 owner 接收并完成精确图片消息核对，测试通过。
4. 另以用户此前提供的 `1206 × 2622` PNG 验证无需归一化的正常路径，真实 owner 同样接收成功。

这证明浏览器本地缩放／编码可以修复“超过 Agent 像素上限、但 Web 先错误接受”的失败类别。Chromium 不能证明物理 iPhone/Safari 对 HEIC/HEIF 的解码能力，因此该格式仍按“浏览器能解码才转换”处理并保留物理设备复验。

## 输入与输出

- 输入：用户通过 Web 文件选择器添加的一个或多个浏览器可解码图片；一次最多 3 张。
- 输出：协议现有 `images[]` 中的 PNG/JPEG data URI；累计解码后字节数不超过 4 MiB，单图像素数不超过 Agent 上限。
- UI 输出：预览使用最终将要发送的归一化 data URI；无法解码或无法压缩到安全预算时，在附件阶段给出具体错误，不允许进入“可发送”状态。

## 行为与不变量

1. 对真实 PNG/JPEG、累计不超过 4 MiB且像素数不超过 2500 万的附件，保留原始字节，不做有损转码。
2. 对浏览器能够解码、但字节数或像素数超过发送上限的图片，在浏览器本地等比例缩放；最长边不超过 4096，最终像素数不超过 2500 万。
3. 需要压缩时输出 JPEG，并采用有界质量阶梯把整批附件压到 4 MiB 总预算内；不得无限循环或默默丢弃图片。
4. 文件选择器允许选择浏览器声明为图片的文件。HEIC/HEIF 仅在当前浏览器能够实际解码时转换为 JPEG；不能解码时明确提示，不宣称跨浏览器统一支持。
5. 浏览器只在内存中处理图片，不上传到第三方、不写 Relay 缓存、不把本机文件路径放入协议。
6. Desktop Agent 继续执行现有独立复核；Web 归一化不能取代服务端安全边界。
7. 最多 3 张、协议 data URI 长度、Relay 8 MiB frame 与 mutation 结果语义保持不变。
8. 发送失败时继续保留文字和已归一化附件；只有确认 accepted 后才清空 composer。

## 边界条件与错误处理

- 空文件、伪造 MIME、解码失败、动画或当前浏览器不支持的格式：附件阶段拒绝并给出明确原因。
- 原图超过浏览器端源文件硬上限 20 MiB，或尺寸／像素数导致安全解码预算不可接受：附件阶段拒绝，不尝试发送。
- 透明 PNG 只有在无需压缩时保留透明度；进入 JPEG 压缩路径时使用白色背景，避免透明像素变黑。
- 多图按整批剩余预算分配编码目标；任何一张无法在有界质量与尺寸阶梯内满足预算，则整次新增失败，保留原有附件列表。
- 图片归一化期间禁用重复添加该批文件，但不阻塞已存在的会话内容和草稿文字。

## TDD 与验收

1. Red：Web 测试证明当前大于 2500 万像素但小于 4 MiB 的 JPEG 会未经处理进入预览；期望改为缩放后的合规 JPEG。
2. Red：覆盖大于 4 MiB但小于 20 MiB、伪 MIME、不可解码、透明 PNG、多图总预算、最多 3 张和原有附件原子保留。
3. Green：以可注入 decoder／encoder 的纯逻辑实现预算与归一化，再接入浏览器 Canvas adapter。
4. 保留真实 Desktop 隔离 fixture：普通 PNG 发送后 owner 可核对，并补一张需要缩放的真实大图发送回归。
5. 使用真实 Chromium 手机 viewport 通过文件选择器加入大图，断言预览存在、最终 payload 为合规 PNG/JPEG、失败时草稿不丢失；物理 iPhone/Safari 的 HEIC 解码能力保留用户复验。
6. 运行 Web 全量测试、production build、Go 图片链路测试与真实 Codex fixture。

## 非目标

- 不在 Relay 或 Desktop Agent 保存附件正文。
- 不增加图片数量或 4 MiB 总发送上限。
- 不承诺浏览器无法解码的 RAW、SVG、GIF、HEIC/HEIF 格式。
- 不修改历史图片按需加载与大图预览逻辑。
