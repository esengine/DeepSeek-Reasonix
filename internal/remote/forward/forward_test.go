package forward

import "testing"

func TestParseShorthand(t *testing.T) {
	cases := []struct {
		in         string
		bind, targ string
		wantErr    bool
	}{
		{"8080", "127.0.0.1:8080", "127.0.0.1:8080", false},
		{"8080:example.com:80", "127.0.0.1:8080", "example.com:80", false},
		{"127.0.0.1:8080:db:5432", "127.0.0.1:8080", "db:5432", false},
		{"0.0.0.0:9000:svc:9000", "0.0.0.0:9000", "svc:9000", false},
		{":8080:svc:80", "127.0.0.1:8080", "svc:80", false},
		{"8080:svc:", "", "", true},
		{"8080::80", "", "", true},
		{"0", "", "", true},
		{"8080:svc:0", "", "", true},
		{"", "", "", true},
		{"a:b:c:d:e", "", "", true},
	}
	for _, c := range cases {
		s, err := ParseShorthand(Local, c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseShorthand(%q): expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseShorthand(%q): %v", c.in, err)
			continue
		}
		if s.BindAddr != c.bind || s.TargetAddr != c.targ {
			t.Errorf("ParseShorthand(%q) = bind %q target %q, want %q / %q", c.in, s.BindAddr, s.TargetAddr, c.bind, c.targ)
		}
	}
}

func TestSpecValidate(t *testing.T) {
	valid := Spec{Direction: Local, BindAddr: "127.0.0.1:0", TargetAddr: "svc:80"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	for _, spec := range []Spec{
		{Direction: Local, BindAddr: "127.0.0.1", TargetAddr: "svc:80"},
		{Direction: Local, BindAddr: "127.0.0.1:8000", TargetAddr: "svc"},
		{Direction: Local, BindAddr: "127.0.0.1:8000", TargetAddr: "svc:0"},
		{Direction: Direction(99), BindAddr: "127.0.0.1:8000", TargetAddr: "svc:80"},
	} {
		if err := spec.Validate(); err == nil {
			t.Errorf("invalid spec accepted: %+v", spec)
		}
	}
}

func TestNonLoopbackBind(t *testing.T) {
	loop, _ := ParseShorthand(Local, "8080")
	if loop.NonLoopbackBind() {
		t.Error("127.0.0.1 flagged as non-loopback")
	}
	open, _ := ParseShorthand(Local, "0.0.0.0:8080:svc:80")
	if !open.NonLoopbackBind() {
		t.Error("0.0.0.0 not flagged as non-loopback")
	}
}

func TestParseDirection(t *testing.T) {
	for _, s := range []string{"local", "-L", "l"} {
		if d, err := ParseDirection(s); err != nil || d != Local {
			t.Errorf("ParseDirection(%q) = %v, %v", s, d, err)
		}
	}
	for _, s := range []string{"remote", "-R", "R"} {
		if d, err := ParseDirection(s); err != nil || d != Remote {
			t.Errorf("ParseDirection(%q) = %v, %v", s, d, err)
		}
	}
	if _, err := ParseDirection("dynamic"); err == nil {
		t.Error("dynamic direction accepted")
	}
}

func TestDefaultName(t *testing.T) {
	s := Spec{Direction: Local, BindAddr: "127.0.0.1:8080", TargetAddr: "svc:80"}
	if got := s.DefaultName(); got != "L:127.0.0.1:8080->svc:80" {
		t.Errorf("DefaultName = %q", got)
	}
	named := Spec{Name: "web", Direction: Remote}
	if got := named.DefaultName(); got != "web" {
		t.Errorf("DefaultName with name = %q", got)
	}
}
