# 0053：回到最新时稳定恢复会话界面

- 状态：Implemented（140 个 Web 单测、11 个真实 Chromium 用例、390×844 展开稳定性几何断言与截图、production build 已验；物理 iPhone/Safari 待用户复验）。
- 范围：会话历史滚动、Header 与输入区域的展开状态、布局过渡期间的底部锚定、移动端真实浏览器回归。
- 关联：[0045 阅读历史时自动收起会话 Header](0045-collapse-conversation-header-while-reading-history.md)、[0017 实时更新时保留历史阅读位置](0017-preserve-reading-position-on-live-updates.md)。

## 问题

Header 与输入区域收起后，transcript 会获得更多高度。用户把历史滚到最新时，两层开始展开，transcript 又会在 180ms 过渡期间持续变矮；如果仍用变化前的 `scrollTop` 计算底部距离，页面会被误判为再次离开最新消息，继而收起两层。收起后 transcript 重新变高，又可能再次落到底部，最终形成明显的界面抖动。

## 用户结果

用户滚到最新消息或点击“回到最新”后，Header 与输入区域只展开一次并保持展开；对话内容在两层恢复期间持续贴住最新消息，不因自身布局变化反复收放。

## 行为契约

1. 从历史阅读状态回到最新时，系统进入一次短暂的“恢复最新布局”过程；该过程覆盖 Header／输入区域现有过渡时长。
2. 恢复过程中，transcript 因 Header／输入区域展开而改变高度时，持续把视图锚定到最新消息；这些程序化几何变化不得重新触发收起。
3. 恢复完成后回到常规 80px／32px 滞回判断，不永久锁定滚动。
4. 若用户在恢复过程中明确向上滚动，立即结束底部锚定，并按普通历史阅读行为重新收起界面，不能与用户手势争夺位置。
5. 点击“回到最新”、自然滚动到底部、切换到新会话以及阅读锚点失效后回到最新，都遵循同一稳定恢复语义。
6. streaming 更新语义不变：跟随最新时继续贴底；阅读旧历史时不抢走位置。

## 验收

1. 单元测试模拟“已收起 → 滚到底部 → 展开使 transcript 高度缩小 → 再次触发 scroll”，断言两层仍保持展开且 transcript 被重新锚定到底部。
2. 单元测试覆盖恢复期间用户向上滚动可以退出锚定并重新进入历史阅读状态。
3. 390×844、touch-enabled、启用实际 CSS transition 的 Chromium fixture 中，自然滚到底部后观察至少 500ms：`data-history-collapsed` 只能发生一次 `true → false`，不得再次变为 `true`。
4. 真实浏览器断言最终 Header／输入区域可见、底部距离接近 0，并保存稳定展开状态截图。
5. 运行 Web 全量单测、全部 Playwright 用例和 production build；物理 iPhone/Safari 仍待用户复验。

## 实现结果

1. 回到最新时建立 260ms 的有界恢复窗口，覆盖现有 180ms Header／输入区域布局过渡；期间每帧重新计算 transcript 底部并保持贴底。
2. 滚动监听会区分布局恢复和用户向上滚动：前者不再触发收起，后者立即取消恢复窗口并回到普通历史阅读状态。
3. 自然滚到底部、浮动按钮、切换会话和阅读锚点失效统一复用同一恢复入口；初始已展开的会话不会建立多余的恢复锁。
4. 真实 Chromium 在启用 CSS transition 的条件下观察 550ms，`data-history-collapsed` 只发生一次 `true → false`；最终 Header 为 66px、输入区域可见、底部距离不超过 1px。
5. 新增稳定展开状态截图 `mobile-history-latest-stable-darwin.png`，人工检查未发现新的遮挡、错位或横向溢出。
