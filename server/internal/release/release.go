// Package release 发布包清单与签名（ECDSA P-256）。
//
// 私钥只在发布者本机（rk-release sign）；服务端与设备只持有公钥。
// 签名对象是规范化字符串（见 Message），设备端 daemon 以同样的格式校验。
package release

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// Package 唯一允许发布的包名。
const Package = "com.icloudza.residentkit"

// Manifest 发布清单，与安装包一起打进发布包（*.rkrel.zip，见 bundle.go）。
type Manifest struct {
	Package   string `json:"package"`
	Version   string `json:"version"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`    // 小写十六进制
	Signature string `json:"signature"` // base64，ASN.1 DER（X9.62）
}

var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Message 被签名的规范化内容。设备端（RKUpdateModule）必须逐字节一致。
func (m Manifest) Message() []byte {
	return []byte(fmt.Sprintf("residentkit-release\nv1\n%s\n%s\n%d\n%s\n", m.Package, m.Version, m.Size, m.SHA256))
}

// Validate 检查字段格式（不含签名）。
func (m Manifest) Validate() error {
	if m.Package != Package {
		return fmt.Errorf("包名必须是 %s", Package)
	}
	if !versionRe.MatchString(m.Version) {
		return errors.New("版本号格式应为 x.y.z")
	}
	if m.Size <= 0 || m.Size > 64<<20 {
		return errors.New("安装包大小异常（上限 64 MB）")
	}
	if len(m.SHA256) != 64 {
		return errors.New("sha256 格式错误")
	}
	if _, err := hex.DecodeString(m.SHA256); err != nil {
		return errors.New("sha256 格式错误")
	}
	return nil
}

// Hash 计算内容的大小与 SHA-256。
func Hash(r io.Reader) (int64, string, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	return n, hex.EncodeToString(h.Sum(nil)), err
}

// ---- 密钥 ----

// GenerateKey 生成私钥（PKCS#8 PEM）与公钥（base64，X9.63 未压缩点，iOS SecKey 可直接导入）。
func GenerateKey() (privPEM []byte, pubB64 string, err error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return nil, "", err
	}
	pub, err := PublicKeyString(&k.PublicKey)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), pub, err
}

func PublicKeyString(k *ecdsa.PublicKey) (string, error) {
	ek, err := k.ECDH()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ek.Bytes()), nil
}

func ParsePrivateKey(p []byte) (*ecdsa.PrivateKey, error) {
	b, _ := pem.Decode(p)
	if b == nil {
		return nil, errors.New("不是 PEM 格式的私钥")
	}
	k, err := x509.ParsePKCS8PrivateKey(b.Bytes)
	if err != nil {
		return nil, err
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok || ek.Curve != elliptic.P256() {
		return nil, errors.New("私钥不是 ECDSA P-256")
	}
	return ek, nil
}

// ParsePublicKey 解析 base64 的 X9.63 未压缩点（65 字节）。
func ParsePublicKey(s string) (*ecdsa.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != 65 || raw[0] != 4 {
		return nil, errors.New("公钥格式错误（应为 base64 的 65 字节 X9.63 点）")
	}
	x, y := new(big.Int).SetBytes(raw[1:33]), new(big.Int).SetBytes(raw[33:])
	k := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	if !k.Curve.IsOnCurve(x, y) {
		return nil, errors.New("公钥不在 P-256 曲线上")
	}
	return k, nil
}

// ---- 签名 ----

func Sign(k *ecdsa.PrivateKey, m *Manifest) error {
	sum := sha256.Sum256(m.Message())
	sig, err := ecdsa.SignASN1(rand.Reader, k, sum[:])
	if err != nil {
		return err
	}
	m.Signature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

func Verify(pub *ecdsa.PublicKey, m Manifest) error {
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		return errors.New("签名格式错误")
	}
	sum := sha256.Sum256(m.Message())
	if !ecdsa.VerifyASN1(pub, sum[:], sig) {
		return errors.New("签名校验失败：安装包不是用发布私钥签的，或清单被改动")
	}
	return nil
}

// CompareVersions 比较 x.y.z：a<b 返回 -1，相等 0，a>b 返回 1。
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
