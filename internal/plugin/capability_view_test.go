package plugin

import (
	"strings"
	"testing"
)

func TestCapabilityViewsWithoutServers(t *testing.T) {
	for _, profile := range []HostProfile{HostProfileCore, HostProfileInteractive, HostProfileDesktopApps} {
		t.Run(profile.String(), func(t *testing.T) {
			h := NewHostWithProfile(profile)
			views := h.CapabilityViews()
			if len(views) != 4 {
				t.Fatalf("views = %d, want 4", len(views))
			}
			wantIDs := []string{"protocol", "core", "interactive", "apps"}
			for i, id := range wantIDs {
				if views[i].ID != id {
					t.Fatalf("view[%d].ID = %q, want %q", i, views[i].ID, id)
				}
			}
			wantInteractive := CapabilityStateUnavailable
			if profile.Capabilities().ElicitationForms {
				wantInteractive = CapabilityStateSupported
			}
			if views[2].State != wantInteractive {
				t.Fatalf("interactive state = %q, want %q", views[2].State, wantInteractive)
			}
			wantApps := CapabilityStateUnavailable
			if profile.Capabilities().AppsUI {
				wantApps = CapabilityStateSupported
			}
			if views[3].State != wantApps {
				t.Fatalf("apps state = %q, want %q", views[3].State, wantApps)
			}
			for _, v := range views {
				if v.State == CapabilityStateNegotiated {
					t.Fatalf("layer %s negotiated with zero servers", v.ID)
				}
				if v.Detail == "" {
					t.Fatalf("layer %s has empty detail", v.ID)
				}
			}
		})
	}
}

func TestCapabilityViewsLayersAndFormat(t *testing.T) {
	h := NewHostWithProfile(HostProfileDesktopApps)
	views := h.CapabilityViews()
	wantLayers := []string{LayerProtocol, LayerCore, LayerInteractive, LayerApps}
	for i, want := range wantLayers {
		if views[i].Layer != want {
			t.Fatalf("view[%d].Layer = %q, want %q", i, views[i].Layer, want)
		}
	}
	formatted := FormatCapabilityViews(views)
	for _, want := range wantLayers {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted matrix missing layer %q:\n%s", want, formatted)
		}
	}
	SortCapabilityViews(views)
	for i, id := range []string{"protocol", "core", "interactive", "apps"} {
		if views[i].ID != id {
			t.Fatalf("unsorted after SortCapabilityViews: %v", views)
		}
	}
}

func TestServerStatusCarriesProfile(t *testing.T) {
	h := NewHostWithProfile(HostProfileInteractive)
	if got := h.Profile(); got != HostProfileInteractive {
		t.Fatalf("profile = %q", got)
	}
	for _, s := range h.Servers() {
		if s.HostProfile != HostProfileInteractive.String() {
			t.Fatalf("server status profile = %q", s.HostProfile)
		}
	}
}

func TestProfileNormalizeMapsUnknownToCore(t *testing.T) {
	if got := HostProfile("future-v9").Normalize(); got != HostProfileCore {
		t.Fatalf("unknown profile normalized to %q", got)
	}
	if got := HostProfile("").UsesEnhancedCache(); got {
		t.Fatal("empty profile must not use the enhanced cache")
	}
}
