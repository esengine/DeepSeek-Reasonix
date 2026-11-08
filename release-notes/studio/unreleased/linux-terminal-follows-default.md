## 修复

- 修复 Linux 上「在终端中打开」不管你把哪个终端设成默认，总是按固定顺序挑一个（比如装了 konsole 就先用它，而不是你的 kitty）。现在如果装了 `xdg-terminal-exec`（桌面规范里读取默认终端的启动器），就由它按你机器声明的默认终端打开；没装的话仍按原来的顺序（#15031）。

---
<!-- lang:en -->

## Fixed

- On Linux, "Open in terminal" picked a terminal by a fixed order rather than the one the machine declares as its default (konsole before kitty, for example). When `xdg-terminal-exec` is installed, it now starts the machine's default terminal; without it the old order still applies (#15031).
