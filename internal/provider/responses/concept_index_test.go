package responses

import "testing"

func TestResolveConcept(t *testing.T) {
	cases := []struct{ q, want string }{
		{"霍奇", "Sovereign.Problem.Hodge"},
		{"霍奇猜想", "Sovereign.Problem.Hodge"}, // 后缀匹配
		{"艾森斯坦", "Sovereign.RootMath.Eisenstein"},
		{"环面同伦", "Sovereign.HoTT.T6Homotopy"},
		{"PvsNP", "Sovereign.Problem.PvsNP"},
		{"太长了的查询词超过六个字符限制", ""},
		{"北京天气", ""}, // 非概念词
	}
	for _, c := range cases {
		got, ok := ResolveConcept(c.q)
		if c.want == "" {
			if ok {
				t.Errorf("ResolveConcept(%q) = %q, want miss", c.q, got)
			}
			continue
		}
		if !ok || got != c.want {
			t.Errorf("ResolveConcept(%q) = %q,%v want %q", c.q, got, ok, c.want)
		}
	}
}

func TestHasFreshnessIntent(t *testing.T) {
	cases := []struct {
		q    string
		want bool
	}{
		{"2026年数论最新突破", true},
		{"ABC conjecture proof status 2026", true},
		{"recent progress Riemann", true},
		{"霍奇猜想是什么", false}, // 静态概念
		{"艾森斯坦整数", false},
	}
	for _, c := range cases {
		if got := HasFreshnessIntent(c.q); got != c.want {
			t.Errorf("HasFreshnessIntent(%q) = %v, want %v", c.q, got, c.want)
		}
	}
}
