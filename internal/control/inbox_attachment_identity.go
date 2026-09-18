package control

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"reasonix/internal/sessioninbox"
)

func inboxAttachmentFingerprint(req InboxRequest, env sessioninbox.PromptEnvelope) string {
	intent := req.Intent
	if intent != sessioninbox.IntentSteer {
		intent = sessioninbox.IntentFollowup
	}
	identity := canonicalSubmissionFingerprint(SubmissionRequest{
		Input: env.SubmitText, Display: env.DisplayText, Original: env.RawText,
		Action: string(intent), Format: req.Format, Invocations: req.Invocations, Attachments: req.Attachments,
	})
	stable := struct {
		Request      string
		Source       string
		Extra        map[string]string
		ExplicitRefs []string
	}{identity, env.Source, env.Extra, env.ExplicitRefs}
	body, _ := json.Marshal(stable)
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}
