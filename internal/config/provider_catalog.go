package config

// ProviderCatalog describes the display hierarchy only. Existing preset IDs,
// credentials and provider entries remain the installation authority.
type ProviderCatalog struct {
	BrandID    string                              `json:"brandId"`
	BrandLabel string                              `json:"brandLabel"`
	Region     string                              `json:"region"`
	Product    string                              `json:"product"`
	Format     string                              `json:"format"`
	BaseURL    string                              `json:"baseUrl"`
	Protocols  map[string]ProviderProtocolEndpoint `json:"protocols,omitempty"`
}

var providerCatalog = map[string]ProviderCatalog{
	"doubao-chat":         {BrandID: "doubao", BrandLabel: "火山引擎 / 豆包", Region: "cn", Product: "api"},
	"doubao-responses":    {BrandID: "doubao", BrandLabel: "火山引擎 / 豆包", Region: "cn", Product: "api"},
	"baidu-cloud":         {BrandID: "baidu", BrandLabel: "百度智能云 / 千帆", Region: "cn", Product: "api"},
	"ppio":                {BrandID: "ppio", BrandLabel: "PPIO / 派欧云", Region: "cn", Product: "api"},
	"qiniu":               {BrandID: "qiniu", BrandLabel: "七牛云", Region: "cn", Product: "api"},
	"xai-chat":            {BrandID: "xai", BrandLabel: "xAI / Grok", Region: "global", Product: "api"},
	"xai-responses":       {BrandID: "xai", BrandLabel: "xAI / Grok", Region: "global", Product: "api"},
	"cerebras":            {BrandID: "cerebras", BrandLabel: "Cerebras", Region: "global", Product: "api"},
	"together":            {BrandID: "together", BrandLabel: "Together AI", Region: "global", Product: "api"},
	"fireworks-chat":      {BrandID: "fireworks", BrandLabel: "Fireworks AI", Region: "global", Product: "api"},
	"fireworks-anthropic": {BrandID: "fireworks", BrandLabel: "Fireworks AI", Region: "global", Product: "api"},
	"fireworks-responses": {BrandID: "fireworks", BrandLabel: "Fireworks AI", Region: "global", Product: "api"},

	"amd-gpu-cloud":                     {BrandID: "amd", BrandLabel: "AMD GPU Cloud", Region: "cn", Product: "api"},
	"deepseek-chat":                     {BrandID: "deepseek", BrandLabel: "DeepSeek", Region: "global", Product: "api"},
	"openai-responses":                  {BrandID: "openai", BrandLabel: "OpenAI", Region: "global", Product: "api"},
	"openai-chat":                       {BrandID: "openai", BrandLabel: "OpenAI", Region: "global", Product: "api"},
	"siliconflow":                       {BrandID: "siliconflow", BrandLabel: "SiliconFlow", Region: "cn", Product: "api"},
	"ollama-local":                      {BrandID: "ollama", BrandLabel: "Ollama", Region: "local", Product: "local"},
	"ollama-cloud":                      {BrandID: "ollama", BrandLabel: "Ollama", Region: "global", Product: "api"},
	"lmstudio":                          {BrandID: "lmstudio", BrandLabel: "LM Studio", Region: "local", Product: "local"},
	"deepseek-anthropic":                {BrandID: "deepseek", BrandLabel: "DeepSeek", Region: "global", Product: "api"},
	"longcat-openai":                    {BrandID: "longcat", BrandLabel: "LongCat", Region: "global", Product: "api"},
	"longcat-anthropic":                 {BrandID: "longcat", BrandLabel: "LongCat", Region: "global", Product: "api"},
	"kimi-cn":                           {BrandID: "kimi", BrandLabel: "Moonshot / Kimi", Region: "cn", Product: "api"},
	"kimi-global":                       {BrandID: "kimi", BrandLabel: "Moonshot / Kimi", Region: "global", Product: "api"},
	"kimi-coding-plan":                  {BrandID: "kimi", BrandLabel: "Moonshot / Kimi", Region: "global", Product: "coding"},
	"mimo-api":                          {BrandID: "mimo", BrandLabel: "MiMo", Region: "global", Product: "api"},
	"mimo-anthropic":                    {BrandID: "mimo", BrandLabel: "MiMo", Region: "global", Product: "api"},
	"mimo-token-plan-cn":                {BrandID: "mimo", BrandLabel: "MiMo", Region: "cn", Product: "token"},
	"mimo-token-plan-cn-anthropic":      {BrandID: "mimo", BrandLabel: "MiMo", Region: "cn", Product: "token"},
	"mimo-token-plan-sgp":               {BrandID: "mimo", BrandLabel: "MiMo", Region: "sgp", Product: "token"},
	"mimo-token-plan-sgp-anthropic":     {BrandID: "mimo", BrandLabel: "MiMo", Region: "sgp", Product: "token"},
	"mimo-token-plan-ams":               {BrandID: "mimo", BrandLabel: "MiMo", Region: "ams", Product: "token"},
	"mimo-token-plan-ams-anthropic":     {BrandID: "mimo", BrandLabel: "MiMo", Region: "ams", Product: "token"},
	"minimax-cn-api":                    {BrandID: "minimax", BrandLabel: "MiniMax", Region: "cn", Product: "api"},
	"minimax-global-api":                {BrandID: "minimax", BrandLabel: "MiniMax", Region: "global", Product: "api"},
	"minimax-cn-anthropic":              {BrandID: "minimax", BrandLabel: "MiniMax", Region: "cn", Product: "api"},
	"minimax-global-anthropic":          {BrandID: "minimax", BrandLabel: "MiniMax", Region: "global", Product: "api"},
	"deepseek-responses":                {BrandID: "deepseek", BrandLabel: "DeepSeek", Region: "global", Product: "api"},
	"glm-cn":                            {BrandID: "zai", BrandLabel: "智谱 / Z.AI", Region: "cn", Product: "api"},
	"zai-global":                        {BrandID: "zai", BrandLabel: "智谱 / Z.AI", Region: "global", Product: "api"},
	"glm-coding-plan-cn":                {BrandID: "zai", BrandLabel: "智谱 / Z.AI", Region: "cn", Product: "coding"},
	"glm-coding-plan-cn-anthropic":      {BrandID: "zai", BrandLabel: "智谱 / Z.AI", Region: "cn", Product: "coding"},
	"zai-coding-plan-global":            {BrandID: "zai", BrandLabel: "智谱 / Z.AI", Region: "global", Product: "coding"},
	"zai-coding-plan-global-anthropic":  {BrandID: "zai", BrandLabel: "智谱 / Z.AI", Region: "global", Product: "coding"},
	"opencode-go":                       {BrandID: "opencode", BrandLabel: "OpenCode", Region: "global", Product: "go"},
	"opencode-zen-anthropic":            {BrandID: "opencode", BrandLabel: "OpenCode", Region: "global", Product: "zen"},
	"qwen-cn":                           {BrandID: "qwen", BrandLabel: "阿里云百炼 / Qwen", Region: "cn", Product: "api"},
	"qwen-global":                       {BrandID: "qwen", BrandLabel: "阿里云百炼 / Qwen", Region: "global", Product: "api"},
	"qwen-coding-plan-cn":               {BrandID: "qwen", BrandLabel: "阿里云百炼 / Qwen", Region: "cn", Product: "coding"},
	"qwen-coding-plan-cn-anthropic":     {BrandID: "qwen", BrandLabel: "阿里云百炼 / Qwen", Region: "cn", Product: "coding"},
	"qwen-coding-plan-global":           {BrandID: "qwen", BrandLabel: "阿里云百炼 / Qwen", Region: "global", Product: "coding"},
	"qwen-coding-plan-global-anthropic": {BrandID: "qwen", BrandLabel: "阿里云百炼 / Qwen", Region: "global", Product: "coding"},
	"stepfun":                           {BrandID: "stepfun", BrandLabel: "StepFun", Region: "global", Product: "coding"},
	"stepfun-responses":                 {BrandID: "stepfun", BrandLabel: "StepFun", Region: "global", Product: "api"},
	"stepfun-anthropic":                 {BrandID: "stepfun", BrandLabel: "StepFun", Region: "global", Product: "coding"},
	"stepfun-api":                       {BrandID: "stepfun", BrandLabel: "StepFun", Region: "global", Product: "api"},
	"stepfun-api-anthropic":             {BrandID: "stepfun", BrandLabel: "StepFun", Region: "global", Product: "api"},
	"opencode-go-recommended":           {BrandID: "opencode", BrandLabel: "OpenCode", Region: "global", Product: "go"},
	"opencode-go-anthropic":             {BrandID: "opencode", BrandLabel: "OpenCode", Region: "global", Product: "go"},
	"opencode-go-responses":             {BrandID: "opencode", BrandLabel: "OpenCode", Region: "global", Product: "go"},
	"opencode-go-deepseek-anthropic":    {BrandID: "opencode", BrandLabel: "OpenCode", Region: "global", Product: "go"},
	"opencode-go-deepseek-responses":    {BrandID: "opencode", BrandLabel: "OpenCode", Region: "global", Product: "go"},
	"scnet":                             {BrandID: "scnet", BrandLabel: "SCNet", Region: "global", Product: "token"},
	"scnet-anthropic":                   {BrandID: "scnet", BrandLabel: "SCNet", Region: "global", Product: "token"},
}

func CatalogForProviderPreset(p ProviderPreset) ProviderCatalog {
	c, ok := providerCatalog[p.ID]
	if !ok {
		c = ProviderCatalog{BrandID: p.ID, BrandLabel: p.Label, Region: "global", Product: "api"}
	}
	if len(p.Entries) == 1 {
		c.Format = p.Entries[0].Kind
		c.BaseURL = p.Entries[0].BaseURL
	} else {
		c.Format = "bundle"
	}
	c.Protocols = ProtocolEndpointsForCatalog(c)
	return c
}
