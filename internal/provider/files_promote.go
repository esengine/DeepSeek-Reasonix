package provider

import "encoding/base64"

// PromoteDataURLsToFiles replaces every retained input data URL with a Files
// id. If any occurrence cannot be uploaded, the original messages are returned
// so a request never mixes file ids and inline images.
func PromoteDataURLsToFiles(messages []Message, resolve func(filename string, raw []byte) (string, error)) []Message {
	if len(messages) == 0 || resolve == nil {
		return messages
	}
	type hit struct {
		msg, image int
		id         string
	}
	var hits []hit
	for i, msg := range messages {
		if msg.Role == RoleAssistant {
			continue
		}
		for j, ref := range msg.Images {
			if ClassifyImage(ref) != ImageDataURL {
				continue
			}
			_, payload, ok := ParseImageDataURL(ref)
			if !ok {
				return messages
			}
			raw, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				return messages
			}
			id, err := resolve("image", raw)
			if err != nil || ClassifyImage(id) != ImageFileID {
				return messages
			}
			hits = append(hits, hit{msg: i, image: j, id: id})
		}
	}
	if len(hits) == 0 {
		return messages
	}
	out := append([]Message(nil), messages...)
	copied := map[int]bool{}
	for _, h := range hits {
		if !copied[h.msg] {
			out[h.msg].Images = append([]string(nil), out[h.msg].Images...)
			copied[h.msg] = true
		}
		out[h.msg].Images[h.image] = h.id
	}
	return out
}
