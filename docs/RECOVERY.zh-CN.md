# 恢复与诊断（v1.20+）

Reasonix 不再提供产品化的 `reasonix-guard` 恢复壳。崩溃记录、pending 更新状态
和配置问题**不会**在下次启动时强制进入全局安全模式。

## 请使用这些命令

```text
reasonix doctor
reasonix doctor repair
reasonix crash report   # 视构建是否包含而定
```

- **doctor**：检查配置、桌面派生状态与常见安装问题，不加载 Wails。
- **doctor repair**：在用户明确选择后做安全修复。
- 崩溃上报仍为用户授权后发送，且不会改变下次启动模式。

## 安装布局（v1.20+）

Windows / Linux 使用版本目录：

```text
InstallRoot/
  reasonix-launcher[.exe]
  Reasonix.exe                 # Windows 便携/开始菜单别名
  reasonix[-cli.exe]
  current.json
  versions/<version>/
    reasonix-desktop[.exe]
    reasonix-cli[.exe]
    reasonix-update-helper[.exe]
```

薄启动器只读取 `current.json` 并启动当前 Desktop，**不会**自动选旧版本或进入
安全模式。

## 从 1.18–1.19.1 升级

若旧客户端卡在 pending update 或安全模式循环：

1. 从官方下载页获取最新签名安装包。
2. 直接运行一次（Windows：双击即可；无需卸载或手工删 JSON）。
3. 兼容载荷中可能仍带有名为 `reasonix-guard` 的**一次性迁移程序**，它只把旧平铺
   布局写成 `current.json` 后自删除，并非旧 Guard 产品。

正式恢复路径**不要求**手工删除 `pending-update.json`、锁文件或 AppData。

## macOS

macOS 仍由 LaunchServices 直接启动 Wails App 包；更新原子替换签名 `.app`，无
Guard 进程。
