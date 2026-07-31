package outbound

import (
	"errors"
	"strings"
)

// ReceiveIdType defines the type of the receiver ID.
type ReceiveIdType string

const (
	ReceiveIdTypeChatID  ReceiveIdType = "chat_id"
	ReceiveIdTypeOpenID  ReceiveIdType = "open_id"
	ReceiveIdTypeUserID  ReceiveIdType = "user_id"
	ReceiveIdTypeUnionID ReceiveIdType = "union_id"
	ReceiveIdTypeEmail   ReceiveIdType = "email"
)

// DetectReceiveIdType infers Feishu's receive_id_type from the prefix of a target id.
//
//	oc_*          → chat_id
//	ou_*          → open_id
//	on_*          → union_id
//	contains '@'  → email
//	fallback      → user_id
func DetectReceiveIdType(to string) (ReceiveIdType, error) {
	if to == "" {
		return "", errors.New("empty receive_id")
	}
	if strings.HasPrefix(to, "oc_") {
		return ReceiveIdTypeChatID, nil
	}
	if strings.HasPrefix(to, "ou_") {
		return ReceiveIdTypeOpenID, nil
	}
	if strings.HasPrefix(to, "on_") {
		return ReceiveIdTypeUnionID, nil
	}
	if strings.Contains(to, "@") {
		return ReceiveIdTypeEmail, nil
	}
	return ReceiveIdTypeUserID, nil
}
