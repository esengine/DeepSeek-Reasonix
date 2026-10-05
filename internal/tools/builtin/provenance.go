package builtin

import (
	"encoding/json"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/browser"
)

func (webFetch) Provenance(args json.RawMessage) tool.Provenance {
	var p struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(args, &p)
	return tool.Provenance{Kind: tool.ProvenanceWeb, Source: tool.HostOf(p.URL)}
}

func (b browserOpen) Provenance(args json.RawMessage) tool.Provenance {
	var p struct {
		URL string `json:"url"`
		Tab string `json:"tab"`
	}
	_ = json.Unmarshal(args, &p)
	if host := tool.HostOf(p.URL); host != "" {
		return tool.Provenance{Kind: tool.ProvenanceBrowser, Source: host}
	}
	return browserProvenance(b.session, p.Tab)
}

func (b browserRead) Provenance(args json.RawMessage) tool.Provenance {
	return browserProvenance(b.session, browserTabArg(args))
}

func (b browserAct) Provenance(args json.RawMessage) tool.Provenance {
	return browserProvenance(b.session, browserTabArg(args))
}

func (computerRead) Provenance(json.RawMessage) tool.Provenance {
	return tool.Provenance{Kind: tool.ProvenanceDesktop}
}

func (computerAct) Provenance(json.RawMessage) tool.Provenance {
	return tool.Provenance{Kind: tool.ProvenanceDesktop}
}

func browserTabArg(args json.RawMessage) string {
	var p struct {
		Tab string `json:"tab"`
	}
	_ = json.Unmarshal(args, &p)
	return p.Tab
}

// browserProvenance names the page a call read or acted on, as the session
// holds it after the call: the named tab, else the active one.
func browserProvenance(s *browser.Session, tabID string) tool.Provenance {
	out := tool.Provenance{Kind: tool.ProvenanceBrowser}
	if s == nil {
		return out
	}
	for _, tab := range s.Tabs() {
		if (tabID != "" && tab.ID == tabID) || (tabID == "" && tab.Active) {
			out.Source = tool.HostOf(tab.URL)
			break
		}
	}
	return out
}
