package config

import "strings"

func renderHideModelPrefix(b *strings.Builder, p ProviderEntry) {
	if p.HideModelPrefix {
		b.WriteString("hide_model_prefix = true   # display only: leave out the prefix every model of this entry shares\n")
	}
}

// ModelShort is how model is drawn with the entry's shared prefix left out, or
// "" when it is drawn as it is: the switch is off, no prefix is shared by every
// listed model, or leaving it out would make the model read the same as another
// (its own alias included). The id and ref never change.
func (e *ProviderEntry) ModelShort(model string) string {
	if !e.HideModelPrefix {
		return ""
	}
	prefix := sharedModelPrefix(e.ModelList())
	if prefix == "" || !strings.HasPrefix(model, prefix) || model == prefix {
		return ""
	}
	short := strings.TrimPrefix(model, prefix)
	for _, other := range e.ModelList() {
		if other == model {
			continue
		}
		if e.ModelAlias(other) == short || (e.ModelAlias(other) == "" && strings.TrimPrefix(other, prefix) == short) {
			return ""
		}
	}
	return short
}

// sharedModelPrefix is the longest leading path every model has in common, up to
// and including its last "/": what a router puts in front of every model it
// offers. It is read off the entry's own list, so no group name is ever written
// into the code.
func sharedModelPrefix(models []string) string {
	if len(models) == 0 {
		return ""
	}
	common := models[0]
	for _, m := range models[1:] {
		n := 0
		for n < len(common) && n < len(m) && common[n] == m[n] {
			n++
		}
		common = common[:n]
	}
	at := strings.LastIndex(common, "/")
	if at < 0 {
		return ""
	}
	return common[:at+1]
}
