package config

// DefaultAttachmentStreamBytes bounds disk consumption by one streamed file.
const DefaultAttachmentStreamBytes int64 = 1 << 30

// AttachmentsConfig contains user-only limits for ordinary file uploads.
type AttachmentsConfig struct {
	MaxStreamBytes *int64 `toml:"max_stream_bytes,omitempty"`
}

// AttachmentLimit records the effective byte ceiling and its configuration owner.
type AttachmentLimit struct {
	Bytes  int64
	Source string
}

// AttachmentStreamLimit defaults to 1 GiB; an explicit user zero is unlimited.
func (c *Config) AttachmentStreamLimit() AttachmentLimit {
	if c == nil || c.Attachments.MaxStreamBytes == nil || *c.Attachments.MaxStreamBytes < 0 {
		return AttachmentLimit{Bytes: DefaultAttachmentStreamBytes, Source: "default"}
	}
	return AttachmentLimit{Bytes: *c.Attachments.MaxStreamBytes, Source: "user"}
}

// LoadAttachmentStreamLimit strictly reads the user's selected config without
// project declarations, credentials, migrations, or recovery to wider defaults.
func LoadAttachmentStreamLimit() (AttachmentLimit, error) {
	cfg, err := loadForEditStrict(userConfigLoadPath(), false, false)
	if err != nil {
		return AttachmentLimit{}, err
	}
	return cfg.AttachmentStreamLimit(), nil
}
