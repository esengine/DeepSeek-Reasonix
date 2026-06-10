# Patent Architect Reference

## SerpAPI Google Patents (Structured Search)

### Endpoint

```
GET https://serpapi.com/search.json?engine=google_patents
```

### Parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| `engine` | Yes | Must be `google_patents` |
| `api_key` | Yes | Your SerpAPI key |
| `q` | Yes | URL-encoded Boolean query |
| `num` | No | Results per page (10-100) |
| `sort` | No | `new` or `old` |
| `language` | No | `ENGLISH`, `CHINESE`, etc. |
| `status` | No | `GRANT` or `APPLICATION` |

### Query Syntax

```
(machine learning) AND (recommendation)
(AI) OR (artificial intelligence)
assignee:(Google)
inventor:(Smith)
```

### Response Structure

```json
{
  "organic_results": [
    {
      "publication_number": "US12345678B2",
      "title": "Patent Title",
      "snippet": "Description...",
      "priority_date": "2020-01-15",
      "assignee": "Tech Corp",
      "inventor": "John Smith"
    }
  ]
}
```

### Example

```bash
curl -s "https://serpapi.com/search.json?engine=google_patents&q=(machine%20learning)%20AND%20(recommendation)&api_key=KEY&num=20"
```

---

## Exa.ai Search API (Semantic Search)

### Endpoint

```
POST https://api.exa.ai/search
```

### Headers

| Header | Required | Value |
|--------|----------|-------|
| `x-api-key` | Yes | Your Exa.ai API key |
| `Content-Type` | Yes | `application/json` |

### Request Body

```json
{
  "query": "semantic description of the invention",
  "type": "neural",
  "numResults": 20,
  "includeDomains": ["patents.google.com", "patentscope.wipo.int", "espacenet.com"],
  "contents": {
    "text": true,
    "summary": true
  }
}
```

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `query` | string | Natural language search query |
| `type` | enum | `neural` (semantic), `fast`, `auto`, `deep` |
| `numResults` | int | Results to return (max 100) |
| `includeDomains` | array | Restrict to specific domains |
| `excludeDomains` | array | Exclude domains |
| `category` | enum | `research paper`, `pdf`, `news`, etc. |
| `startPublishedDate` | string | ISO 8601 date filter |
| `contents.text` | bool | Include full text |
| `contents.summary` | bool | Include AI summary |

### Response Structure

```json
{
  "results": [
    {
      "title": "Patent Title",
      "url": "https://patents.google.com/patent/US12345678B2",
      "publishedDate": "2023-01-15",
      "summary": "AI-generated summary of the patent...",
      "text": "Full patent text content..."
    }
  ]
}
```

### Patent Domains

Use these in `includeDomains` for patent search:
- `patents.google.com` - Google Patents
- `patentscope.wipo.int` - WIPO PatentScope
- `espacenet.com` - European Patent Office
- `patents.justia.com` - Justia Patents

### Example Request

```bash
curl -X POST 'https://api.exa.ai/search' \
  -H 'x-api-key: YOUR_KEY' \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "machine learning calendar scheduling focus time recommendation",
    "type": "neural",
    "numResults": 10,
    "includeDomains": ["patents.google.com"],
    "contents": {"summary": true}
  }'
```

---

## Application Form Format

See `template.md` for the complete structural template. Key format requirements are provided below.

### Required Sections

All patent applications must include: Background Technology (背景技术), Search Analysis (检索分析), Invention Content (发明内容), Specific Implementation Modes (具体实施方式), and Others (其他).

### Implementation Requirements

Provide at least 3 embodiments with variations in:
- 数据流向 (push/pull, sync/async)
- 触发条件 (time-based, event-based, threshold-based)
- 模块边界 (monolithic, distributed, edge-cloud)
- 处理位置 (local, remote, edge)

---

## Language Conventions

### 避免使用

| 类型 | 示例 |
|------|------|
| 产品名称 | iPhone, MacBook, Galaxy |
| UI 术语 | 按钮, 页面, 弹窗, 下拉框 |
| 品牌名称 | Google, Apple, Microsoft |
| 口语化 | 然后, 接着, 之后 |

### 应该使用

| 类型 | 示例 |
|------|------|
| 设备 | 移动终端设备, 便携式计算设备, 显示装置 |
| UI 元素 | 用户交互元素, 显示界面, 输入组件, 选择控件 |
| 通用术语 | 处理单元, 存储模块, 通信接口 |
| 专利表述 | 响应于, 根据, 基于, 用于 |

### Standard Phrases

- `一种...` - 发明名称开头
- `包括/包含` - 描述组成部分
- `用于...` - 描述功能目的
- `其特征在于` - 引出创新点
- `所述...` - 指代前文元素
- `根据...确定...` - 逻辑关系
- `响应于...` - 触发条件
- `配置为...` - 模块功能
