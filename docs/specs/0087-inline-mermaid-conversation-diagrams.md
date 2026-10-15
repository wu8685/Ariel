# 0087：会话内 Mermaid 图表渲染

- 状态：Implemented（用户于 2026-10-16 确认；219 个 Web 单测、33 个真实 Chromium 用例、production build 与依赖审计已验；物理 iPhone/Safari 待用户复验）。
- 范围：Web 会话消息中的 Mermaid 识别、SVG 渲染、移动端浏览、流式更新与失败降级。
- 不涉及：服务端生成图片、Relay 持久化图表、编辑 Mermaid、导出 PNG、执行任意 HTML/JavaScript。

## 问题

当前会话正文只经 `react-markdown` 与 GFM 渲染。模型直接输出的裸 Mermaid（例如以 `flowchart TD` 或 `sequenceDiagram` 开头、未包裹在 ` ```mermaid ` code fence 中）会被当作普通文字；标准 Mermaid code fence 也只会显示为代码块，不能形成图像。

实际回复还可能在图表之后继续输出“核心结论”等普通 Markdown。Web 必须只把 Mermaid 部分交给图表引擎，不能吞掉、改写或隐藏后续正文。

## 用户结果

会话中出现 Mermaid 时，Web 就地显示为清晰、可缩放浏览的 SVG 图表；图表后的解释文字继续按 Markdown 展示。用户不需要手工补 code fence。图表还在 streaming、语法错误或渲染器不可用时，原始内容仍然可读。

## 输入

支持两种输入：

1. 标准 fenced block：` ```mermaid ... ``` `。
2. 裸 Mermaid block：某一行以受支持的 Mermaid diagram type 开始，后续连续 Mermaid 语句构成图表；遇到首个不属于当前 diagram grammar 的普通正文行时结束。

首版支持 Ariel 对话中常见的 diagram type：

- `flowchart` / `graph`
- `sequenceDiagram`
- `stateDiagram` / `stateDiagram-v2`
- `classDiagram`
- `erDiagram`
- `journey`
- `gantt`
- `pie`
- `mindmap`
- `timeline`

裸 block 的切分必须至少覆盖以下边界：

- flowchart 的节点、边、edge label、`subgraph`、`end`、`direction`、`style`、`classDef`、`class`、`click` 与注释；
- sequence diagram 的 participant、actor、message、Note、loop/alt/opt/par/critical/break/rect、activate/deactivate 与相应结束语句；
- Mermaid 内部空行不结束图表；
- 示例中的 `核心结论：` 及其后正文不得进入图表源码。

无法可靠确定裸 block 结束位置时，宁可保留为普通文本，不得误吞正文。标准 fenced block 不需要启发式切分。

## 行为契约

1. Markdown 解析先把 Mermaid block 转为专用 React component，其余内容继续走现有安全 Markdown 投影。
2. Mermaid 在浏览器端渲染为内联 SVG，不向 Relay 上传图表源码，不发起外部网络请求。
3. Mermaid 使用严格安全模式：禁用任意 HTML label、脚本执行和外部资源加载；生成的 SVG 不允许 `script`、`foreignObject`、事件处理属性或危险 URL。
4. 每个图表拥有稳定且互不冲突的 DOM id；同一消息中的多图和不同消息中的同名节点不得互相污染。
5. 图表渲染为异步加载：非 Mermaid 消息不下载 Mermaid runtime；图表加载时显示与消息字体一致的轻量占位，不阻塞其他正文。
6. 图表 source 变化时丢弃旧异步结果，避免 streaming 期间旧 SVG 覆盖新内容。
7. fenced Mermaid 在闭合 fence 前按代码显示；裸 Mermaid 在语法尚不完整或渲染失败时显示完整源码和“图表暂时无法渲染”提示，不隐藏内容。
8. 单个 diagram 设置有界限制（源码长度、行数或渲染超时）；超限时降级源码，避免一条消息冻结页面。
9. SVG 宽度不得撑破消息气泡或页面。桌面端按容器缩放；移动端在保持可读性的前提下允许图表容器独立横向滚动。
10. 图表提供 `role="img"` 与可读 label；键盘用户可聚焦横向滚动区域。动画遵守 `prefers-reduced-motion`。
11. 现有 Markdown 安全链接、图片占位、表格和超长正文降级行为保持不变。

## 错误处理

- Mermaid runtime 加载失败、parse 失败、render 失败或超时：显示原始 Mermaid source，不抛到整条消息的 Markdown error boundary。
- SVG 安全检查失败：拒绝注入 SVG并显示源码。
- 裸 block 切分不确定：按普通 Markdown/纯文本显示原内容。
- 消息超过现有 Markdown 总长度上限：继续沿用整条消息纯文本降级，不尝试 Mermaid。

## TDD 与验收

1. Red：先增加单测，证明当前实现不会渲染 fenced Mermaid 和用户示例中的裸 `flowchart TD`。
2. 单测覆盖：
   - 用户给出的完整 flowchart 成功形成一个图表，`核心结论` 与后续段落仍作为正文；
   - fenced Mermaid、多图、同名节点、streaming source 更新；
   - 非 Mermaid code fence 不受影响；
   - 语法错误、超限、runtime error 与危险 SVG 均安全降级；
   - 现有 Markdown 测试保持通过。
3. Green：实现最小 block 识别、lazy Mermaid renderer、安全 SVG 投影和样式。
4. 真实浏览器回归：在桌面与 390×844 手机 viewport 中打开包含用户示例的隔离 fixture，断言 SVG 可见、正文未丢、页面无横向溢出、图表容器可独立滚动。
5. 保存一张手机 viewport 截图并人工检查中文 label、连线、节点文本和发送区无重叠。
6. 运行 Web 全量单测、相关 Playwright 用例与 production build。
7. 物理 iPhone/Safari 仍由用户复验；Chromium 手机 viewport 不宣称等同物理 Safari。

## 实现结果

1. 新增裸 Mermaid 分块器：识别常见 diagram type，保护既有 code fence，并在用户示例的 `核心结论：` 前结束 flowchart，后续正文继续由 Markdown 渲染。
2. fenced 与裸 Mermaid 共用 lazy-loaded Mermaid 12.2.0 renderer；普通消息不触发 runtime 下载。渲染使用 strict security、关闭 HTML label，并对最终 SVG 再检查危险元素、事件属性和外部 URL。
3. 图表加载、语法错误、超限、超时与危险 SVG 均有独立状态；失败只降级当前图表源码，不触发整条消息的 Markdown fallback。
4. streaming source 更新会取消旧结果写入，避免较慢的旧 SVG 覆盖新内容；图表 DOM id 按 React 实例隔离。
5. 复杂图按有界自然宽度展示，手机端只在图表容器内横向滚动；初始视口优先居中首个流程节点，页面本身不产生横向溢出。
6. 新增桌面与 390×844 手机真实 Chromium fixture，直接使用本 spec 的完整中文 DCG flowchart；移动截图已人工检查，正文保留、SVG 可见、容器滚动和页面几何均通过。
7. 最终验证：27 个 Vitest 文件共 219 项通过，33 项 Playwright 用例通过，production build 通过，`npm audit --omit=dev` 为 0 vulnerabilities。
