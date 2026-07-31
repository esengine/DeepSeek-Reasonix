package outbound

import (
	"testing"
)

func TestDetectReceiveIdType(t *testing.T) {
	tests := []struct {
		name    string
		to      string
		want    ReceiveIdType
		wantErr bool
	}{
		{
			name:    "empty",
			to:      "",
			want:    "",
			wantErr: true,
		},
		{
			name:    "chat_id",
			to:      "oc_1234567890",
			want:    ReceiveIdTypeChatID,
			wantErr: false,
		},
		{
			name:    "open_id",
			to:      "ou_1234567890",
			want:    ReceiveIdTypeOpenID,
			wantErr: false,
		},
		{
			name:    "union_id",
			to:      "on_1234567890",
			want:    ReceiveIdTypeUnionID,
			wantErr: false,
		},
		{
			name:    "email",
			to:      "user@example.com",
			want:    ReceiveIdTypeEmail,
			wantErr: false,
		},
		{
			name:    "user_id (fallback)",
			to:      "123456",
			want:    ReceiveIdTypeUserID,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectReceiveIdType(tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("DetectReceiveIdType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DetectReceiveIdType() got = %v, want %v", got, tt.want)
			}
		})
	}
}
