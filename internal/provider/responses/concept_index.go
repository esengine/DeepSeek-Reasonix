package responses

import "strings"

// conceptIndex maps a short concept keyword (2-6 chars) to the cached
// module query whose entry holds the authoritative definition. It fixes the
// short-query recall gap found in math cross-verification (2026-08-03):
// "霍奇" (2 chars) against a long Query ("Sovereign.Problem.Hodge 霍奇...")
// scores below the L2 threshold and falls through to a live web_search
// (15-34s). Concept resolution runs before L2 and lands an L1 hit.
var conceptIndex = map[string]string{
	// 代数学
	"数字根":  "Sovereign.RootMath.DigitalRoot",
	"艾森斯坦": "Sovereign.RootMath.Eisenstein",
	"高斯整数": "Sovereign.RootMath.AlgebraicComplex",
	"长度格":  "Sovereign.RootMath.LengthLattice",
	"三进制":  "Sovereign.Coupling.LCM",
	// 耦合与纤维丛
	"卡坦挠率": "Sovereign.Coupling.CartanTorsion",
	"旋量":   "Sovereign.Coupling.SpinTwistor",
	"宇称":   "Sovereign.Coupling.ParityViolation",
	"损益":   "Sovereign.Coupling.LossGain",
	"纠缠":   "Sovereign.Coupling.Entanglement",
	"仲吕":   "Sovereign.Coupling.Zhonglv",
	// 同伦与拓扑
	"环面同伦": "Sovereign.HoTT.T6Homotopy",
	"陈类":   "Sovereign.HoTT.ChernClass",
	"陈数":   "Sovereign.HoTT.ChernConservation",
	"纤维":   "Sovereign.HoTT.Fibration",
	"同伦":   "Sovereign.HoTT.T6Homotopy",
	// 全息与幻方
	"幻方":   "Sovereign.Structology.MagicSquareM4",
	"全息":   "Sovereign.Structology.HolographicSpace",
	"柏拉图":  "Sovereign.Structology.Platonics",
	"四面体群": "Sovereign.Structology.BinaryTetrahedral",
	"以太":   "Sovereign.Structology.Aether",
	// 分析学
	"谱定理": "Sovereign.Analysis.SpectralTheorem",
	// 问题与映射
	"霍奇":    "Sovereign.Problem.Hodge",
	"黎曼":    "Sovereign.Problem.Riemann",
	"挂谷":    "Sovereign.Problem.Kakeya",
	"PvsNP": "Sovereign.Problem.PvsNP",
	// 物理映射
	"纳音": "Sovereign.MetaStructure.Nayin",
	"五行": "Sovereign.MetaStructure.WuXing",
	"声子": "Sovereign.Physics.QuartzPhonon",
	// 公理与宪法
	"边界": "Sovereign.Constitution.Boundaries",
}

// ResolveConcept maps a short query (≤6 chars, no spaces) to the module
// query whose cache entry holds its definition. Returns the module query
// and true when the query is a known concept keyword; false otherwise
// (caller proceeds with normal L1/L2).
func ResolveConcept(query string) (string, bool) {
	q := strings.TrimSpace(query)
	if q == "" || len([]rune(q)) > 6 || strings.ContainsAny(q, " .,;:!?") {
		return "", false
	}
	if mod, ok := conceptIndex[q]; ok {
		return mod, true
	}
	// 后缀匹配（"霍奇猜想" → 霍奇）
	for k, mod := range conceptIndex {
		if strings.HasPrefix(q, k) || strings.HasSuffix(q, k) {
			return mod, true
		}
	}
	return "", false
}

// HasFreshnessIntent reports whether a query asks for current/latest
// information (news, markets, "最新/2026/recent"). Freshness-seeking
// queries must not be served static domain knowledge (Tier=domain, locally
// injected pre-verified content) — the user wants the newest paper, not the
// library snapshot.
func HasFreshnessIntent(q string) bool {
	lower := strings.ToLower(q)
	for _, w := range []string{"最近", "最新", "本月", "今年", "current", "latest", "recent", "new", "2025", "2026"} {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// DefaultSemanticThresholdEN is the L2 semantic cutoff for English queries.
// English shares many stopwords ("the/of/and/2026") that inflate character-
// set overlap, so the 0.35 Chinese threshold false-positives across math
// topics ("ABC conjecture" → "CRT beat frequency", 2026-08-03). English
// requires a stricter match; Chinese keeps 0.35 (semantic-dense chars).
const DefaultSemanticThresholdEN = 0.55

// SemanticThresholdFor picks the L2 threshold by query language.
func SemanticThresholdFor(query string) float64 {
	if DetectLanguage(query) == "en" {
		return DefaultSemanticThresholdEN
	}
	return DefaultSemanticThreshold
}
