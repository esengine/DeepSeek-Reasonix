package attachment

import (
	"encoding/json"

	"reasonix/internal/sessioncontent"
)

// CollectJSONRefs extracts attachment objects from any JSON value that may
// contain ImageInput or AttachmentRef objects. It is used by export, import,
// and authorization rebuilds.
func CollectJSONRefs(raw []byte) []sessioncontent.Ref {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	seen := map[string]sessioncontent.Ref{}
	walkValue(value, seen)
	if len(seen) == 0 {
		return nil
	}
	out := make([]sessioncontent.Ref, 0, len(seen))
	for _, ref := range seen {
		out = append(out, ref)
	}
	return out
}

func walkValue(value any, seen map[string]sessioncontent.Ref) {
	switch item := value.(type) {
	case map[string]any:
		if ref, ok := attachmentRefFromMap(item); ok {
			key := ref.Digest + ":" + itoa(ref.Bytes) + ":" + ref.IndexDigest
			seen[key] = ref
		}
		for _, nested := range item {
			walkValue(nested, seen)
		}
	case []any:
		for _, nested := range item {
			walkValue(nested, seen)
		}
	}
}

func attachmentRefFromMap(item map[string]any) (sessioncontent.Ref, bool) {
	content, _ := item["content"].(map[string]any)
	if content == nil {
		if _, ok := item["digest"].(string); ok && item["v"] == nil {
			content = item
		}
	}
	if content == nil {
		return sessioncontent.Ref{}, false
	}
	digest, _ := content["digest"].(string)
	if digest == "" {
		return sessioncontent.Ref{}, false
	}
	ref := sessioncontent.Ref{
		Digest:      digest,
		MediaType:   stringField(content, "mediaType"),
		Name:        stringField(content, "name"),
		IndexDigest: stringField(content, "indexDigest"),
	}
	switch bytes := content["bytes"].(type) {
	case float64:
		ref.Bytes = int64(bytes)
	case json.Number:
		n, _ := bytes.Int64()
		ref.Bytes = n
	}
	switch block := content["integrityBlockBytes"].(type) {
	case float64:
		ref.IntegrityBlock = int64(block)
	case json.Number:
		n, _ := block.Int64()
		ref.IntegrityBlock = n
	}
	if ref.Bytes <= 0 {
		return sessioncontent.Ref{}, false
	}
	return ref, true
}

func stringField(item map[string]any, key string) string {
	value, _ := item[key].(string)
	return value
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
