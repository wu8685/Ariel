# 0111：紧凑会话顶部验收

规格：[0111](../specs/0111-compact-conversation-header.md)；关联 Issue：#9。用户明确授权设计后直接实施。

## 结果

| 项目 | 原值 | 当前值 |
| --- | --- | --- |
| 展开 header 高度 | 66px | 52px，减少约 21%，释放 14px |
| 标题／路径字号 | 15px／11px | 14px／10px |
| 两行间距 | 4px | 2px |
| 菜单／文件／权限图标 | 19px／20px／20px | 统一 18px |
| 控件点击区域 | 44×44px | 保持 44×44px |
| 正文／输入／文件阅读字号 | 既有值 | 不变 |

仅修改会话 header 的 CSS，不改变 App 状态机、历史加载、Agent／Relay 或协议。

## TDD 与真实浏览器

1. 先更新样式契约和已有历史折叠几何断言，再新增 `compact-conversation-header.spec.ts`；实际运行 red：3 项样式测试失败，320px Chromium 测得高度仍为 66px 而非 52px。
2. 最小 CSS 实现后，样式测试 green。新浏览器 fixture 校正了关闭文件浏览器的实际 accessible name，并在窄屏点击侧栏遮罩露出的区域，不使用强制点击；长路径 fixture 确保确实触发省略。
3. 320px、390px 手机 viewport／touch 与 1280px 桌面 context 均验证：高度、字号、图标尺寸、44px 点击区域、无重叠／横向溢出、长标题／路径省略、transcript 紧贴 header，以及菜单／文件浏览／权限弹层的打开和关闭、键盘激活。
4. 新增 390px 固定截图基线；首轮全量 suite 的 16 项失败均为既有截图的布局差异。检查 diff 与实际图后更新 18 张受影响基线，未更新登录、文件原文／diff 页面、折叠 header 等未受影响基线。
5. 不使用 `--update-snapshots` 的最终完整浏览器 suite 53 项通过，包括历史折叠／恢复、键盘弹起、streaming 输入稳定性、文件原文／diff／引用及真实 Relay cache 恢复回归。

## 最终验证

| 命令 | 结果 |
| --- | --- |
| `npm --prefix web test -- --run` | 31 个文件、266 项通过 |
| `npm --prefix web run test:ui -- --workers=3` | 53 项通过 |
| `npm --prefix web run build` | 通过；仍有既有非阻断 chunk-size 提示 |
| `go test ./...` | 通过 |
| `git diff --check` | 通过 |

浏览器采用隔离 WebSocket fixture，没有向真实 Codex 业务会话发送消息或写入设置。Chromium 手机模拟不是物理 iPhone／Safari；物理设备仍待复验。本轮未执行生产部署。
