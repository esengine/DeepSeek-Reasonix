package builtin

import "encoding/json"

// hostSubjectArgs is what the permission gate reads for a call whose subject
// the host knows: the arguments schema declares, with key set to subject or
// removed when subject is empty. An undeclared field is dropped because the
// gate would otherwise take a subject the model invented from it.
func hostSubjectArgs(schema, args json.RawMessage, key, subject string) json.RawMessage {
	var declared struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	_ = json.Unmarshal(schema, &declared)
	var sent map[string]json.RawMessage
	_ = json.Unmarshal(args, &sent)
	fields := map[string]json.RawMessage{}
	for name, value := range sent {
		if _, ok := declared.Properties[name]; ok && name != key {
			fields[name] = value
		}
	}
	if subject != "" {
		fields[key], _ = json.Marshal(subject)
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}
