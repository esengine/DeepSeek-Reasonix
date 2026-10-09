package pluginpkg

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestRereviewExportAcceptedFieldCasing(t *testing.T) {
	for _, upper := range []bool{false, true} {
		fields := map[string]any{"Env": map[string]string{"ORDINARY": "fixturesecret", "REF": "${EXISTING}"}, "Headers": map[string]string{"Authorization": "Basic fixturesecret"}, "Url": "https://host/mcp?%74oken=fixturesecret", "Args": []string{"-eKEY=fixturesecret", "-HAuthorization:Basic fixturesecret", "ordinary"}, "Command": "node --header 'Authorization: Basic fixturesecret'"}
		if upper {
			upperFields := make(map[string]any, len(fields))
			for key, value := range fields {
				upperFields[strings.ToUpper(key)] = value
			}
			fields = upperFields
		}
		root := testenv.TempDir(t)
		body, err := json.Marshal(map[string]any{"MCPServers": map[string]any{"neutral": fields}, "nested": map[string]any{"Runtime": fields}})
		if err != nil {
			t.Fatal(err)
		}
		writeExportFile(t, filepath.Join(root, claudeMCPPath), string(body))
		manifest := &Manifest{}
		_, issues := appendClaudeMCPFile(root, manifest)
		if len(issues) != 0 || manifest.MCPServers["neutral"].URL != "https://host/mcp?%74oken=fixturesecret" {
			t.Fatalf("operational decoder rejected accepted casing: %+v %+v", manifest, issues)
		}
		archive, required, err := Export("neutral", root)
		if err != nil {
			t.Fatal(err)
		}
		out := exportedEntries(t, archive)["neutral/"+claudeMCPPath]
		if strings.Contains(out, "fixturesecret") {
			t.Errorf("export leaked: %s", out)
		}
		if !strings.Contains(out, "${EXISTING}") || !strings.Contains(out, "${NEUTRAL_AUTHORIZATION}") || !strings.Contains(out, "ordinary") || len(required) == 0 {
			t.Errorf("export reference/scope/ordinary contract changed: %s %v", out, required)
		}
	}
}

func TestRereviewExportDecoderUnicodeCaseFolding(t *testing.T) {
	root := testenv.TempDir(t)
	body := `{"mcp\u017ferver\u017f":{"neutral":{"url":"https://host/mcp","Header\u017f":{"Authorization":"Basic fixturesecret"},"Arg\u017f":["--token=fixturesecret"]}}}`
	writeExportFile(t, filepath.Join(root, claudeMCPPath), body)
	manifest := &Manifest{}
	_, issues := appendClaudeMCPFile(root, manifest)
	if len(issues) != 0 || manifest.MCPServers["neutral"].Headers["Authorization"] != "Basic fixturesecret" {
		t.Fatalf("decoder did not accept folded field: %+v %+v", manifest, issues)
	}
	archive, _, err := Export("neutral", root)
	if err != nil {
		t.Fatal(err)
	}
	out := exportedEntries(t, archive)["neutral/"+claudeMCPPath]
	if strings.Contains(out, "fixturesecret") || !strings.Contains(out, "${NEUTRAL_AUTHORIZATION}") {
		t.Fatalf("folded export field escaped projection: %s", out)
	}
}
