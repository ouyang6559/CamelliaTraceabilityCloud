package ctxid

import "testing"

func TestHasPerm(t *testing.T) {
	cases := []struct {
		perms    []string
		resource string
		action   string
		want     bool
	}{
		{[]string{"farm:read"}, "farm", "read", true},
		{[]string{"farm:read"}, "farm", "create", false},
		{[]string{"farm:*"}, "farm", "delete", true},
		{[]string{"*:read"}, "plot", "read", true},
		{[]string{"*:read"}, "plot", "create", false},
		{[]string{"*:*"}, "anything", "delete", true},
		{[]string{"farm:read"}, "plot", "read", false},
	}
	for _, c := range cases {
		id := Identity{Perms: c.perms}
		if got := id.HasPerm(c.resource, c.action); got != c.want {
			t.Errorf("HasPerm(%v, %s, %s) = %v, want %v", c.perms, c.resource, c.action, got, c.want)
		}
	}
}
