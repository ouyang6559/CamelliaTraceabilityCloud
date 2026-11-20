package alert

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		op        string
		value     float64
		threshold float64
		want      bool
	}{
		{">", 41, 40, true},
		{">", 40, 40, false},
		{">=", 40, 40, true},
		{"<", 17, 18, true},
		{"<=", 18, 18, true},
		{"==", 18, 18, true},
		{"=", 18, 18, true},
		{"=", 19, 18, false},
		{"<>", 18, 18, false}, // unsupported operator never fires
	}
	for _, c := range cases {
		if got := compare(c.op, c.value, c.threshold); got != c.want {
			t.Errorf("compare(%q, %v, %v) = %v, want %v", c.op, c.value, c.threshold, got, c.want)
		}
	}
}
