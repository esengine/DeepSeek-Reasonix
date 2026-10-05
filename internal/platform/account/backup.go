package account

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Backup is one stored configuration backup. The service holds the sealed
// envelope and this metadata; it never sees what the envelope contains.
type Backup struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Format          int      `json:"format"`
	AppVersion      string   `json:"appVersion"`
	Platform        string   `json:"platform"`
	Categories      []string `json:"categories"`
	CiphertextBytes int64    `json:"ciphertextBytes"`
	CreatedAt       string   `json:"createdAt"`
}

// BackupUpload is what the client sends besides the envelope itself.
type BackupUpload struct {
	Label      string   `json:"label"`
	Format     int      `json:"format"`
	AppVersion string   `json:"appVersion"`
	Platform   string   `json:"platform"`
	Categories []string `json:"categories"`
}

// BackupLimits is the per-account quota the service enforces.
type BackupLimits struct {
	MaxCount int   `json:"maxCount"`
	MaxBytes int64 `json:"maxBytes"`
}

// Errors the backup routes answer with, by the service's own code.
var (
	ErrBackupNotFound     = errors.New("account: no such backup")
	ErrBackupLimit        = errors.New("account: backup limit reached")
	ErrBackupTooLarge     = errors.New("account: backup is larger than the service accepts")
	ErrBackupsUnavailable = errors.New("account: backup storage is unavailable")
)

const backupReadLimit = 8 << 20

func (c *Client) ListBackups(ctx context.Context, token string) ([]Backup, BackupLimits, error) {
	var out struct {
		Backups []Backup     `json:"backups"`
		Limits  BackupLimits `json:"limits"`
	}
	if err := c.do(ctx, http.MethodGet, "/me/backups", token, nil, &out); err != nil {
		return nil, BackupLimits{}, backupError(err)
	}
	return out.Backups, out.Limits, nil
}

func (c *Client) UploadBackup(ctx context.Context, token string, meta BackupUpload, envelope []byte) (*Backup, error) {
	body := struct {
		BackupUpload
		Envelope string `json:"envelope"`
	}{meta, base64.StdEncoding.EncodeToString(envelope)}
	var out struct {
		Backup Backup `json:"backup"`
	}
	if err := c.do(ctx, http.MethodPost, "/me/backups", token, body, &out); err != nil {
		return nil, backupError(err)
	}
	return &out.Backup, nil
}

func (c *Client) DownloadBackup(ctx context.Context, token, id string) (*Backup, []byte, error) {
	var out struct {
		Backup   Backup `json:"backup"`
		Envelope string `json:"envelope"`
	}
	if err := c.doLimit(ctx, http.MethodGet, "/me/backups/"+url.PathEscape(id), token, nil, &out, backupReadLimit); err != nil {
		return nil, nil, backupError(err)
	}
	envelope, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out.Envelope))
	if err != nil {
		return nil, nil, errors.New("account: backup download is not base64")
	}
	return &out.Backup, envelope, nil
}

func (c *Client) DeleteBackup(ctx context.Context, token, id string) error {
	return backupError(c.do(ctx, http.MethodDelete, "/me/backups/"+url.PathEscape(id), token, nil, nil))
}

func backupError(err error) error {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		return err
	}
	switch {
	case apiErr.Status == http.StatusUnauthorized:
		return ErrUnauthorized
	case apiErr.Code == "backup_not_found":
		return ErrBackupNotFound
	case apiErr.Code == "backup_limit_reached":
		return ErrBackupLimit
	case apiErr.Code == "backup_too_large":
		return ErrBackupTooLarge
	case apiErr.Code == "backups_unavailable":
		return ErrBackupsUnavailable
	}
	return err
}
