package config

import (
	"fmt"
	"strings"
)

const DefaultAttachmentStreamBytes int64 = 1 << 30

type AttachmentsConfig struct {
	MaxStreamBytes *int64 `toml:"max_stream_bytes,omitempty"`
}

type AttachmentLimit struct {
	Bytes  int64
	Source string
}

func (c *Config) AttachmentStreamLimit() AttachmentLimit {
	if c != nil && c.Attachments.MaxStreamBytes != nil && *c.Attachments.MaxStreamBytes >= 0 {
		return AttachmentLimit{Bytes: *c.Attachments.MaxStreamBytes, Source: "user"}
	}
	return AttachmentLimit{Bytes: DefaultAttachmentStreamBytes, Source: "default"}
}

// Read the user's file strictly: a malformed safety setting must not silently
// fall back to a larger limit, and a workspace cannot supply this authority.
func LoadAttachmentStreamLimit() (AttachmentLimit, error) {
	cfg, err := loadForEditStrict(processRoots().userConfigLoadPath(), false, false)
	if err != nil {
		return AttachmentLimit{}, err
	}
	return cfg.AttachmentStreamLimit(), nil
}

func cloneAttachments(c AttachmentsConfig) AttachmentsConfig {
	if c.MaxStreamBytes != nil {
		value := *c.MaxStreamBytes
		c.MaxStreamBytes = &value
	}
	return c
}

func renderAttachments(b *strings.Builder, c *Config, scope RenderScope) {
	if scope != RenderScopeProject && c.Attachments.MaxStreamBytes != nil {
		fmt.Fprintf(b, "[attachments] # user/global only; 0 disables the stream byte ceiling\nmax_stream_bytes = %d\n\n", *c.Attachments.MaxStreamBytes)
	}
}
