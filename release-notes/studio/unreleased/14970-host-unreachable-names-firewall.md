## 修复

- Windows 上防火墙或安全软件拦截了本机入站连接时，Studio 不再等 21 秒后只报一个看不懂的 ERR_CONNECTION_TIMED_OUT，而是几秒内告诉你内核在运行但连不上它，并说明要放行 Reasonix Studio 和 reasonix-studio-host（#14970）。

---
<!-- lang:en -->

## Fixed

- When a Windows firewall or security product blocks the local inbound connection, Studio no longer waits 21 seconds and shows only ERR_CONNECTION_TIMED_OUT; within seconds it says the kernel is running but unreachable and which programs to allow (#14970).
