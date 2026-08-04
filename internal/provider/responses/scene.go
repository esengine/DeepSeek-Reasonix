package responses

// 场景分类矩阵（2026-08-03 用户要求：经济/工业/代码/学生/科研不同人群和
// 职业全覆盖 + 多语言检索，不依赖单一信息来源）。场景决定检索模板与
// 人群视角；语言决定查询翻译。

// InfoDomain is the five primary usage scenarios plus a general fallback.
type InfoDomain string

const (
	DomainEconomic   InfoDomain = "economic"   // 经济：投资/市场/宏观/企业
	DomainIndustrial InfoDomain = "industrial" // 工业：制造/供应链/能源/工程
	DomainCode       InfoDomain = "code"       // 代码：开发/架构/运维/开源
	DomainStudent    InfoDomain = "student"    // 学生：学业/考研/留学/求职
	DomainResearch   InfoDomain = "research"   // 科研：论文/方法/实验/前沿
	DomainGeneral    InfoDomain = "general"    // 通用兜底
)

// SceneAudience is a人群/职业 bucket inside a domain.
type SceneAudience string

const (
	// economic
	AudInvestor     SceneAudience = "investor"     // 投资者
	AudAnalyst      SceneAudience = "analyst"      // 分析师
	AudEntrepreneur SceneAudience = "entrepreneur" // 创业者/企业家
	AudWorker       SceneAudience = "worker"       // 打工人/从业者
	// industrial
	AudEngineer    SceneAudience = "engineer"     // 工程师
	AudSupplyChain SceneAudience = "supply_chain" // 供应链/采购
	AudFactory     SceneAudience = "factory"      // 工厂/产线
	// code
	AudDeveloper SceneAudience = "developer" // 开发者
	AudArchitect SceneAudience = "architect" // 架构师
	AudDevOps    SceneAudience = "devops"    // 运维/SRE
	// student
	AudUndergrad SceneAudience = "undergrad"  // 本科生
	AudPostgrad  SceneAudience = "postgrad"   // 研究生/考研
	AudJobSeeker SceneAudience = "job_seeker" // 求职者
	AudAbroad    SceneAudience = "abroad"     // 留学
	// research
	AudScholar    SceneAudience = "scholar"    // 学者
	AudResearcher SceneAudience = "researcher" // 研究员/实验室
	AudReviewer   SceneAudience = "reviewer"   // 审稿人/同行评审
)

// sceneAudiences maps each domain to its人群.
var sceneAudiences = map[InfoDomain][]SceneAudience{
	DomainEconomic:   {AudInvestor, AudAnalyst, AudEntrepreneur, AudWorker},
	DomainIndustrial: {AudEngineer, AudSupplyChain, AudFactory},
	DomainCode:       {AudDeveloper, AudArchitect, AudDevOps},
	DomainStudent:    {AudUndergrad, AudPostgrad, AudJobSeeker, AudAbroad},
	DomainResearch:   {AudScholar, AudResearcher, AudReviewer},
	DomainGeneral:    {},
}

// sceneQueryHints are domain-specific retrieval keywords appended to queries
// so the model searches the right register (不依赖单一信息来源).
var sceneQueryHints = map[InfoDomain]string{
	DomainEconomic:   " 市场 数据 政策 影响",
	DomainIndustrial: " 产业链 产能 供应链 技术参数",
	DomainCode:       " 文档 API 实现 最佳实践",
	DomainStudent:    " 备考 攻略 经验 要求",
	DomainResearch:   " 论文 方法 实验 数据 综述",
	DomainGeneral:    "",
}

// Languages supported for multilingual retrieval (deep-research 6-language
// practice, codecized). Queries are generated per language so results are
// not bound to a single information source.
var Languages = []string{"zh", "en", "ja", "ko", "es", "ar"}

// languageHints are translation anchors per language for template expansion.
// The model translates the topic; the hint is the register marker.
var languageHints = map[string]string{
	"zh": "",
	"en": " latest",
	"ja": " 最新",
	"ko": " 최신",
	"es": " últimas noticias",
	"ar": " آخر الأخبار",
}

// AudiencesFor returns the人群 buckets of a domain (empty for general).
func AudiencesFor(d InfoDomain) []SceneAudience {
	return sceneAudiences[d]
}

// SceneQuery builds a domain-tuned query for one language.
func SceneQuery(topic string, d InfoDomain, lang string) string {
	q := topic
	if d != DomainGeneral {
		q += sceneQueryHints[d]
	}
	if hint, ok := languageHints[lang]; ok && hint != "" {
		q += hint
	}
	return q
}

// ClassifyScene heuristically picks the domain from a query (keyword
// matching; research/code signals are the strongest).
func ClassifyScene(query string) InfoDomain {
	for _, d := range []struct {
		domain InfoDomain
		words  []string
	}{
		{DomainResearch, []string{"论文", "综述", "实验", "科研", "算法", "方法", "引用", "影响因子", "paper", "research", "experiment", "citation"}},
		{DomainCode, []string{"代码", "编程", "api", "sdk", "框架", "bug", "部署", "git", "docker", "编译", "代码", "programming", "function", "repo"}},
		{DomainEconomic, []string{"股票", "基金", "投资", "经济", "gdp", "通胀", "汇率", "股价", "财报", "market", "stock", "economy", "inflation"}},
		{DomainIndustrial, []string{"工厂", "产能", "供应链", "制造", "设备", "工艺", "能源", "物流", "factory", "supply", "manufacturing", "logistics"}},
		{DomainStudent, []string{"考研", "留学", "考试", "备考", "申请", "绩点", "论文答辩", "求职", "面试", "exam", "study", "admission", "interview"}},
	} {
		for _, w := range d.words {
			if containsFold(query, w) {
				return d.domain
			}
		}
	}
	return DomainGeneral
}

func containsFold(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		h := []rune(haystack)
		n := []rune(needle)
		for i := 0; i+len(n) <= len(h); i++ {
			match := true
			for j := range n {
				if lowerRune(h[i+j]) != lowerRune(n[j]) {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
		return false
	})()
}

func lowerRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}
