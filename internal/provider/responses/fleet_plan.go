package responses

import (
	"encoding/json"
	"fmt"
	"strings"
)

// retrievalPromptBase 是并行检索子代理的公共指令（fable5 优化）：
// 检索策略（缓存优先）、质量要求（权威来源/排除营销煽动）、输出约束
// （facts/sources 数量、confidence 语义）。两处任务生成器共用。
func retrievalPromptBase() string {
	return "你是并行检索子代理，负责高质量信息检索。\n" +
		"检索策略：1) 先调用 retrieve_info 查询本地知识缓存（零成本，命中直接采用）；" +
		"2) 未命中再用 web_search 检索；3) 优先权威来源（政府/官方机构/主流媒体/学术），" +
		"排除营销话术（最有效/零风险/限时抢购/专家推荐）与煽动性内容（恐慌/末日/必须转发）。\n" +
		"输出约束：facts 3-8 条核心事实（涉及时效的信息标注时间）；sources 最多 5 个；" +
		"confidence 0-1（来源越权威、交叉验证越多，可信度越高）。\n"
}

// fleet_plan.go：把 info-frame 接入 fleet 并行子代理（2026-08-03 方案 B）。
// 模型调用 fleet 工具前，先用 BuildFleetRetrievalTasks 展开检索计划为
// 并行子代理任务（每任务 = 场景×语言×四维 一帧），子代理各自用
// web_search 检索并返回 InfoFrame JSON，最后 MergeFrames 拼图。

// FleetTaskSpec is one parallel sub-agent retrieval task.
type FleetTaskSpec struct {
	Prompt      string   `json:"prompt"`
	Description string   `json:"description"`
	ReadOnly    bool     `json:"read_only"`
	Tools       []string `json:"tools"`
	MaxSteps    int      `json:"max_steps"`
}

// BuildFleetRetrievalTasks expands a research plan into parallel fleet tasks.
// For each (domain × language) it emits one sub-agent task whose prompt asks
// for a structured InfoFrame JSON back. 默认覆盖：全部语言 × 指定场景。
// depth 控制四维查询模板（fact 必含，其余按深度）。
func BuildFleetRetrievalTasks(topic string, depth ResearchDepth, langs []string, domains []InfoDomain) []FleetTaskSpec {
	if len(langs) == 0 {
		langs = []string{"zh", "en"}
	}
	if len(domains) == 0 {
		domains = []InfoDomain{DomainGeneral}
	}
	plan := PlanResearch(topic, depth)

	var tasks []FleetTaskSpec
	for _, d := range domains {
		audiences := AudiencesFor(d)
		audDesc := ""
		if len(audiences) > 0 {
			audDesc = "（该场景典型人群: " + strings.Join(audienceNames(audiences), "、") + "）"
		}
		for _, lang := range langs {
			// 该帧的检索查询：四维模板 + 场景提示 + 语言后缀
			queries := make([]string, 0, len(plan.Queries))
			for _, q := range plan.Queries {
				queries = append(queries, SceneQuery(q.Query, d, lang))
			}
			prompt := retrievalPromptBase() + fmt.Sprintf(
				"本次任务：主题「%s」的【%s】场景%s【%s】语言帧。\n"+
					"请检索以下查询（可合并为 1-3 次搜索）：\n%s\n\n"+
					"只输出一个 JSON 对象（InfoFrame 格式），不要多余文字：\n"+
					`{"domain":"%s","language":"%s","topic":"%s","facts":["核心事实1","核心事实2"],"sources":[{"title":"来源名","url":"https://..."}],"confidence":0.0}`,
				topic, domainName(d), audDesc, lang,
				"- "+strings.Join(queries, "\n- "),
				d, lang, topic)
			tasks = append(tasks, FleetTaskSpec{
				Prompt:      prompt,
				Description: fmt.Sprintf("检索 %s/%s", domainName(d), lang),
				ReadOnly:    true,
				Tools:       []string{"web_search", "retrieve_info", "web_fetch"},
				MaxSteps:    5,
			})
		}
	}
	return tasks
}

func domainName(d InfoDomain) string {
	switch d {
	case DomainEconomic:
		return "经济"
	case DomainIndustrial:
		return "工业"
	case DomainCode:
		return "代码"
	case DomainStudent:
		return "学生"
	case DomainResearch:
		return "科研"
	default:
		return "通用"
	}
}

