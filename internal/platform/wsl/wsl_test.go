package wsl

import (
	"context"
	"errors"
	"slices"
	"testing"
	"unicode/utf16"
)

func utf16le(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(string(rune(0xFEFF)) + s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

func TestTheListIsReadByStructureWhateverTheLanguage(t *testing.T) {
	for name, header := range map[string]string{"english": "  NAME      STATE           VERSION", "chinese": "  名称      状态            版本"} {
		out := header + "\r\n* Ubuntu-22.04    Running         2\r\n  Debian          已停止          1\r\n"
		got, err := ParseList(Decode(utf16le(out)))
		want := []Distro{{Name: "Ubuntu-22.04", Default: true, Version: 2}, {Name: "Debian", Version: 1}}
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
}

func TestAListRowThatIsNotARowIsRefused(t *testing.T) {
	for _, out := range []string{"NAME STATE VERSION\nUbuntu Running\n", "NAME STATE VERSION\nUbuntu Running 3\n", "NAME STATE VERSION\nUbuntu Running x\n"} {
		if _, err := ParseList(out); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q = %v", out, err)
		}
	}
}

func TestDecodeTakesUTF8AsItIs(t *testing.T) {
	if got := Decode([]byte(string(rune(0xFEFF)) + "hi\n")); got != "hi\n" {
		t.Fatalf("%q", got)
	}
}

func fakeRun(list string, answers map[string]string) (Runner, *[][]string) {
	var calls [][]string
	return func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		if args[0] == "-l" {
			return utf16le(list), nil
		}
		a, ok := answers[args[len(args)-1]+args[len(args)-2]]
		if !ok {
			return nil, errors.New("exit status 1")
		}
		return []byte(a), nil
	}, &calls
}

const oneDistro = "NAME STATE VERSION\n* Ubuntu Running 2\n"

func TestCheckAsksAListedDistributionWithFixedArgv(t *testing.T) {
	run, calls := fakeRun(oneDistro, map[string]string{"-muname": "x86_64\n", "-unid": "dev\n"})
	p, err := Check(context.Background(), run, "Ubuntu")
	if err != nil || p.Arch != "amd64" || p.User != "dev" || p.Distro.Version != 2 {
		t.Fatalf("Check = %+v, %v", p, err)
	}
	want := [][]string{{"-l", "-v"}, {"-d", "Ubuntu", "--exec", "uname", "-m"}, {"-d", "Ubuntu", "--exec", "id", "-un"}}
	if len(*calls) != 3 || !slices.Equal((*calls)[1], want[1]) || !slices.Equal((*calls)[2], want[2]) {
		t.Fatalf("argv = %v", *calls)
	}
}

func TestANameThatIsNotListedNeverReachesWslExe(t *testing.T) {
	run, calls := fakeRun(oneDistro, nil)
	for _, name := range []string{"Other", "--shutdown", "Ubuntu; calc", ""} {
		if _, err := Check(context.Background(), run, name); !errors.Is(err, ErrNoDistro) {
			t.Errorf("%q = %v", name, err)
		}
	}
	for _, c := range *calls {
		if c[0] != "-l" {
			t.Errorf("wsl.exe was run with %v", c)
		}
	}
}

func TestAnArchitectureNobodyBuildsForIsUnsupported(t *testing.T) {
	run, _ := fakeRun(oneDistro, map[string]string{"-muname": "riscv64\n"})
	if _, err := Check(context.Background(), run, "Ubuntu"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("riscv64 = %v", err)
	}
}

func TestNothingInstalledIsAnEmptyListButNoWslIsUnavailable(t *testing.T) {
	none := func(context.Context, ...string) ([]byte, error) { return nil, errors.New("exit status 1") }
	if l, err := List(context.Background(), none); err != nil || len(l) != 0 {
		t.Fatalf("List = %v, %v", l, err)
	}
	gone := func(context.Context, ...string) ([]byte, error) { return nil, ErrUnavailable }
	if _, err := List(context.Background(), gone); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("List = %v", err)
	}
}

func TestSystemIsUnavailableOffWindows(t *testing.T) {
	if _, err := List(context.Background(), System()); err != nil && !errors.Is(err, ErrUnavailable) {
		t.Fatalf("List = %v", err)
	}
}
