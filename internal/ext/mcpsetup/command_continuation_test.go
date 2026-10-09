package mcpsetup

import (
	"reflect"
	"testing"
)

func TestParseContinuedSetupCommands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  string
		single string
	}{
		{"package", "npx -y \\\n  @reasonix/fixture-server@1.2.3 \\\n  --mode read", "npx -y @reasonix/fixture-server@1.2.3 --mode read"},
		{"terminal prompt", "$ npx \\\n  -y @reasonix/fixture-server@1.2.3", "npx -y @reasonix/fixture-server@1.2.3"},
		{"executable", "np\\\nx -y @reasonix/fixture-server", "npx -y @reasonix/fixture-server"},
		{"package word", "npx -y @reasonix/fixture-\\\nserver", "npx -y @reasonix/fixture-server"},
		{"python module", "python3 -m \\\n  reasonix_probe", "python3 -m reasonix_probe"},
		{"uv command", "uvx \\\n  demo-server", "uvx demo-server"},
		{"copied CLI command", "reasonix mcp add docs \\\n  --http https://mcp.example.test/endpoint \\\n  --header 'X-Label=two words'", "reasonix mcp add docs --http https://mcp.example.test/endpoint --header 'X-Label=two words'"},
		{"quoted value", "reasonix mcp add docs --http https://mcp.example.test/endpoint --header \"X-Label=word\\\nwrap\"", "reasonix mcp add docs --http https://mcp.example.test/endpoint --header X-Label=wordwrap"},
		{"bare endpoint", "https://mcp.example.test/endpoint \\\n  --header 'X-Label=two words'", "https://mcp.example.test/endpoint --header 'X-Label=two words'"},
		{"leading continuation", "\\\nnpx -y @reasonix/fixture-server", "npx -y @reasonix/fixture-server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := Parse(tc.single)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Parse(%q) = %+v, want one-line declaration %+v", tc.input, got, want)
			}
		})
	}
}

func TestTokenizeContinuationPreservesLiteralText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  []string
	}{
		{"single quoted newline", "server 'first\\\nsecond'", []string{"server", "first\\\nsecond"}},
		{"quoted plain newline", "server \"first\nsecond\"", []string{"server", "first\nsecond"}},
		{"Windows paths", `C:\tools\server.exe "C:\Users\Fixture User\settings.json"`, []string{`C:\tools\server.exe`, `C:\Users\Fixture User\settings.json`}},
		{"UNC path", `server "\\host\share\settings.json"`, []string{"server", `\\host\share\settings.json`}},
		{"literal double backslash", "server \"first\\\\\nsecond\"", []string{"server", "first\\\\\nsecond"}},
		{"uncontinued newline", "server first\nsecond", []string{"server", "first\nsecond"}},
		{"trailing backslash", "server tail\\", []string{"server", "tail\\"}},
		{"empty quoted argument", "server \"\" \\\n  value", []string{"server", "", "value"}},
		{"Unicode argument", "server \"发布\\\n摘要\"", []string{"server", "发布摘要"}},
		{"only continuation", "\\\n", nil},
		{"consecutive continuations", "server \\\n\\\nvalue", []string{"server", "value"}},
		{"unterminated quote", "server 'two words", []string{"server", "two words"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Tokenize(tc.input); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Tokenize(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
