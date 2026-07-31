package registration

const (
	StatusPolling        = "polling"
	StatusSlowDown       = "slow_down"
	StatusDomainSwitched = "domain_switched"
)

type QRCodeInfo struct {
	URL      string
	ExpireIn int
}

type StatusChangeInfo struct {
	Status   string
	Interval int
}

// AppPreset contains values used to pre-fill the app creation page after the
// user opens the QR code URL. Users can still edit these values on the page;
// the final app values are whatever the user submits.
type AppPreset struct {
	// Avatar contains app avatar URLs. Use one entry for a single avatar, or
	// 1-6 entries for candidates. The first entry is selected by default.
	// Values are URL-encoded by the SDK when building the QR URL.
	Avatar []string

	// Name is the app name shown on the app creation page. It supports the
	// {user} placeholder, which is resolved by the web page for the scanning
	// user.
	Name string

	// Desc is the app description shown on the app creation page. It supports
	// the {user} placeholder, which is resolved by the web page for the scanning
	// user.
	Desc string
}

type UserInfo struct {
	OpenID      string
	TenantBrand string
}

type RegisterAppResult struct {
	ClientID     string
	ClientSecret string
	UserInfo     *UserInfo
}

type Options struct {
	Source         string
	Domain         string
	LarkDomain     string
	AppPreset      *AppPreset
	OnQRCode       func(info *QRCodeInfo)
	OnStatusChange func(info *StatusChangeInfo)
}

type beginResponse struct {
	DeviceCode              string `json:"device_code"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	VerificationURI         string `json:"verification_uri"`
	UserCode                string `json:"user_code"`
	Interval                int    `json:"interval"`
	ExpireIn                int    `json:"expire_in"`
}

type pollResponse struct {
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	UserInfo     *userInfo `json:"user_info,omitempty"`
	Error        string    `json:"error,omitempty"`
	ErrorDesc    string    `json:"error_description,omitempty"`
}

type userInfo struct {
	OpenID      string `json:"open_id,omitempty"`
	TenantBrand string `json:"tenant_brand,omitempty"`
}
