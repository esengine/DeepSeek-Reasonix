package bot

import (
	"crypto/sha256"
	"encoding/hex"
)

// hashID 对用户/群聊 ID 做脱敏哈希，用于日志和状态展示。
func hashID(id string) string {
	if id == "" {
		return ""
	}
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:])[:12]
}
