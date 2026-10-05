package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/model/laya"
	"reasonix/internal/safety/typesafe"
)

// SystemOneSpec is one decision endpoint plus the local runtime. The endpoint
// is an ordinary provider entry the decision role names, so Name is the name
// the person gave that source — which is what the model is offered, rather
// than a vendor this file would have to know.
type SystemOneSpec struct {
	HTTP      *http.Client
	Name      string
	BaseURL   string
	Model     string
	APIKey    func() string
	LayaLocal *laya.LocalClient
}

type systemOne struct{ spec SystemOneSpec }

func NewSystemOne(spec SystemOneSpec) tool.Tool { return systemOne{spec: spec} }

func SystemOneConfigured(spec SystemOneSpec) bool {
	return spec.LayaLocal != nil || spec.APIKey != nil && strings.TrimSpace(spec.APIKey()) != ""
}

// endpointName is what the model may ask for, and what settings calls it.
func (s systemOne) endpointName() string {
	if name := strings.TrimSpace(s.spec.Name); name != "" {
		return name
	}
	return "endpoint"
}

func (s systemOne) backends() []string {
	out := make([]string, 0, 2)
	if s.spec.APIKey != nil && strings.TrimSpace(s.spec.APIKey()) != "" {
		out = append(out, s.endpointName())
	}
	if s.spec.LayaLocal != nil {
		out = append(out, localBackend)
	}
	return out
}

const localBackend = "laya-local"

func (systemOne) Name() string { return "system_one" }

func (s systemOne) Description() string {
	return "Evaluate state with the configured System One decision backend (" + strings.Join(s.backends(), ", ") + "). Ask typed Noul, Choice, or Score questions and receive complete probabilities, confidence, routing metadata, and usage."
}

// The enum is what is actually configured, by the names settings shows: an
// option the model can read but never reach is a refusal it has to discover.
func (s systemOne) Schema() json.RawMessage {
	enum, err := json.Marshal(s.backends())
	if err != nil {
		enum = []byte(`[]`)
	}
	return json.RawMessage(`{"type":"object","properties":{"backend":{"type":"string","enum":` + string(enum) + `,"description":"Configured decision backend; optional only when exactly one is available"},"state":{"description":"Text or structured JSON state to evaluate"},"questions":{"type":"object","description":"Named questions; answer keys match these ids","additionalProperties":{"type":"object","properties":{"type":{"type":"string","enum":["noul","choice","score"]},"instructions":{"description":"Question text or structured instructions"},"criteria":{"description":"Noul: optional true/false object; Choice: 2-255 option map; Score: ordered array of 2-10 levels"}},"required":["type","instructions"],"additionalProperties":false}}},"required":["state","questions"],"additionalProperties":false}`)
}

func (systemOne) ReadOnly() bool     { return true }
func (systemOne) PlanModeSafe() bool { return true }
func (systemOne) SnipHint() tool.SnipHint {
	return tool.SnipHint{Head: 120, Tail: 8, HeadChars: 12000, TailChars: 1000}
}

func (s systemOne) ProviderVisible(context.Context) bool {
	return SystemOneConfigured(s.spec)
}

func (systemOne) Unavailable(context.Context) tool.Refusal {
	return tool.Refusal{Code: "decision.not_configured", Message: "no model is assigned to the decision role"}
}

func (s systemOne) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var input struct {
		Backend   string                       `json:"backend"`
		State     any                          `json:"state"`
		Questions map[string]typesafe.Question `json:"questions"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", err
	}
	if !s.ProviderVisible(ctx) {
		return "", errors.New("system_one needs a model assigned to the decision role")
	}
	backend, err := s.backend(input.Backend)
	if err != nil {
		return "", err
	}
	request := typesafe.Request{State: input.State, Questions: input.Questions}
	var response typesafe.Response
	switch backend {
	case localBackend:
		request.Model = s.spec.LayaLocal.Model
		response, err = s.spec.LayaLocal.Evaluate(ctx, request)
	default:
		request.Model = strings.TrimSpace(s.spec.Model)
		if request.Model == "" {
			request.Model = "jev-latest"
		}
		response, err = (typesafe.Client{HTTP: s.spec.HTTP, BaseURL: s.spec.BaseURL, APIKey: s.spec.APIKey}).Evaluate(ctx, request)
	}
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(response)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (s systemOne) backend(requested string) (string, error) {
	available := s.backends()
	requested = strings.TrimSpace(requested)
	if requested == "" && len(available) == 1 {
		return available[0], nil
	}
	for _, backend := range available {
		if backend == requested {
			return backend, nil
		}
	}
	if requested == "" {
		return "", fmt.Errorf("system_one backend is required; available: %s", strings.Join(available, ", "))
	}
	return "", fmt.Errorf("system_one backend %q is not configured; available: %s", requested, strings.Join(available, ", "))
}
