# Composer 模型入口同排与状态图标验收

- 关联：Issue #17、spec 0110 的同排展示调整。
- 用户要求：模型入口与附件／发送按钮同行，推理强度、速度用 icon 展示。
- 只修改现有 Web 组件布局与展示，复用统一设置面板；没有新增 API、follower、轮询或设置写入语义。

## TDD 与设计

1. 先实际运行五项失败 App／组件测试：模型入口不在 composer-actions 内，缺少带语义的强度／速度 SVG。真实 Chromium 同排几何断言也在旧布局上失败，再开始实现。
2. 将现有入口移动到操作栏，模型名保留文字，强度使用档位条形图标；速度区分继承、标准、catalog 明确为 Fast 的档位及其他自定义档位。未知 native effort／tier 不猜测为高强度或加速。
3. 保留完整 title、SVG accessible name 和 aria-describedby；手机打开原面板可读完整选项说明，不依赖 hover。
4. 沿用既有深色 token 和 inline SVG 风格，无新增图标库。只对提供模型入口的操作栏保证 44px 点击区域；旧 Agent／无 capability 的既有布局不变。
5. 模型不兼容图片的提示移到输入区独立提示行，并以 App 测试确认保留，不把操作栏撑宽或丢失发送门禁。
6. 真实浏览器覆盖 320／390／1280px，含超长模型名、running 与 queue editing 同时出现；逐项断言同行、至少 44px、无交叠／横向溢出、草稿保留及取消／保存／键盘焦点。
7. 几何测量统一在同一浏览器采样中读取，避免 resize 时分次跨帧读取造成虚假交叠。曾有一次测试 fixture 缺失 clientMessageId，被现有严格 schema 拒绝；只修正 fixture，没有放宽生产协议。
8. 新增两张局部截图（常规手机及 320px 编辑排队输入）；既有普通页面与原设置面板截图基线不需更新。

## 结果

| 验证 | 结果 |
| --- | --- |
| Web unit | 32 文件、290 项通过 |
| Chromium viewport／touch | 57 项全量通过；布局专项连续 3 次通过 |
| Web production build | 通过，仅保留既有 chunk-size 提示 |
| Go 全量／vet | 通过；无 Go 实现改动 |
| native 兼容性 | 未改 Adapter 或写入链路，不新增真实业务写探针；沿用 0110 已验证能力 |
| 物理 iPhone／Safari | 待用户复验，不由 Chromium 外推 |
| 部署 | 本轮未执行 |
