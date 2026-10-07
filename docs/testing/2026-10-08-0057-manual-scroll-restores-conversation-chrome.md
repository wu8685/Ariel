# 0057 手动滚到最新恢复会话界面验收

- 日期：2026-10-08
- 规格：[0057 手动滚到最新时可靠恢复会话界面](../specs/0057-manual-scroll-restores-conversation-chrome.md)
- 结论：通过。手动滚到最新后 Header 与输入区域会恢复，并在展开过渡期间持续贴住最新消息。

## 根因与红灯

折叠分支原先使用下面的单事件条件识别恢复动作：

```text
currentScrollTop > previousScrollTop + 1
```

手机惯性滚动或亚像素滚动可能先大幅向最新移动，但最后只用 1px 跨入底部 32px 恢复区间。此时方向条件为 false，界面继续折叠；“回到最新”按钮直接调用恢复函数，不经过这项判断，因此表现正常。

新增测试先稳定复现了两个失败场景：

1. 向最新移动到恢复区间外，再以最后 1px 跨入区间，Header 与输入区域仍然折叠。
2. transcript 已在底部、`scrollTop` 无法继续增加时，向最新方向的触摸手势不能恢复。

两项测试在实现前均按预期失败，实现后转绿。

## 实现与边界

1. 连续 `scroll` 事件共享最近方向意图：正增量标记向最新，负增量标记向历史，零增量保留方向。
2. wheel 向下与触摸向上显式标记向最新；反向动作清除意图。这覆盖了视口扩张后已经到达底部、没有新 scroll 增量的情况。
3. 到达 32px 恢复区间后复用既有 `restoreLatestChrome`，没有建立第二套展开状态。
4. 260ms 恢复窗口保留向最新意图，避免 Header／输入区域展开造成的布局调整被误认成用户上滚；反向 wheel／touch 会立即结束锚定。
5. 初始打开、切换会话和恢复结束都会清理旧意图，streaming 与阅读锚点语义不变。

## 自动与视觉验收

- `npm test -- --run`：20 个文件、148 个测试通过。
- `npm run build`：TypeScript 与 Vite production build 通过；保留既有单 chunk 大于 500 kB 警告。
- `npm run test:ui`：13 个真实 Chromium 测试全部通过。
- `GO111MODULE=on go test ./...`：通过。
- `GO111MODULE=on go vet ./...`：通过。
- `git diff --check`：通过。

新增真实浏览器用例在 390×844、touch-enabled viewport 中执行“底部外 33px → 最后 1px 进入 32px 区间”，并等待 550ms 验证：

- `data-history-collapsed` 只发生一次 `true → false`；
- Header 最终高度为 66px；
- 输入区域可见；
- transcript 到最新消息的距离不超过 1px。

视觉证据：`web/e2e/mobile-history-header.spec.ts-snapshots/mobile-history-manual-latest-restored-darwin.png`。人工检查确认 Header、最新消息与输入区域同时可见，“回到最新”浮动按钮已消失，没有横向溢出。

## 未覆盖边界

物理 iPhone/Safari 的真实惯性曲线仍需用户复验；自动测试覆盖了导致原问题的事件边界和移动浏览器布局变化，但不冒充物理设备测试。
