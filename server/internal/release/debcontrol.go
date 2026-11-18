package release

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
)

// ReadControl 读出 .deb 里的 control 文件内容（软件源的 Packages 索引要用）。
// 支持 control.tar.gz 与未压缩的 control.tar；其他压缩格式返回错误，由调用方降级处理。
func ReadControl(debPath string) (string, error) {
	f, err := os.Open(debPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	magic := make([]byte, 8)
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != "!<arch>\n" {
		return "", errors.New("不是 deb 文件")
	}
	hdr := make([]byte, 60)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			return "", errors.New("deb 中没有 control.tar")
		}
		name := strings.TrimSuffix(strings.TrimSpace(string(hdr[0:16])), "/")
		size, err := strconv.ParseInt(strings.TrimSpace(string(hdr[48:58])), 10, 64)
		if err != nil || size < 0 || size > 16<<20 {
			return "", errors.New("deb 格式错误")
		}
		if strings.HasPrefix(name, "control.tar") {
			body := io.LimitReader(r, size)
			switch name {
			case "control.tar.gz":
				gz, err := gzip.NewReader(body)
				if err != nil {
					return "", err
				}
				defer gz.Close()
				return controlFromTar(gz)
			case "control.tar":
				return controlFromTar(body)
			default:
				return "", fmt.Errorf("不支持的 control 压缩格式 %s", name)
			}
		}
		if _, err := io.CopyN(io.Discard, r, size+size%2); err != nil { // 成员按偶数字节对齐
			return "", errors.New("deb 格式错误")
		}
	}
}

func controlFromTar(rd io.Reader) (string, error) {
	tr := tar.NewReader(rd)
	for {
		h, err := tr.Next()
		if err != nil {
			return "", errors.New("control.tar 中没有 control")
		}
		if path.Clean("/"+h.Name) == "/control" && h.Size < 64<<10 {
			var b bytes.Buffer
			if _, err := io.Copy(&b, tr); err != nil {
				return "", err
			}
			return strings.TrimRight(b.String(), "\n"), nil
		}
	}
}
