package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const ImageUnavailablePlaceholder = "该图片不可用，无法据此进行视觉判断"

type ImageIdentity struct {
	MessageID     string `json:"messageId"`
	ImageOrdinal  int    `json:"imageOrdinal"`
	ContentDigest string `json:"contentDigest"`
}

type ImageIsolationScope struct {
	Kind     string `json:"kind"`
	Provider string `json:"provider,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	Variant  string `json:"variant,omitempty"`
}

type ImageIsolationDecision struct {
	Version  int                 `json:"version"`
	Identity ImageIdentity       `json:"identity"`
	Reason   string              `json:"reason"`
	Scope    ImageIsolationScope `json:"scope"`
}

type ImageRequestIdentity struct {
	Provider string
	Protocol string
	Endpoint string
	Model    string
	Variant  string
}

type ImageRequestIdentityProvider interface {
	ImageRequestIdentity() ImageRequestIdentity
}

func ResolveImageRequestIdentity(p Provider, modelRef string) ImageRequestIdentity {
	identity := ImageRequestIdentity{Model: strings.TrimSpace(modelRef)}
	if p != nil {
		identity.Provider = strings.TrimSpace(p.Name())
	}
	if described, ok := p.(ImageRequestIdentityProvider); ok {
		configured := described.ImageRequestIdentity()
		if configured.Provider != "" {
			identity.Provider = strings.TrimSpace(configured.Provider)
		}
		identity.Protocol = strings.TrimSpace(configured.Protocol)
		identity.Endpoint = strings.TrimSpace(configured.Endpoint)
		if configured.Model != "" {
			identity.Model = strings.TrimSpace(configured.Model)
		}
		identity.Variant = strings.TrimSpace(configured.Variant)
	}
	return identity
}

func (s ImageIsolationScope) Matches(identity ImageRequestIdentity) bool {
	switch strings.TrimSpace(s.Kind) {
	case "content":
		return true
	case "provider":
		return equalOptional(s.Provider, identity.Provider) &&
			equalOptional(s.Protocol, identity.Protocol) &&
			equalOptional(s.Endpoint, identity.Endpoint) &&
			equalOptional(s.Model, identity.Model) &&
			equalOptional(s.Variant, identity.Variant)
	default:
		return false
	}
}

func equalOptional(expected, actual string) bool {
	expected = strings.TrimSpace(expected)
	return expected == "" || expected == strings.TrimSpace(actual)
}

func ImageContentDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func ImageDecisionKey(decision ImageIsolationDecision) string {
	scope := decision.Scope
	raw := strings.Join([]string{
		decision.Identity.MessageID,
		fmt.Sprint(decision.Identity.ImageOrdinal),
		decision.Identity.ContentDigest,
		scope.Kind, scope.Provider, scope.Protocol, scope.Endpoint, scope.Model, scope.Variant,
	}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type ImageRequestError struct {
	APIError   *APIError
	Reason     string
	Identity   ImageIdentity
	Candidates []ImageIdentity
	Scope      ImageIsolationScope
}

// ImageRecoveryCandidate is safe display metadata for one request image. It
// contains identity only; image bytes, URLs, prompts, and provider bodies stay
// out of the recovery surface.
type ImageRecoveryCandidate struct {
	Identity ImageIdentity `json:"identity"`
	Label    string        `json:"label"`
}

// ImageRecoveryAction asks the user to choose the images to isolate after an
// explicit provider image rejection could not be mapped to one image.
type ImageRecoveryAction struct {
	ID         string                   `json:"id"`
	Reason     string                   `json:"reason"`
	Candidates []ImageRecoveryCandidate `json:"candidates"`
}

func (e *ImageRequestError) Error() string {
	if e == nil {
		return "image request rejected"
	}
	if e.APIError != nil {
		return e.APIError.Error()
	}
	if strings.TrimSpace(e.Reason) != "" {
		return "image request rejected: " + strings.TrimSpace(e.Reason)
	}
	return "image request rejected"
}

func (e *ImageRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.APIError
}

func AsImageRequestError(err error) *ImageRequestError {
	var imageErr *ImageRequestError
	if errors.As(err, &imageErr) {
		return imageErr
	}
	return nil
}

func (e *ImageRequestError) UniqueIdentity() (ImageIdentity, bool) {
	if e == nil {
		return ImageIdentity{}, false
	}
	if validImageIdentity(e.Identity) {
		if len(e.Candidates) == 0 || (len(e.Candidates) == 1 && e.Candidates[0] == e.Identity) {
			return e.Identity, true
		}
	}
	if len(e.Candidates) == 1 && validImageIdentity(e.Candidates[0]) {
		return e.Candidates[0], true
	}
	return ImageIdentity{}, false
}

func ImageRecoveryForError(e *ImageRequestError) *ImageRecoveryAction {
	if e == nil {
		return nil
	}
	if _, unique := e.UniqueIdentity(); unique || len(e.Candidates) < 2 {
		return nil
	}
	candidates := make([]ImageRecoveryCandidate, 0, len(e.Candidates))
	seen := map[ImageIdentity]bool{}
	for _, identity := range e.Candidates {
		if !validImageIdentity(identity) || seen[identity] {
			continue
		}
		seen[identity] = true
		candidates = append(candidates, ImageRecoveryCandidate{
			Identity: identity,
			Label:    fmt.Sprintf("Image %d in message %s", identity.ImageOrdinal+1, identity.MessageID),
		})
	}
	if len(candidates) < 2 {
		return nil
	}
	hashInput := []string{e.Reason, e.Scope.Kind, e.Scope.Provider, e.Scope.Protocol, e.Scope.Endpoint, e.Scope.Model, e.Scope.Variant}
	for _, candidate := range candidates {
		hashInput = append(hashInput, candidate.Identity.MessageID, fmt.Sprint(candidate.Identity.ImageOrdinal), candidate.Identity.ContentDigest)
	}
	sum := sha256.Sum256([]byte(strings.Join(hashInput, "\x00")))
	return &ImageRecoveryAction{ID: hex.EncodeToString(sum[:16]), Reason: e.Reason, Candidates: candidates}
}

func validImageIdentity(identity ImageIdentity) bool {
	return strings.TrimSpace(identity.MessageID) != "" && identity.ImageOrdinal >= 0 && strings.TrimSpace(identity.ContentDigest) != ""
}

func ApplyImageIsolation(messages []Message, decisions []ImageIsolationDecision, request ImageRequestIdentity) ([]Message, bool) {
	if len(messages) == 0 || len(decisions) == 0 {
		return messages, false
	}
	byMessage := make(map[string][]ImageIsolationDecision)
	for _, decision := range decisions {
		if decision.Version != 1 || !decision.Scope.Matches(request) {
			continue
		}
		byMessage[decision.Identity.MessageID] = append(byMessage[decision.Identity.MessageID], decision)
	}
	if len(byMessage) == 0 {
		return messages, false
	}
	out := append([]Message(nil), messages...)
	changed := false
	for i := range out {
		candidates := byMessage[out[i].ID]
		if len(candidates) == 0 || len(out[i].Images) == 0 {
			continue
		}
		kept := make([]string, 0, len(out[i].Images))
		removed := false
		for ordinal, image := range out[i].Images {
			digest := ImageContentDigest(image)
			matched := false
			for _, decision := range candidates {
				identity := decision.Identity
				if identity.ImageOrdinal == ordinal && identity.ContentDigest == digest {
					matched = true
					break
				}
			}
			if matched {
				removed = true
				continue
			}
			kept = append(kept, image)
		}
		if !removed {
			continue
		}
		changed = true
		out[i].Images = kept
		out[i].Content = appendImageUnavailablePlaceholder(out[i].Content)
	}
	return out, changed
}

func appendImageUnavailablePlaceholder(content string) string {
	if strings.Contains(content, ImageUnavailablePlaceholder) {
		return content
	}
	if strings.TrimSpace(content) == "" {
		return ImageUnavailablePlaceholder
	}
	return content + "\n\n" + ImageUnavailablePlaceholder
}
