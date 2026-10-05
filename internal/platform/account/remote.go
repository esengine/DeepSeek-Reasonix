package account

import (
	"context"
	"errors"
	"strings"
)

type RemoteCapability string

const (
	RemoteTasks    RemoteCapability = "tasks"
	RemoteLogs     RemoteCapability = "logs"
	RemoteFiles    RemoteCapability = "files"
	RemoteDesktop  RemoteCapability = "desktop"
	RemoteTerminal RemoteCapability = "terminal"
)

type RemoteDevice struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Platform     string             `json:"platform"`
	PublicKey    string             `json:"publicKey"`
	Capabilities []RemoteCapability `json:"capabilities"`
	LastSeenAt   *string            `json:"lastSeenAt"`
	RevokedAt    *string            `json:"revokedAt"`
}

type RemoteDeviceRegistration struct {
	Name         string             `json:"name"`
	Platform     string             `json:"platform"`
	PublicKey    string             `json:"publicKey"`
	Capabilities []RemoteCapability `json:"capabilities"`
}

type RegisteredRemoteDevice struct {
	Device           RemoteDevice `json:"device"`
	DeviceCredential string       `json:"deviceCredential"`
}

type RemoteAttachmentGrant struct {
	ObjectID       string `json:"objectId"`
	UploadTicket   string `json:"uploadTicket"`
	DownloadTicket string `json:"downloadTicket"`
	MaxBytes       int64  `json:"maxBytes"`
	ExpiresAt      string `json:"expiresAt"`
}

func (c *Client) RegisterRemoteDevice(
	ctx context.Context,
	token string,
	registration RemoteDeviceRegistration,
) (*RegisteredRemoteDevice, error) {
	var out RegisteredRemoteDevice
	if err := c.do(ctx, "POST", "/me/devices", token, registration, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Device.ID) == "" || strings.TrimSpace(out.DeviceCredential) == "" {
		return nil, errors.New("account: device registration returned no credential")
	}
	return &out, nil
}

func (c *Client) RemoteDevices(ctx context.Context, token string) ([]RemoteDevice, error) {
	var out struct {
		Devices []RemoteDevice `json:"devices"`
	}
	if err := c.do(ctx, "GET", "/me/devices", token, nil, &out); err != nil {
		return nil, err
	}
	return out.Devices, nil
}

func (c *Client) IssueRemoteAttachment(
	ctx context.Context,
	token, targetDeviceID string,
	ciphertextBytes int64,
) (*RemoteAttachmentGrant, error) {
	var out struct {
		Attachment RemoteAttachmentGrant `json:"attachment"`
	}
	body := map[string]any{"targetDeviceId": targetDeviceID, "ciphertextBytes": ciphertextBytes}
	if err := c.do(ctx, "POST", "/me/remote-attachments", token, body, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Attachment.ObjectID) == "" || strings.TrimSpace(out.Attachment.UploadTicket) == "" ||
		strings.TrimSpace(out.Attachment.DownloadTicket) == "" {
		return nil, errors.New("account: attachment grant is incomplete")
	}
	return &out.Attachment, nil
}
