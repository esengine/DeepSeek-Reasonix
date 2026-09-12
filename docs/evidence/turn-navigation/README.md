# Turn navigation / 轮次导航

Ported from DeepSeek Harness c291e7961a into the existing Reasonix scroll controller.
移植右侧刻度、当前轮次高亮、悬停/键盘预览、点击/键盘跳转及独立滚动；替换顶部下拉导航。
Only loaded turns are shown. 单轮隐藏，多轮显示，分页扩展导航范围。

2026-09-12, macOS arm64, production-path fixture replay:

| Runtime | 240-turn input P95 | 1,000-turn input P95 | Max long task (1,000) | Switch P95 |
| --- | ---: | ---: | ---: | ---: |
| Chromium 153 | 73.4 ms | 129.1 ms | 248 ms | 40.4 ms |
| WebKit 26.6 | 40 ms | 87 ms | API unavailable | 48 ms |
| Electron 44.2.0 | 110.6 ms | 50.8 ms | 256 ms | 26.9 ms |

All passed hover preview, pointer and keyboard jumps, loaded-turn count,
60 streaming size changes, history prepend, disclosures, drawer restoration,
session switching and queue convergence. Stream drift 0 px; prepend 0.094 px.
Chromium released heap growth: 217,220 bytes. Raw JSON and screenshot are adjacent.

WebKit's first replay measured while native wheel animation was still moving
and failed the drift assertion. The test now waits for native movement to settle
before measuring content-induced drift; the original 2 px assertion remains.
WebKit 随后的完整回放通过，没有放宽锚点阈值或屏蔽异常。

Frontend transcript tests, build/typecheck/lint/theme/scroll-writer checks and
unchanged bundle budgets passed. Final ZIP signature and packaged renderer/service
handshake passed; normal shell/service exit passed. Native app navigation was
also exercised against the existing two-turn conversation without sending messages.

Installed: `~/Applications/Reasonix-Canary/Reasonix.app`

Profile: `~/Library/Application Support/Reasonix-Test-ChatRefactor`

ZIP SHA-256: `4b5ae2677eefcc16ab22b8f6c78bbf4ae43f3a94a71a04352bf49d010611e96d`

Native startup ready: `2026-09-12T13:03:47.765Z`, service pid 96932.
Other native WebView hosts and extended native IME soak were not rerun.
未发布、未合并 PR；其他原生 WebView 和长时间原生输入法测试未验证。
