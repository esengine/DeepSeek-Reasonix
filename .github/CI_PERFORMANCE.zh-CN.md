# CI 与发版执行

[English](CI_PERFORMANCE.md)

## Desktop PR 检查

`desktop-prepare` 统一生成 Wails 绑定并构建一次 Linux 前端，产物供
`desktop-frontend`、`desktop-browser`、`desktop-go` 三个独立 job 使用。
原有 required check `desktop` 汇总这四项结果，拒绝失败、取消和意外跳过；
只有路径检测成功且判定无关时，才接受各分组跳过。其他原生系统检查保持独立。

`node desktop/frontend/scripts/run-ci-tests.mjs --list` 可以查看单测清单。
它展开原有专用脚本和生命周期钩子，自动发现新增测试，并保留每个 TypeScript
测试原有的 loader，每个套件只执行一次。未知命令语法或冲突调用会直接失败。
CI 同时运行两个隔离进程，历史性能基准在它们结束后单独运行。
本地仍可使用原有的 `pnpm test:*` 专用命令。

## 内存筛查

协议 v2 仍要求三个独立进程，每个完成 128 轮 full、128 轮 windowed、
128 轮 safety 和 512 轮 mixed 往返，保持相同检查点、五份堆快照、GC、
稳定帧等待与判定阈值。只有明确启用 mock 内存 soak 的 URL 会去掉夹具人为
设置的 1.5 秒 hydration 延迟；加载仍经过异步定时器任务。
普通浏览器和原生几何测试继续使用原来的延迟路径。

采样时鼠标停在会话行之外；布局切换后先完成一次会话往返，再记录预热基线，
避免采到菜单的临时状态。报告声明协议版本，汇总拒绝旧协议。
`timings.json` 在测试宿主侧记录导航、稳定帧等待、GC、堆快照与解析的次数、
总耗时和最大耗时。短程 profiling 仅用于诊断，不能通过完整筛查门禁。
筛查通过仍不等于完成离线堆引用归因。

## 签名发版产物

Stable 的 SignPath 预检成功后，将完整原生平台矩阵的产物交给 desktop
publisher。后者重新验证授权、候选身份、签名契约及缓存和文档校验，再校验
并发布同一批签名字节，不重复构建和签名。CLI 与 npm 仍等待完整预检通过，
不会提前开始公开发布。

每个平台的 bundle 将文件大小、SHA-256 与候选及控制流程 SHA、版本、tag、
channel、签名契约指纹、run、调用和生产 attempt 绑定。
平台缺失或多余、身份冲突、符号链接、重复文件名及字节变化都会阻止发布。
这些传输哈希补充既有 Authenticode/minisign 校验，不能代替签名验证。

只重跑失败 job 时，可复用同一 run 和调用中此前成功的平台；重建的平台只
替换自身已重新验证的 bundle。新的 workflow run 重新准备整套产物。
独立恢复发版仍自行构建并验证完整矩阵。pnpm 下载依赖按锁文件缓存；签名
产物通过 artifact 交接，不放入依赖缓存。
