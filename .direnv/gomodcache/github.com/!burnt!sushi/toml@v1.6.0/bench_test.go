package toml_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	tomltest "github.com/BurntSushi/toml/internal/toml-test"
)

func BenchmarkDecode(b *testing.B) {
	files := make(map[string][]string)
	fs.WalkDir(tomltest.EmbeddedTests(), ".", func(path string, d fs.DirEntry, err error) error {
		if strings.HasPrefix(path, "valid/") && strings.HasSuffix(path, ".toml") {
			d, _ := fs.ReadFile(tomltest.EmbeddedTests(), path)
			g := filepath.Dir(path[6:])
			if g == "." {
				g = "top"
			}
			files[g] = append(files[g], string(d))
		}
		return nil
	})

	type test struct {
		group string
		toml  []string
	}
	tests := make([]test, 0, len(files))
	for k, v := range files {
		tests = append(tests, test{group: k, toml: v})
	}
	sort.Slice(tests, func(i, j int) bool { return tests[i].group < tests[j].group })

	b.ResetTimer()
	for _, tt := range tests {
		b.Run(tt.group, func(b *testing.B) {
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				for _, f := range tt.toml {
					var val map[string]any
					toml.Decode(f, &val)
				}
			}
		})
	}

	b.Run("large-doc", func(b *testing.B) {
		d, err := os.ReadFile("testdata/Cargo.toml")
		if err != nil {
			b.Fatal(err)
		}
		doc := string(d)

		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			var val map[string]any
			toml.Decode(doc, &val)
		}
	})
}

func BenchmarkEncode(b *testing.B) {
	files := make(map[string][]map[string]any)
	fs.WalkDir(tomltest.EmbeddedTests(), ".", func(path string, d fs.DirEntry, err error) error {
		if strings.HasPrefix(path, "valid/") && strings.HasSuffix(path, ".toml") {
			d, _ := fs.ReadFile(tomltest.EmbeddedTests(), path)
			g := filepath.Dir(path[6:])
			if g == "." {
				g = "top"
			}

			// "next" version of TOML.
			switch path {
			case "valid/string/escape-esc.toml", "valid/datetime/no-seconds.toml",
				"valid/string/hex-escape.toml", "valid/inline-table/newline.toml":
				return nil
			}

			var dec map[string]any
			_, err := toml.Decode(string(d), &dec)
			if err != nil {
				b.Fatalf("decode %q: %s", path, err)
			}

			buf := new(bytes.Buffer)
			err = toml.NewEncoder(buf).Encode(dec)
			if err != nil {
				b.Logf("encode failed for %q (skipping): %s", path, err)
				return nil
			}

			files[g] = append(files[g], dec)
		}
		return nil
	})

	type test struct {
		group string
		data  []map[string]any
	}
	tests := make([]test, 0, len(files))
	for k, v := range files {
		tests = append(tests, test{group: k, data: v})
	}
	sort.Slice(tests, func(i, j int) bool { return tests[i].group < tests[j].group })

	b.ResetTimer()
	for _, tt := range tests {
		b.Run(tt.group, func(b *testing.B) {
			buf := new(bytes.Buffer)
			buf.Grow(1024 * 64)
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				for _, f := range tt.data {
					toml.NewEncoder(buf).Encode(f)
				}
			}
		})
	}
}

func BenchmarkExample(b *testing.B) {
	d, err := os.ReadFile("_example/example.toml")
	if err != nil {
		b.Fatal(err)
	}
	t := string(d)

	var decoded example
	_, err = toml.Decode(t, &decoded)
	if err != nil {
		b.Fatal(err)
	}

	buf := new(bytes.Buffer)
	err = toml.NewEncoder(buf).Encode(decoded)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.Run("decode", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			var c example
			toml.Decode(t, &c)
		}
	})

	b.Run("encode", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			buf.Reset()
			toml.NewEncoder(buf).Encode(decoded)
		}
	})
}

// Copy from _example/example.go
type (
	example struct {
		Title      string
		Integers   []int
		Times      []fmtTime
		Duration   []duration
		Distros    []distro
		Servers    map[string]server
		Characters map[string][]struct {
			Name string
			Rank string
		}
	}

	server struct {
		IP       string
		Hostname string
		Enabled  bool
	}

	distro struct {
		Name     string
		Packages string
	}

	duration struct{ time.Duration }
	fmtTime  struct{ time.Time }
)

func (d *duration) UnmarshalText(text []byte) (err error) {
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

func (t fmtTime) String() string {
	f := "2006-01-02 15:04:05.999999999"
	if t.Time.Hour() == 0 {
		f = "2006-01-02"
	}
	if t.Time.Year() == 0 {
		f = "15:04:05.999999999"
	}
	if t.Time.Location() == time.UTC {
		f += " UTC"
	} else {
		f += " -0700"
	}
	return t.Time.Format(`"` + f + `"`)
}
