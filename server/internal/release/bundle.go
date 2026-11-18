package release

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// 发布包（*.rkrel.zip）：rk-release sign 生成，控制台上传这一个文件即可。
//
//	manifest.json  清单与签名（Manifest）
//	package.deb    安装包
const (
	bundleManifest = "manifest.json"
	bundlePackage  = "package.deb"
)

// WriteBundle 把清单与安装包写成发布包。
func WriteBundle(w io.Writer, m Manifest, deb io.Reader) error {
	z := zip.NewWriter(w)
	mw, err := z.Create(bundleManifest)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if _, err := mw.Write(append(b, '\n')); err != nil {
		return err
	}
	// deb 本身已压缩，存储即可。
	dw, err := z.CreateHeader(&zip.FileHeader{Name: bundlePackage, Method: zip.Store})
	if err != nil {
		return err
	}
	if _, err := io.Copy(dw, deb); err != nil {
		return err
	}
	return z.Close()
}

// Bundle 已打开的发布包。
type Bundle struct {
	Manifest Manifest
	z        *zip.ReadCloser
	deb      *zip.File
}

// OpenBundle 打开发布包并读出清单（只做格式检查，签名由调用方校验）。
func OpenBundle(path string) (*Bundle, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, errors.New("不是有效的发布包（应为 rk-release sign 生成的 .rkrel.zip）")
	}
	b := &Bundle{z: z}
	var mf *zip.File
	for _, f := range z.File {
		switch strings.TrimPrefix(f.Name, "./") {
		case bundleManifest:
			mf = f
		case bundlePackage:
			b.deb = f
		}
	}
	if mf == nil || b.deb == nil {
		z.Close()
		return nil, errors.New("发布包缺少 manifest.json 或 package.deb")
	}
	r, err := mf.Open()
	if err != nil {
		z.Close()
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(r, 4097))
	r.Close()
	if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &b.Manifest) != nil {
		z.Close()
		return nil, errors.New("发布包中的清单无效")
	}
	b.Manifest.SHA256 = strings.ToLower(b.Manifest.SHA256)
	return b, nil
}

// ExtractPackage 把安装包解出到 dst，并确认大小与 SHA-256 与清单一致。
// 最多读取清单声明的大小 + 1 字节，防止压缩炸弹。
func (b *Bundle) ExtractPackage(dst string) error {
	r, err := b.deb.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, b.Manifest.Size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("解出安装包失败：%w", err)
	}
	if n != b.Manifest.Size || hex.EncodeToString(h.Sum(nil)) != b.Manifest.SHA256 {
		return errors.New("安装包与清单不符（大小或 SHA-256 不一致）")
	}
	return nil
}

func (b *Bundle) Close() error { return b.z.Close() }
