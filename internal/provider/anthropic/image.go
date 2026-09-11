package anthropic

import "reasonix/internal/provider"

func imageSourceFromRef(ref string) *imageSource {
	switch provider.ClassifyImage(ref) {
	case provider.ImageDataURL:
		mt, data, ok := provider.ParseImageDataURL(ref)
		if !ok {
			return nil
		}
		return &imageSource{Type: "base64", MediaType: mt, Data: data}
	case provider.ImageHTTPURL:
		return &imageSource{Type: "url", URL: ref}
	case provider.ImageFileID:
		return &imageSource{Type: "file", FileID: ref}
	default:
		return nil
	}
}

func visionRequestUsesFileID(req provider.Request) bool {
	for _, m := range req.Messages {
		if m.Role != provider.RoleUser && m.Role != provider.RoleTool {
			continue
		}
		for _, ref := range m.Images {
			if provider.ClassifyImage(ref) == provider.ImageFileID {
				return true
			}
		}
	}
	return false
}
