// frontmatter_keys.go — the vocabulary a skill file may declare in.
package skill

const (
	skillFrontmatterDescription      = "description"
	skillFrontmatterName             = "name"
	skillFrontmatterRunAs            = "runas"
	skillFrontmatterContext          = "context"
	skillFrontmatterAgent            = "agent"
	skillFrontmatterAllowedTools     = "allowed-tools"
	skillFrontmatterModel            = "model"
	skillFrontmatterEffort           = "effort"
	skillFrontmatterReadOnly         = "read-only"
	skillFrontmatterTriggers         = "triggers"
	skillFrontmatterNegativeTriggers = "negative-triggers"
	skillFrontmatterAutoUse          = "auto-use"
	skillFrontmatterCost             = "cost"
	skillFrontmatterColor            = "color"
	skillFrontmatterInvocation       = "invocation"
	skillFrontmatterRequires         = "requires"
	skillFrontmatterProfiles         = "profiles"
)

var skillMarkerFrontmatterKeys = []string{
	skillFrontmatterDescription,
	skillFrontmatterName,
	skillFrontmatterRunAs,
	skillFrontmatterContext,
	skillFrontmatterAgent,
	skillFrontmatterAllowedTools,
	skillFrontmatterModel,
	skillFrontmatterEffort,
	skillFrontmatterReadOnly,
	skillFrontmatterTriggers,
	skillFrontmatterNegativeTriggers,
	skillFrontmatterAutoUse,
	skillFrontmatterCost,
	skillFrontmatterColor,
	skillFrontmatterInvocation,
	skillFrontmatterRequires,
	skillFrontmatterProfiles,
}
