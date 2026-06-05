//go:build windows

package sysproxy

import (
	"net/url"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winhttp              = windows.NewLazySystemDLL("winhttp.dll")
	procGetIEProxyConfig = winhttp.NewProc("WinHttpGetIEProxyConfigForCurrentUser")
	procOpen             = winhttp.NewProc("WinHttpOpen")
	procGetProxyForURL   = winhttp.NewProc("WinHttpGetProxyForUrl")
	procCloseHandle      = winhttp.NewProc("WinHttpCloseHandle")

	kernel32       = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalFree = kernel32.NewProc("GlobalFree")
)

type ieProxyConfig struct {
	fAutoDetect       int32
	lpszAutoConfigURL *uint16
	lpszProxy         *uint16
	lpszProxyBypass   *uint16
}

type autoproxyOptions struct {
	dwFlags                uint32
	dwAutoDetectFlags      uint32
	lpszAutoConfigURL      *uint16
	lpvReserved            uintptr
	dwReserved             uint32
	fAutoLogonIfChallenged int32
}

type proxyInfo struct {
	dwAccessType    uint32
	lpszProxy       *uint16
	lpszProxyBypass *uint16
}

const (
	fAutoDetect       = 0x00000001
	fConfigURL        = 0x00000002
	detectTypeDHCP    = 0x00000001
	detectTypeDNSA    = 0x00000002
	accessTypeNoProxy = 1
)

// ForURL resolves the Windows system proxy (static IE proxy, PAC autoconfig URL,
// or WPAD auto-detect) for target. Returns nil when the system is set to direct
// or no proxy applies; callers then fall back to env/direct.
func ForURL(target *url.URL) (*url.URL, error) {
	if target == nil {
		return nil, nil
	}
	var ie ieProxyConfig
	if r, _, _ := procGetIEProxyConfig.Call(uintptr(unsafe.Pointer(&ie))); r == 0 {
		return nil, nil
	}
	defer globalFree(ie.lpszProxy)
	defer globalFree(ie.lpszProxyBypass)
	defer globalFree(ie.lpszAutoConfigURL)

	scheme := strings.ToLower(target.Scheme)
	if scheme == "" {
		scheme = "http"
	}

	if ie.lpszProxy != nil {
		bypass := ptrToString(ie.lpszProxyBypass)
		if !bypassed(target.Hostname(), bypass) {
			if u := parseProxyList(ptrToString(ie.lpszProxy), scheme); u != nil {
				return u, nil
			}
		}
	}
	if ie.fAutoDetect != 0 || ie.lpszAutoConfigURL != nil {
		if u := pacProxy(target, ie.lpszAutoConfigURL, scheme); u != nil {
			return u, nil
		}
	}
	return nil, nil
}

func pacProxy(target *url.URL, autoConfigURL *uint16, scheme string) *url.URL {
	session, _, _ := procOpen.Call(0, accessTypeNoProxy, 0, 0, 0)
	if session == 0 {
		return nil
	}
	defer func() { _, _, _ = procCloseHandle.Call(session) }()

	opts := autoproxyOptions{fAutoLogonIfChallenged: 1}
	if autoConfigURL != nil {
		opts.dwFlags = fConfigURL
		opts.lpszAutoConfigURL = autoConfigURL
	} else {
		opts.dwFlags = fAutoDetect
		opts.dwAutoDetectFlags = detectTypeDHCP | detectTypeDNSA
	}

	urlPtr, err := windows.UTF16PtrFromString(target.String())
	if err != nil {
		return nil
	}
	var info proxyInfo
	r, _, _ := procGetProxyForURL.Call(session, uintptr(unsafe.Pointer(urlPtr)), uintptr(unsafe.Pointer(&opts)), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return nil
	}
	defer globalFree(info.lpszProxy)
	defer globalFree(info.lpszProxyBypass)
	if info.lpszProxy == nil {
		return nil
	}
	return parseProxyList(ptrToString(info.lpszProxy), scheme)
}

func ptrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	return windows.UTF16PtrToString(p)
}

func globalFree(p *uint16) {
	if p != nil {
		_, _, _ = procGlobalFree.Call(uintptr(unsafe.Pointer(p)))
	}
}
