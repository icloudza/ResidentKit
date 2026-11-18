package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// makeDeb 按 deb 格式（ar 包：debian-binary、control.tar.gz、data.tar.gz）生成测试文件。
func makeDeb(t *testing.T, control string) string {
	t.Helper()
	var ctl bytes.Buffer
	gz := gzip.NewWriter(&ctl)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755})
	_ = tw.WriteHeader(&tar.Header{Name: "./control", Mode: 0o644, Size: int64(len(control))})
	_, _ = tw.Write([]byte(control))
	_ = tw.Close()
	_ = gz.Close()

	var out bytes.Buffer
	out.WriteString("!<arch>\n")
	member := func(name string, data []byte) {
		fmt.Fprintf(&out, "%-16s%-12d%-6d%-6d%-8s%-10d`\n", name, 0, 0, 0, "100644", len(data))
		out.Write(data)
		if len(data)%2 == 1 {
			out.WriteByte('\n')
		}
	}
	member("debian-binary", []byte("2.0\n"))
	member("control.tar.gz", ctl.Bytes())
	member("data.tar.gz", []byte{1, 2, 3}) // 奇数长度，验证对齐
	p := filepath.Join(t.TempDir(), "x.deb")
	if err := os.WriteFile(p, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadControl(t *testing.T) {
	ctrl := "Package: com.example.pkg\nName: Example\nVersion: 1.2.3\nArchitecture: iphoneos-arm64\n"
	got, err := ReadControl(makeDeb(t, ctrl))
	if err != nil {
		t.Fatal(err)
	}
	if got != "Package: com.example.pkg\nName: Example\nVersion: 1.2.3\nArchitecture: iphoneos-arm64" {
		t.Fatalf("got %q", got)
	}
}

func TestReadControlRejectsNonDeb(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.deb")
	_ = os.WriteFile(p, []byte("not a deb file at all"), 0o600)
	if _, err := ReadControl(p); err == nil {
		t.Fatal("expected error")
	}
}
