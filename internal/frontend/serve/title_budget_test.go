package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"reasonix/internal/contract/provider"
)

// A model that ignores the request to think less spends the whole output
// budget on reasoning and says nothing. The host knows that is what happened,
// so it gives the model room once instead of failing the naming.
type thinkingTitleProvider struct {
	mu      sync.Mutex
	budgets []int
	needs   int
}

func (*thinkingTitleProvider) Name() string { return "thinking-title" }
func (p *thinkingTitleProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.budgets = append(p.budgets, req.MaxTokens)
	p.mu.Unlock()
	if req.MaxTokens < p.needs {
		return chunks(provider.Chunk{Type: provider.ChunkReasoning, Text: "let me think about a title"}, provider.Chunk{Type: provider.ChunkDone})
	}
	return chunks(provider.Chunk{Type: provider.ChunkReasoning, Text: "short thought"}, provider.Chunk{Type: provider.ChunkText, Text: "Fix the login timeout"}, provider.Chunk{Type: provider.ChunkDone})
}

func TestAutoNameRetriesOnceWithRoomWhenTheModelOnlyThought(t *testing.T) {
	h, rt, path, _ := titleSession(t)
	p := &thinkingTitleProvider{needs: 200}
	rt.Server.titleModel.prov = p
	res := titlePost(t, h, "auto-name", path, "")
	var got struct {
		Title string `json:"title"`
	}
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &got) != nil || got.Title != "Fix the login timeout" {
		t.Fatalf("auto-name = %d %s", res.Code, res.Body.String())
	}
	if len(p.budgets) != 2 || p.budgets[0] >= p.budgets[1] || p.budgets[1] < 200 {
		t.Fatalf("budgets asked for = %v, want a short one then a larger one", p.budgets)
	}
}

func TestAutoNameDoesNotRetryAModelThatAnswersInTime(t *testing.T) {
	h, rt, path, _ := titleSession(t)
	p := &thinkingTitleProvider{needs: 0}
	rt.Server.titleModel.prov = p
	if res := titlePost(t, h, "auto-name", path, ""); res.Code != http.StatusOK {
		t.Fatalf("auto-name = %d %s", res.Code, res.Body.String())
	}
	if len(p.budgets) != 1 {
		t.Fatalf("a model that answered was asked %d times: %v", len(p.budgets), p.budgets)
	}
}

func TestAutoNameStillNamesItsCauseWhenMoreRoomDoesNotHelp(t *testing.T) {
	h, rt, path, _ := titleSession(t)
	p := &thinkingTitleProvider{needs: 1 << 30}
	rt.Server.titleModel.prov = p
	res := titlePost(t, h, "auto-name", path, "")
	var reason Reason
	if json.Unmarshal(res.Body.Bytes(), &reason) != nil || reason.Code != "session.title_thought_only" {
		t.Fatalf("auto-name = %d %s", res.Code, res.Body.String())
	}
	if len(p.budgets) != 2 {
		t.Fatalf("a model that never answers was asked %d times, want exactly 2: %v", len(p.budgets), p.budgets)
	}
}
