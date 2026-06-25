package controltypes

type SignalType string

const (
	SignalDecision    SignalType = "DECISION"
	SignalConstraint  SignalType = "CONSTRAINT"
	SignalWeight      SignalType = "WEIGHT"
	SignalObservation SignalType = "OBSERVATION"
)

type SourceLayer string

const (
	LayerControl     SourceLayer = "control"
	LayerEquilibrium SourceLayer = "equilibrium"
	LayerExecution   SourceLayer = "execution"
	LayerTrace       SourceLayer = "trace"
)

type TypedSignal struct {
	Type        SignalType
	SourceLayer SourceLayer
	Payload     any
	Reason      string
}

func NewSignal(signalType SignalType, layer SourceLayer, payload any, reason string) TypedSignal {
	return TypedSignal{
		Type:        signalType,
		SourceLayer: layer,
		Payload:     payload,
		Reason:      reason,
	}
}
