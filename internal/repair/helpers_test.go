package repair

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestValidateHTTPURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string // substring; empty means valid
	}{
		{"empty", "", "scheme must be http or https"},
		{"https ok", "https://api.example.com", ""},
		{"http with port and path", "http://localhost:8080/v1", ""},
		{"ftp rejected", "ftp://files.example.com", "scheme must be http or https"},
		{"file rejected", "file:///etc/passwd", "scheme must be http or https"},
		{"missing scheme", "example.com", "scheme must be http or https"},
		{"missing host", "https://", "host is required"},
		{"unparsable", "://broken", "missing protocol scheme"},
		{"whitespace trimmed", "  https://api.example.com  ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHTTPURL(tt.raw)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateHTTPURL(%q) unexpected error: %v", tt.raw, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateHTTPURL(%q) = nil, want error containing %q", tt.raw, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateHTTPURL(%q) error = %q, want substring %q", tt.raw, err, tt.wantErr)
			}
		})
	}
}

func TestApplyFailureMatchesUpdate(t *testing.T) {
	tx := &UpdateTransaction{
		ToVersion:  "2.1.0",
		CreatedAt:  "2026-01-01T00:00:00Z",
		Platform:   "test",
		TargetKind: "file",
		TargetPath: "/tmp/target",
	}
	txID := UpdateTransactionID(tx)

	tests := []struct {
		name    string
		failure *UpdateApplyFailure
		tx      *UpdateTransaction
		want    bool
	}{
		{"nil failure", nil, tx, false},
		{"nil transaction", &UpdateApplyFailure{ToVersion: "2.1.0", UpdateTransactionID: txID}, nil, false},
		{"both nil", nil, nil, false},
		{"version mismatch", &UpdateApplyFailure{ToVersion: "2.2.0", UpdateTransactionID: txID}, tx, false},
		{"empty failure version", &UpdateApplyFailure{ToVersion: "", UpdateTransactionID: txID}, tx, false},
		{"empty transaction id", &UpdateApplyFailure{ToVersion: "2.1.0"}, tx, false},
		{"wrong transaction id", &UpdateApplyFailure{ToVersion: "2.1.0", UpdateTransactionID: "deadbeef"}, tx, false},
		{"exact match", &UpdateApplyFailure{ToVersion: "2.1.0", UpdateTransactionID: txID}, tx, true},
		{"padded versions match", &UpdateApplyFailure{ToVersion: "  2.1.0  ", UpdateTransactionID: txID}, tx, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyFailureMatchesUpdate(tt.failure, tt.tx); got != tt.want {
				t.Fatalf("applyFailureMatchesUpdate(%+v, %+v) = %v, want %v", tt.failure, tt.tx, got, tt.want)
			}
		})
	}
}

type redactTestError struct{ msg string }

func (e redactTestError) Error() string { return e.msg }

func TestRedactNetworkError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"plain error", errors.New("connection refused"), "network request failed"},
		{"wrapped error", fmt.Errorf("dial tcp 1.2.3.4:443: %w", errors.New("timeout")), "network request failed"},
		{"custom error type", redactTestError{msg: "proxy auth failed"}, "network request failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactNetworkError(tt.err); got != tt.want {
				t.Fatalf("redactNetworkError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
