## 修复

- 修复 Windows 上安装或更新被「Reasonix Studio 仍在运行」反复拦住的问题：机器上有别的应用留下的 `cua-driver.exe`（比如以管理员身份运行、或父进程已退出的孤儿进程）时，安装器会把它当成 Studio 本体，点多少次重试都过不去。现在只按名字匹配 Studio 自己的程序，不再匹配这个多个应用共用的驱动（f1bbea388）。

---
<!-- lang:en -->

## Fixed

- Fixed the Windows installer and updater being blocked forever by "Reasonix Studio is still running". A `cua-driver.exe` left behind by another application (for example one running as administrator, or an orphan whose parent exited) was taken for Studio itself, so Retry could never succeed. Only Studio's own programs are matched by name now, not the driver other applications share (f1bbea388).
