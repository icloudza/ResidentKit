package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/icloudza/ResidentKit/server/internal/dto"
	"github.com/icloudza/ResidentKit/server/internal/release"
)

func signedEnroll(t *testing.T, k *ecdsa.PrivateKey, ts int64) dto.EnrollRequest {
	pub, err := release.PublicKeyString(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	req := dto.EnrollRequest{Device: "F10A3F6A-C8BD-46F9-92AF-27F702894CE8", Name: "iPhone", Model: "iPhone15,2",
		Version: "1.6.4", PublicKey: pub, TS: ts}
	sum := sha256.Sum256(EnrollMessage(req.Device, req.PublicKey, req.TS))
	sig, _ := ecdsa.SignASN1(rand.Reader, k, sum[:])
	req.Signature = base64.StdEncoding.EncodeToString(sig)
	return req
}

func TestEnrollVerify(t *testing.T) {
	s := &EnrollService{}
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := time.Now().Unix()

	if err := s.verify(signedEnroll(t, k, now)); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	// 用别的私钥签名、声称是这把公钥：拒绝。
	forged := signedEnroll(t, other, now)
	forged.PublicKey = signedEnroll(t, k, now).PublicKey
	if !errors.Is(s.verify(forged), ErrBadEnroll) {
		t.Fatal("forged signature accepted")
	}
	// 篡改字段：签名不再匹配。
	tampered := signedEnroll(t, k, now)
	tampered.Device = "AAAAAAAA-0000-0000-0000-000000000000"
	if !errors.Is(s.verify(tampered), ErrBadEnroll) {
		t.Fatal("tampered request accepted")
	}
	// 过期的时间戳（重放）。
	if s.verify(signedEnroll(t, k, now-3600)) == nil {
		t.Fatal("stale timestamp accepted")
	}
	if c := pairingCode("x"); len(c) != 6 || pairingCode("x") != c {
		t.Fatal("pairing code unstable")
	}
}
