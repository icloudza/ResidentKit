package v1

import "testing"

func TestDeviceURL(t *testing.T) {
	cases := map[string]string{
		"https://rk.example.com":  "wss://rk.example.com/rk",
		"https://rk.example.com/": "wss://rk.example.com/rk",
		"http://10.0.0.2:8765":    "ws://10.0.0.2:8765/rk",
		"":                        "",
	}
	for in, want := range cases {
		if got := deviceURL(in, "/rk"); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
