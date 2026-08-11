//go:build darwin

package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestDarwinExternalOpenersUseNativeApplicationMetadata(t *testing.T) {
	specs := platformExternalOpenerSpecs()
	finder, ok := externalOpenerByID(specs, "finder")
	if !ok {
		t.Fatal("Finder must always be present on macOS")
	}
	if !strings.HasSuffix(finder.IconSource, "Finder.app") {
		t.Fatalf("Finder icon source = %q, want native application bundle", finder.IconSource)
	}
	if icon := externalOpenerIconDataURL(finder); !strings.HasPrefix(icon, "data:image/png;base64,") {
		t.Fatalf("Finder native icon = %q, want PNG data URL", icon)
	}
	if iterm, installed := externalOpenerByID(specs, "iterm"); installed && iterm.View.Name != "iTerm2" {
		t.Fatalf("iTerm label = %q, want official iTerm2 name", iterm.View.Name)
	}
	if ghostty, installed := externalOpenerByID(specs, "ghostty"); installed && ghostty.LaunchMode != "ghostty" {
		t.Fatalf("Ghostty launch mode = %q, want working-directory aware mode", ghostty.LaunchMode)
	} else if installed {
		path := "/tmp/reasonix workspace"
		want := []string{"/usr/bin/open", "-na", ghostty.Target, "--args", "--working-directory=" + path}
		if got := darwinExternalOpenerCommand(ghostty, path).Args; !reflect.DeepEqual(got, want) {
			t.Fatalf("Ghostty command = %#v, want %#v", got, want)
		}
	}
}

func TestDarwinCatalogIncludesInstalledCodexApplications(t *testing.T) {
	installed := darwinInstalledApplicationIndex()
	specs := platformExternalOpenerSpecs()
	for appName, id := range map[string]string{
		"xcode":          "xcode",
		"android studio": "android-studio",
		"pycharm":        "pycharm",
	} {
		if installed[appName] == "" {
			continue
		}
		if _, ok := externalOpenerByID(specs, id); !ok {
			t.Errorf("installed %s is missing from external opener catalog", appName)
		}
	}
}

func TestDarwinInstalledOpenersExposeNativeIcons(t *testing.T) {
	for _, spec := range platformExternalOpenerSpecs() {
		if spec.IconSource == "" {
			t.Errorf("%s has no native icon source", spec.View.Name)
			continue
		}
		if icon := externalOpenerIconDataURL(spec); !strings.HasPrefix(icon, "data:image/png;base64,") {
			t.Errorf("%s native icon could not be extracted", spec.View.Name)
		}
	}
}
