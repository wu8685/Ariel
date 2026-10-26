# 0038：排队项双向拖拽排序

- 状态：Implemented（pointer capture／lost capture 回归、390×844 Chromium touch E2E、截图与 production build 已验；物理 iPhone/Safari 待用户复验）。
- 范围：Web 与手机端排队项左侧手柄的 pointer 拖拽排序。

## 输入与输出

- 输入：用户按住任一排队项左侧手柄，并将指针向上或向下越过其他排队项的纵向中点。
- 输出：界面实时预览完整新顺序；松手后通过 `queue.reorder` 提交每个 queue ID 一次，顺序与预览一致。

## 行为与不变量

1. 向上拖动可使排队项提前，向下拖动可使排队项后移，包括从队首拖到队尾。
2. 插入位置按排队项实际几何位置和纵向中点计算，不依赖 pointer capture 期间的 `elementFromPoint()` 命中结果。
3. 未越过相邻项中点时不改变顺序；取消拖拽时不提交。
4. 键盘上移／下移、更多菜单、Codex durable queue SSOT 与完整顺序校验保持不变。

## 边界与错误处理

- 指针移到列表上方时最多移动到队首，移到列表下方时最多移动到队尾。
- DOM 行数据缺失、拖拽 ID 已不在当前队列或队列在拖拽期间被外部更新时，不构造不完整顺序；沿用既有同步保护。
- `queue.reorder` 未确认成功时，仍以 Codex 返回的权威队列和既有错误提示为准。

## TDD 与验收

1. 增加回归测试：模拟 pointer capture 使 hit-test 始终返回被拖项，验证队首仍可向下拖到队尾。
2. 保留既有键盘向下重排测试，并覆盖纯排序计算的上下边界。
3. Web 全量测试与 production build 通过；推送后重启本地 Ariel，并验证 LAN health endpoint。

## 非目标

- 不改变 queue 协议、执行语义或 Codex owner 边界。
- 不增加 Ariel 自有队列，也不实现跨长列表自动滚动。

## 2026-10-27 补充：拖拽排队项不选择展示文本

- 状态：Implemented（用户明确要求排队项展示文字不能选中；主题与真实浏览器先 red 后 green，Web 292／Chromium 60／production build 通过；未部署，物理 Safari 待复验）。
- 输入／输出：排队项的长按、文字拖动和手柄拖拽均不产生文本选区或系统复制浮层；左侧手柄继续拖拽排序，更多菜单继续提供编辑等原操作。
- 实现边界：仅对 `.queue-item` 设置 `user-select: none`、`-webkit-user-select: none` 和 `-webkit-touch-callout: none`，覆盖摘要及附加说明；不扩大到整个 composer／页面，不清空全局 Selection，不新增全局事件或改变拖拽门槛。`touch-action: none` 仍只属于手柄，不阻断列表正常滚动。
- 不变量：底部编辑输入框仍能选择、复制、修改文本；正文和文件内容的选择能力、队列顺序协议、pointercancel、键盘排序、按钮功能和尺寸均不变。
- TDD 验收：先以主题测试和真实 Chromium 鼠标选择复现旧文本可选，再修改局部 CSS；390／1280px 验证排队文字不可选、正文可选、菜单编辑回填后可选字修改，390px touch 链路验证长按手柄后跨文字拖动只提交一次完整新顺序且没有选区。全量 Web／浏览器／build 回归，截图无视觉变化；物理 iPhone／Safari 的复制浮层需用户复验。
- 实施证据：旧版 Chromium 实际鼠标选择产生“第一条：检查移动端输入”等非空选区；修改后同一用例选区为空，正文仍可选，编辑回填后可按片段替换输入。CDP touch 按住手柄 650ms 后跨文字拖动只提交一次完整顺序。现有截图基线全部直接通过，无图片更新。
