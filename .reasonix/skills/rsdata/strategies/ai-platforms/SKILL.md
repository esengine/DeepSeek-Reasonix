# ai-platforms — AI/ML 平台搜索策略

**HuggingFace、百度 AI Studio、Radiant MLHub 等平台页面 JS 渲染，web_fetch 拿不到内容。用搜索引擎摘要替代。**

## 按平台分类的搜索模板

### Hugging Face

```
web_search "site:huggingface.co {遥感关键词}"
web_search "site:huggingface.co remote sensing {任务} model"
web_search "site:huggingface.co land cover segmentation dataset"
web_search "site:huggingface.co {卫星名} pretrained"
```

摘要通常包含：模型名、下载量、更新时间、标签、模型大小。

### 百度 AI Studio

```
web_search "site:aistudio.baidu.com/datasetdetail {关键词}"
web_search "site:aistudio.baidu.com 遥感 数据集"
web_search "site:aistudio.baidu.com 土地利用 分类"
web_search "{数据集名} 百度AI Studio"
```

摘要通常包含：数据集名、大小、下载次数。

### Radiant MLHub

```
web_search "site:mlhub.earth {关键词}"
web_search "site:mlhub.earth dataset {遥感任务}"
web_search "radiant mlhub benchmark {任务}"
```

### Kaggle

```
web_search "site:kaggle.com {关键词} dataset"
web_search "kaggle remote sensing {任务} competition"
```

### 和鲸社区 (heywhale.com)

```
web_search "site:heywhale.com {关键词} 数据集"
web_search "和鲸 遥感 数据集 {关键词}"
```

### OpenDataLab

```
web_search "site:opendatalab.com {关键词}"
web_search "opendatalab 遥感 数据集"
```

## 关键原则

- 这些平台全是 JS 渲染，web_fetch 只会拿到空白骨架
- 搜索引擎摘要已有足够信息：名称、描述、下载量、更新时间
- 搜索时要中英文都搜（HuggingFace 用英文，百度AI Studio/和鲸用中文）