func audienceNames(as []SceneAudience) []string {
	names := map[SceneAudience]string{
		AudInvestor: "投资者", AudAnalyst: "分析师", AudEntrepreneur: "创业者", AudWorker: "从业者",
		AudEngineer: "工程师", AudSupplyChain: "供应链", AudFactory: "工厂",
		AudDeveloper: "开发者", AudArchitect: "架构师", AudDevOps: "运维",
		AudUndergrad: "本科生", AudPostgrad: "研究生", AudJobSeeker: "求职者", AudAbroad: "留学",
		AudScholar: "学者", AudResearcher: "研究员", AudReviewer: "审稿人",
	}
	out := make([]string, 0, len(as))
	for _, a := range as {
		if n, ok := names[a]; ok {
			out = append(out, n)
		}
	}
	return out
}

// ParseInfoFrame decodes a sub-agent's JSON reply into an InfoFrame.
func ParseInfoFrame(raw []byte) (*InfoFrame, error) {
	var f InfoFrame
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse info frame: %w", err)
	}
	if f.Topic == "" {
		return nil, fmt.Errorf("parse info frame: empty topic")
	}
	return &f, nil
}

// AssembleFrameView runs the parallel-retrieval contract: parse every
// sub-agent reply and merge into the 信息拼图.
func AssembleFrameView(topic string, rawReplies [][]byte) FrameView {
	frames := make([]*InfoFrame, 0, len(rawReplies))
	for _, raw := range rawReplies {
		if f, err := ParseInfoFrame(raw); err == nil {
			frames = append(frames, f)
		}
	}
	return MergeFrames(topic, frames)
}

// FrameDomainFor maps a sovereign module path ("Sovereign.Coupling.LCM" or
// "Sovereign.Physics.QuantumCorrespondence") to its mathematical frame
// domain. The directory segment (index 2) is the authoritative classifier —
// 26 top-level dirs in src/Sovereign/ — so file-level knowledge entries
// assemble into domain frames without an "Other" bucket. Unknown paths
// return "" (caller falls back to a generic frame).
func FrameDomainFor(modulePath string) string {
	segs := strings.Split(modulePath, ".")
	if len(segs) < 2 {
		return ""
	}
	// 路径结构：Sovereign.<目录>.<子模块>（3 段）或 Sovereign.<顶层模块>
	// （2 段）。目录是 segs[1]；子模块（segs[2]）不参与分帧。
	switch segs[1] {
	case "RootMath", "Algebra", "Binary", "Decimal", "Tryte", "GF9AlgebraicChain", "GF729":
		return "代数学"
	case "Coupling", "LCMVortexConnection", "L2Bridge", "LCM", "TQ10", "CartanTorsion", "Zhonglv", "Winding", "Aether":
		return "耦合与纤维丛"
	case "HoTT", "Topology", "LieDiscrete":
		return "同伦与拓扑"
	case "Structology", "Holographic", "MagicSquare", "A4Representations", "Platonics", "BinaryTetrahedral", "Closure", "Lattice", "TorusClosure", "KanComposition", "MagicSquareM4", "WuXingTransition":
		return "全息与幻方"
	case "Base", "Foundation", "Constitution", "SevenStages":
		return "公理与宪法"
	case "Arithmetic", "PigeonholeStandard", "SpectralTheorem", "DiscreteAnalysis", "FunctionalAnalysisDiscrete", "NormDiscrete", "LinearFunctionalDiscrete", "DiscreteLimit", "DiscreteJacobi", "DiscreteDE", "ConvergenceAlignment", "Analysis", "Integration", "Diagnosis":
		return "分析学"
	case "Geometry", "Projection", "ProjectiveTransform", "ProjectionDifferential", "ProjectionDiffGeo", "ComplexProjection", "ConformalCore", "ProjectiveCore", "ProjectiveOrbit", "ProjectionAnalysis":
		return "几何与投影"
	case "Physics", "PDE", "PDEDiscrete", "QuartzPhonon", "Resonance", "EntropySpin", "ElectricCivilization", "FineStructureMapping", "WindingAsymmetry", "Boundaries", "XuanwuAbsorption", "Nayin", "WuXing", "H2OC60", "QsUpdate", "External":
		return "物理映射"
	case "Quantum", "QuantumCorrespondence":
		return "量子对应"
	case "AI", "Coding", "Engine", "StateMachine", "DataAnchors", "Format", "Applied":
		return "计算与工程"
	case "Completeness", "CompletenessTheorem", "Density", "DegenerationTaxonomy", "DegenerationRisk", "SectionRisk", "MyopiaRisk":
		return "完备性与风险"
	case "MetaStructure", "TowerConnection", "Scaling", "Layer":
		return "元结构"
	case "Problem", "PvsNP", "Trust":
		return "问题与映射"
	case "Hodge", "Riemann", "Kakeya", "AlgebraicPoleUnified", "DiscreteRepresentation", "Jacobian", "FiniteDynamics", "EnergyGap", "HolographicPi", "HolographicSpace":
		return "分析映射"
	default:
		return ""
	}
}

