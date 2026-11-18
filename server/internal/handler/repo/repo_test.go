package repo

import "testing"

func TestFromStart(t *testing.T) {
	for rng, want := range map[string]bool{"": true, "bytes=0-": true, "bytes=0-1023": true, "bytes=1024-": false, " bytes=0-": true} {
		if got := fromStart(rng); got != want {
			t.Errorf("%q: got %v", rng, got)
		}
	}
}

// APT 平铺源请求 <地址>/./Release；Sileo 请求 <地址>/Release，两者要指向同一个文件。
func TestCleanFile(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "Release": "Release", "./Release": "Release", "./Packages.gz": "Packages.gz",
		"debs/1.0.deb": "debs/1.0.deb", "./debs/1.0.deb": "debs/1.0.deb", "../Release": "Release", "a/../Release": "Release",
	} {
		if got := cleanFile(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
