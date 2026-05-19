# parallel-mcp

多 Agent 并行执行引擎 — MCP Server。3 个独立 Worker 并行设计方案或执行编程任务。

## 安装

```bash
pip install parallel-mcp
```

## 使用

### Reasonix / Claude Code 配置

在 `.mcp.json` 中添加：

```json
{
  "mcpServers": {
    "parallel": {
      "command": "parallel-mcp",
      "args": ["--stdio"]
    }
  }
}
```

### 工具

| 工具 | 说明 |
|------|------|
| `parallel_schedule` | 启动并行 Worker，返回 task_id |
| `parallel_status` | 查询任务进度和结果 |

### 模式

- **design**: 3 Worker (architect/security/performance) 并行设计方案，缓存 ~90%
- **code**: 1+ Worker 带文件系统 MCP，直接读写代码文件

## 缓存

| 轮次 | 命中率 |
|------|--------|
| 首轮 | 0% |
| 第2轮 | ~85% |
| 第3轮+ | ~95% |

固定 system prompt 保证高缓存复用。
