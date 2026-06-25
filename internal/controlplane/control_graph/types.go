package controlgraph

const (
	MinExplorationRatePercent     = 3
	DefaultExplorationRatePercent = 10
	MaxExplorationRatePercent     = 12
)

type NodeType string

const (
	NodeStability     NodeType = "stability"
	NodeExploration   NodeType = "exploration"
	NodeMutation      NodeType = "mutation"
	NodeSemanticDrift NodeType = "semantic_drift"
)

type Action string

const (
	ActionSafeMode  Action = "safe_mode"
	ActionStabilize Action = "stabilize"
	ActionDampen    Action = "dampen"
	ActionBalanced  Action = "balanced"
	ActionExplore   Action = "explore"
)

type Influence string

const (
	InfluenceAmplify  Influence = "amplify"
	InfluenceSuppress Influence = "suppress"
	InfluenceBalance  Influence = "balance"
)

type SystemState struct {
	Stable                    bool
	Unstable                  bool
	Oscillating               bool
	HasDrift                  bool
	SemanticShift             []string
	RecentSuccesses           int
	RecentFailures            int
	RecentSoftDrifts          int
	RecentHardDrifts          int
	MemoryFailureAttributions int
	MemoryNoisePatterns       int
	CompilerImprovements      int
	MutationPressure          int
}

type ControlSignal struct {
	NodeID                 string
	Type                   NodeType
	Action                 Action
	Strength               float64
	Confidence             float64
	Weight                 float64
	Reliability            float64
	ExplorationRatePercent int
	Gain                   float64
	Reason                 string
}

type ControlNode interface {
	ID() string
	Type() NodeType
	Weight() float64
	Reliability() float64
	Signal(SystemState) ControlSignal
}

type ControlEdge struct {
	From      string
	To        string
	Influence Influence
}

type NodeInfluence struct {
	NodeID      string
	Type        NodeType
	Action      Action
	Share       float64
	Weight      float64
	Reliability float64
}

type ControlGraph struct {
	Nodes []ControlNode
	Edges []ControlEdge
}

type ControlDecision struct {
	Action                 Action
	Confidence             float64
	ConsensusScore         float64
	Variance               float64
	ExplorationRatePercent int
	Gain                   float64
	Controller             string
	SemanticShift          []string
	NodeInfluence          []NodeInfluence
	Signals                []string
	Reasons                []string
	EquilibriumState       string
	EquilibriumActions     []string
	ControlGraphEntropy    float64
	SystemStabilityScore   float64
	ConvergenceVelocity    float64
	OscillationIndex       float64
	SafeMode               bool
}

func Clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func ClampRate(rate int) int {
	if rate < MinExplorationRatePercent {
		return MinExplorationRatePercent
	}
	if rate > MaxExplorationRatePercent {
		return MaxExplorationRatePercent
	}
	return rate
}

func WithoutNode(graph ControlGraph, id string) ControlGraph {
	out := ControlGraph{Edges: make([]ControlEdge, 0, len(graph.Edges))}
	for _, node := range graph.Nodes {
		if node.ID() != id {
			out.Nodes = append(out.Nodes, node)
		}
	}
	for _, edge := range graph.Edges {
		if edge.From != id && edge.To != id {
			out.Edges = append(out.Edges, edge)
		}
	}
	return out
}

func CloneGraph(graph ControlGraph) ControlGraph {
	out := ControlGraph{
		Nodes: append([]ControlNode(nil), graph.Nodes...),
		Edges: append([]ControlEdge(nil), graph.Edges...),
	}
	return out
}