// FrameSubdomainFor refines a module path into a domain×subtopic frame:
// the directory (segs[1]) picks the 13 domain frames; the submodule
// (segs[2]) narrows to a subtopic ("代数学·数字根", "耦合与纤维丛·旋量").
// Returns "" when the subtopic is unknown — caller keeps the domain-level
// frame. Unknown domain (FrameDomainFor=="") also returns "".
func FrameSubdomainFor(modulePath string) string {
	segs := strings.Split(modulePath, ".")
	if len(segs) < 3 {
		return ""
	}
	dom := FrameDomainFor(modulePath)
	if dom == "" {
		return ""
	}
	sub := segs[2]
	topics := map[string]string{
		// 代数学
		"DigitalRoot": "数字根", "Eisenstein": "艾森斯坦整数", "AlgebraicComplex": "高斯整数",
		"LengthLattice": "长度格", "EnergyGap": "能隙", "Tryte": "三进制编码", "GF9AlgebraicChain": "GF9链",
		// 耦合与纤维丛
		"LCM": "三进制归约", "Zhonglv": "仲吕", "CartanTorsion": "卡坦挠率", "SpinTwistor": "旋量",
		"ParityViolation": "宇称破缺", "LossGain": "损益", "ZhonglvPhaseSync": "仲吕相移",
		"Entanglement": "纠缠", "TQ10": "校验", "Winding": "缠绕",
		// 同伦与拓扑
		"T6Homotopy": "环面同伦", "ChernClass": "陈类", "Fibration": "纤维", "KanComposition": "Kan填充",
		"DiscreteCCHM": "离散CCHM", "PhaseTransitionPaths": "相变路径", "ChernConservation": "陈守恒",
		// 全息与幻方
		"MagicSquareM4": "幻方M4", "HolographicSpace": "全息空间", "A4Representations": "A4群表示",
		"BinaryTetrahedral": "二元四面体群", "Platonics": "柏拉图体", "TorusClosure": "环面闭包",
		"Lattice": "格", "Aether": "以太", "WuXingTransition": "五行跃迁",
		// 公理与宪法
		"Boundaries": "边界", "WindingAsymmetry": "缠绕不对称",
		// 分析学
		"CRTLemmas": "CRT引理", "SpectralTheorem": "谱定理", "FunctionalAnalysisDiscrete": "离散泛函",
		"DiscreteJacobi": "离散雅可比", "DiscreteLimit": "离散极限", "ConvergenceAlignment": "收敛对齐",
		// 几何与投影
		"ProjectiveTransform": "射影变换", "ProjectiveCore": "射影核", "ConformalCore": "共形核",
		"ComplexProjection": "复投影", "ProjectionDifferential": "投影微分",
		// 物理映射
		"QuartzPhonon": "石英声子", "FineStructureMapping": "精细结构", "H2OC60": "水C60",
		"EntropySpin": "熵自旋", "Resonance": "谐振", "Nayin": "纳音", "WuXing": "五行",
		// 量子对应
		"QuantumCorrespondence": "量子对应", "Foundation": "量子基础",
		// 问题与映射
		"PvsNP": "PvsNP", "Hodge": "霍奇", "Riemann": "黎曼", "Kakeya": "挂谷",
		// 完备性与风险
		"CompletenessTheorem": "完备性定理", "DegenerationTaxonomy": "退化分类",
	}
	if sub != "" {
		if t, ok := topics[sub]; ok {
			return dom + "·" + t
		}
	}
	// 未知子模块：回退域级帧（不丢失分类）。
	return dom
}
