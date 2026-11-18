package release

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignVerify(t *testing.T) {
	privPEM, pub, err := GenerateKey()
	require.NoError(t, err)
	k, err := ParsePrivateKey(privPEM)
	require.NoError(t, err)
	pk, err := ParsePublicKey(pub)
	require.NoError(t, err)

	m := Manifest{Package: Package, Version: "1.6.0", Size: 123, SHA256: "ab" + string(make([]byte, 0)) + "cdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"}
	require.NoError(t, m.Validate())
	require.NoError(t, Sign(k, &m))
	require.NoError(t, Verify(pk, m))

	tampered := m
	tampered.Version = "1.6.1"
	require.Error(t, Verify(pk, tampered))

	_, other, _ := GenerateKey()
	opk, _ := ParsePublicKey(other)
	require.Error(t, Verify(opk, m))
}

func TestCompareVersions(t *testing.T) {
	require.Equal(t, -1, CompareVersions("1.5.0", "1.6.0"))
	require.Equal(t, 1, CompareVersions("1.10.0", "1.9.9"))
	require.Equal(t, 0, CompareVersions("2.0.0", "2.0.0"))
}

func TestBundleRoundTrip(t *testing.T) {
	privPEM, _, _ := GenerateKey()
	k, _ := ParsePrivateKey(privPEM)
	deb := []byte("fake deb content")
	size, sum, _ := Hash(bytes.NewReader(deb))
	m := Manifest{Package: Package, Version: "1.2.3", Size: size, SHA256: sum}
	if err := Sign(k, &m); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "x.rkrel.zip")
	f, _ := os.Create(p)
	if err := WriteBundle(f, m, bytes.NewReader(deb)); err != nil {
		t.Fatal(err)
	}
	f.Close()

	b, err := OpenBundle(p)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Manifest != m || Verify(&k.PublicKey, b.Manifest) != nil {
		t.Fatalf("manifest mismatch: %+v", b.Manifest)
	}
	if err := b.ExtractPackage(filepath.Join(dir, "out.deb")); err != nil {
		t.Fatal(err)
	}
	// 清单声明的大小与实际不符时拒绝。
	b.Manifest.Size--
	if b.ExtractPackage(filepath.Join(dir, "out2.deb")) == nil {
		t.Fatal("size mismatch accepted")
	}
	if _, err := OpenBundle(filepath.Join(dir, "out.deb")); err == nil {
		t.Fatal("non-zip accepted")
	}
}

func TestCertPins(t *testing.T) {
	if _, _, err := CertPins([]byte("not pem")); err == nil {
		t.Fatal("bad pem accepted")
	}
}
