package service

import "testing"

func TestSameArgs(t *testing.T) {
	cases := []struct {
		stored string
		args   map[string]any
		want   bool
	}{
		{`{"on":true}`, map[string]any{"on": true}, true},
		{`{"on":true}`, map[string]any{"on": false}, false},
		{`{"a":1,"b":"x"}`, map[string]any{"b": "x", "a": int64(1)}, true}, // 键顺序、整数表示不影响
		{`{"a":1}`, map[string]any{"a": 1.5}, false},
		{`{}`, nil, true},
		{``, map[string]any{}, true},
		{`{"a":1}`, nil, false},
	}
	for i, c := range cases {
		if got := sameArgs([]byte(c.stored), c.args); got != c.want {
			t.Errorf("case %d: sameArgs(%s, %v) = %v, want %v", i, c.stored, c.args, got, c.want)
		}
	}
}
