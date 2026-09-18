package provider

import (
	"errors"
	"fmt"
)

const (
	MaxImagesPerRequest           = 600
	MaxRequestFilesBytes          = 128 << 20
	MaxInlineRequestImageBytes    = 20 << 20
	ImageOffloadByteQuantum       = 64 << 20
	InlineImageOffloadByteQuantum = 10 << 20
	ImageOffloadCountQuantum      = 20
	imageRepresentationRaw        = "raw"
	imageRepresentationBase64     = "base64"
	ImageOffloadRequiredCode      = "IMAGE_OFFLOAD_REQUIRED"
)

// ImageOffloadBudget is one route's retained-image high watermark and the
// quanta used to name how many oldest occurrences to omit.
type ImageOffloadBudget struct {
	Representation string
	MaxBytes       int
	MaxImages      int
	ByteQuantum    int
	CountQuantum   int
}

// ImageOffloadRequiredError means the retained images still exceed the route
// budget. OffloadImages is how many oldest retained input occurrences to omit
// before retrying. This is a durable surface repair, not a provider retry.
type ImageOffloadRequiredError struct {
	OffloadImages  int
	Representation string
}

func (e *ImageOffloadRequiredError) Error() string {
	if e == nil {
		return "request images exceed the route budget"
	}
	kind := e.Representation
	if kind == "" {
		kind = "image"
	}
	return fmt.Sprintf("DeepSeek %s request images exceed the route budget; %d more oldest occurrence(s) must be offloaded", kind, e.OffloadImages)
}

func AsImageOffloadRequired(err error) *ImageOffloadRequiredError {
	var req *ImageOffloadRequiredError
	if err != nil && errors.As(err, &req) {
		return req
	}
	return nil
}

// RequestImageOffloadBudget chooses Files vs inline watermarks from the
// retained refs. File ids never mix with inline payloads on official DeepSeek.
func RequestImageOffloadBudget(messages []Message) ImageOffloadBudget {
	if requestUsesFileImages(messages) {
		return ImageOffloadBudget{
			Representation: imageRepresentationRaw,
			MaxBytes:       MaxRequestFilesBytes,
			MaxImages:      MaxImagesPerRequest,
			ByteQuantum:    ImageOffloadByteQuantum,
			CountQuantum:   ImageOffloadCountQuantum,
		}
	}
	return ImageOffloadBudget{
		Representation: imageRepresentationBase64,
		MaxBytes:       MaxInlineRequestImageBytes,
		MaxImages:      MaxImagesPerRequest,
		ByteQuantum:    InlineImageOffloadByteQuantum,
		CountQuantum:   ImageOffloadCountQuantum,
	}
}

// CheckRetainedImages returns ImageOffloadRequiredError when the retained
// input images exceed the route budget at their represented byte lengths.
func CheckRetainedImages(messages []Message) error {
	budget := RequestImageOffloadBudget(messages)
	n := RequiredImageOffload(messages, budget)
	if n <= 0 {
		return nil
	}
	return &ImageOffloadRequiredError{OffloadImages: n, Representation: budget.Representation}
}

// RequiredImageOffload is the number of oldest retained occurrences to omit
// so the request fits. Zero when it already fits.
func RequiredImageOffload(messages []Message, budget ImageOffloadBudget) int {
	return OffloadedImagePrefixCount(retainedInputImageLengths(messages, budget.Representation), budget)
}

// OffloadedImagePrefixCount removes a whole count/byte quantum once the
// budget is exceeded, matching DeepSeek Harness requiredImageOffload.
func OffloadedImagePrefixCount(lengths []int, budget ImageOffloadBudget) int {
	if len(lengths) == 0 {
		return 0
	}
	total := 0
	for _, n := range lengths {
		total += n
	}
	excessCount := 0
	if budget.MaxImages > 0 {
		excessCount = max(0, len(lengths)-budget.MaxImages)
	}
	excessBytes := 0
	if budget.MaxBytes > 0 {
		excessBytes = max(0, total-budget.MaxBytes)
	}
	if excessCount == 0 && excessBytes == 0 {
		return 0
	}
	countQuantum := budget.CountQuantum
	if countQuantum <= 0 {
		countQuantum = 1
	}
	byteQuantum := budget.ByteQuantum
	if byteQuantum <= 0 {
		byteQuantum = 1
	}
	removeCount := 0
	if excessCount > 0 {
		removeCount = ((excessCount + countQuantum - 1) / countQuantum) * countQuantum
	}
	removeBytes := 0
	if excessBytes > 0 {
		removeBytes = ((excessBytes + byteQuantum - 1) / byteQuantum) * byteQuantum
	}
	count := 0
	removedBytes := 0
	for _, imageBytes := range lengths {
		byteTargetMet := removeBytes == 0 ||
			(byteQuantum == 1 && removedBytes >= removeBytes) ||
			(byteQuantum != 1 && removedBytes > removeBytes)
		if count >= removeCount && byteTargetMet {
			break
		}
		removedBytes += imageBytes
		count++
	}
	return count
}

func requestUsesFileImages(messages []Message) bool {
	for _, msg := range messages {
		if msg.Role == RoleAssistant {
			continue
		}
		for _, ref := range msg.Images {
			if ClassifyImage(ref) == ImageFileID {
				return true
			}
		}
	}
	return false
}

func retainedInputImageLengths(messages []Message, representation string) []int {
	var lengths []int
	for _, msg := range messages {
		if msg.Role == RoleAssistant {
			continue
		}
		for _, ref := range msg.Images {
			raw, ok := retainedImageRawBytes(ref)
			if !ok {
				continue
			}
			if representation == imageRepresentationBase64 {
				lengths = append(lengths, base64Length(raw))
				continue
			}
			lengths = append(lengths, raw)
		}
	}
	return lengths
}

func retainedImageRawBytes(ref string) (int, bool) {
	switch ClassifyImage(ref) {
	case ImageNone:
		return 0, false
	case ImageDataURL:
		_, payload, ok := ParseImageDataURL(ref)
		if !ok {
			return 0, true
		}
		return base64DecodedLen(payload), true
	case ImageFileID:
		if n, ok := DefaultFileIndex().BytesForFileID(ref); ok {
			return n, true
		}
		return 0, true
	default:
		return 0, true
	}
}

func base64Length(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 2) / 3 * 4
}

func base64DecodedLen(payload string) int {
	n := len(payload)
	if n == 0 {
		return 0
	}
	pad := 0
	if payload[n-1] == '=' {
		pad++
		if n > 1 && payload[n-2] == '=' {
			pad++
		}
	}
	return n/4*3 - pad
}
