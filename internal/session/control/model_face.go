package control

import "reasonix/internal/contract/config"

// ModelFace is what the build resolved about the session's model: the facts a
// status read needs without loading configuration again.
type ModelFace struct {
	Ref            string
	Effort         string
	Vision         bool
	VisionDeclared bool
}

func faceOfEntry(e *config.ProviderEntry) *ModelFace {
	if e == nil {
		return nil
	}
	return &ModelFace{
		Ref:            e.Name + "/" + e.Model,
		Effort:         e.Effort,
		Vision:         config.EffectiveVision(e),
		VisionDeclared: config.VisionDeclared(e),
	}
}

// ModelFace reports the session's model as resolved at build, false when the
// controller was assembled without a resolved entry.
func (c *Controller) ModelFace() (ModelFace, bool) {
	if c.modelFace == nil {
		return ModelFace{}, false
	}
	return *c.modelFace, true
}
