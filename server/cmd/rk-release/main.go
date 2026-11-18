// Command rk-release 在发布者本机生成发布密钥、为安装包签名。私钥不离开本机。
//
//	rk-release keygen                  生成私钥（~/.config/residentkit/release-key.pem）并打印公钥
//	rk-release pubkey                  打印已有私钥对应的公钥
//	rk-release pin <cert.pem>          打印服务器证书的公钥指纹与证书指纹（写入 server.env 的 RK_SERVER_PIN）
//	rk-release sign <deb> <version>    生成发布包 <deb 去掉 .deb>.rkrel.zip（安装包 + 签名清单），控制台上传它即可
package main

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/icloudza/ResidentKit/server/internal/release"
)

func keyPath() string {
	if p := os.Getenv("RK_RELEASE_KEY"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "residentkit", "release-key.pem")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("用法：rk-release keygen | pubkey | pin <cert.pem> | sign <deb> <version>")
	}
	switch args[0] {
	case "keygen":
		p := keyPath()
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("%s 已存在，为防止覆盖拒绝执行", p)
		}
		priv, pub, err := release.GenerateKey()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(p, priv, 0o600); err != nil {
			return err
		}
		fmt.Printf("私钥已保存：%s（0600，切勿上传或提交）\n公钥：%s\n", p, pub)
		return nil
	case "pubkey":
		k, err := loadKey()
		if err != nil {
			return err
		}
		pub, err := release.PublicKeyString(&k.PublicKey)
		fmt.Println(pub)
		return err
	case "pin":
		if len(args) != 2 {
			return errors.New("用法：rk-release pin <cert.pem>")
		}
		b, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		keyPin, certPin, err := release.CertPins(b)
		if err != nil {
			return err
		}
		fmt.Printf("公钥指纹：%s\n证书指纹：%s\n\n建议写入 server.env：RK_SERVER_PIN=%s\n"+
			"（公钥指纹在证书续期时不变，只要续期时沿用同一把私钥）\n", keyPin, certPin, keyPin)
		return nil
	case "sign":
		if len(args) != 3 {
			return errors.New("用法：rk-release sign <deb> <version>")
		}
		k, err := loadKey()
		if err != nil {
			return err
		}
		f, err := os.Open(args[1])
		if err != nil {
			return err
		}
		size, sum, err := release.Hash(f)
		f.Close()
		if err != nil {
			return err
		}
		m := release.Manifest{Package: release.Package, Version: args[2], Size: size, SHA256: sum}
		if err := m.Validate(); err != nil {
			return err
		}
		if err := release.Sign(k, &m); err != nil {
			return err
		}
		if err := release.Verify(&k.PublicKey, m); err != nil {
			return err
		}
		out := strings.TrimSuffix(args[1], ".deb") + ".rkrel.zip"
		if err := writeBundle(out, m, args[1]); err != nil {
			return err
		}
		fmt.Printf("已签名：%s（版本 %s，%d 字节）\n在控制台「发布」页上传这个文件。\n", out, m.Version, m.Size)
		return nil
	}
	return fmt.Errorf("未知命令 %s", args[0])
}

// writeBundle 先写临时文件再改名，中途失败不留下半个发布包。
func writeBundle(out string, m release.Manifest, debPath string) error {
	deb, err := os.Open(debPath)
	if err != nil {
		return err
	}
	defer deb.Close()
	tmp := out + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	err = release.WriteBundle(f, m, deb)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, out)
}

func loadKey() (*ecdsa.PrivateKey, error) {
	b, err := os.ReadFile(keyPath())
	if err != nil {
		return nil, fmt.Errorf("读取私钥失败（先运行 rk-release keygen）：%w", err)
	}
	return release.ParsePrivateKey(b)
}
