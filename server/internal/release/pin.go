package release

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
)

// CertPins 服务器证书的公钥指纹与证书指纹（与 daemon RKTLS 的算法一致）：
//
//	公钥指纹 = SHA-256(公钥：EC 为 X9.63 未压缩点，RSA 为 PKCS#1)
//	证书指纹 = SHA-256(证书 DER)
func CertPins(certPEM []byte) (keyPin, certPin string, err error) {
	b, _ := pem.Decode(certPEM)
	if b == nil {
		return "", "", errors.New("不是 PEM 格式的证书")
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return "", "", err
	}
	var raw []byte
	switch k := c.PublicKey.(type) {
	case *ecdsa.PublicKey:
		ek, err := k.ECDH()
		if err != nil {
			return "", "", err
		}
		raw = ek.Bytes()
	case *rsa.PublicKey:
		raw = x509.MarshalPKCS1PublicKey(k)
	default:
		return "", "", errors.New("不支持的证书公钥类型")
	}
	ks, cs := sha256.Sum256(raw), sha256.Sum256(c.Raw)
	return hex.EncodeToString(ks[:]), hex.EncodeToString(cs[:]), nil
}
