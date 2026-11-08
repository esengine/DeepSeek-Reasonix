## 修复

- 修复 Windows 上每次启动 Reasonix Studio 都会先闪一下黑色控制台窗口的问题：启动时用来确认"是不是同一个 Studio"的内核探测，以及启动内核本身，现在都隐藏控制台窗口（#15016）。

---
<!-- lang:en -->

## Fixed

- On Windows, launching Reasonix Studio no longer flashes a console window first: the kernel probe that tells whether this is the same Studio, and the kernel launch itself, now run with their console hidden (#15016).
