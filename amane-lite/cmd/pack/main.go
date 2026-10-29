// pack 构建 Mica Home 模块包：交叉编译 sidecar、回写 manifest 哈希、打 zip。
//
// 用 Go 而不是 PowerShell 处理 JSON：宿主按 UTF-8 读取 manifest，Windows 的 Set-Content 会写 BOM。
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	root := flag.String("root", ".", "仓库根目录")
	flag.Parse()
	if err := run(*root); err != nil {
		fmt.Fprintln(os.Stderr, "打包失败:", err)
		os.Exit(1)
	}
}

func run(root string) error {
	manifestPath := filepath.Join(root, "module", "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("manifest 解析失败: %w", err)
	}
	version, _ := manifest["version"].(string)
	if version == "" {
		return fmt.Errorf("manifest 缺少 version")
	}

	// 前端与内嵌版本保持同一份源码。
	for _, name := range []string{"app.js", "style.css"} {
		if err := copyFile(filepath.Join(root, "internal", "api", "web", name),
			filepath.Join(root, "module", "frontend", name)); err != nil {
			return err
		}
	}

	bin := filepath.Join(root, "module", "sidecar", "amaned")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", bin, "./cmd/amaned")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("编译 sidecar 失败: %w", err)
	}

	sum, err := fileSHA256(bin)
	if err != nil {
		return err
	}
	sidecar, ok := manifest["sidecar"].(map[string]any)
	if !ok {
		return fmt.Errorf("manifest 缺少 sidecar")
	}
	sidecar["sha256"] = map[string]string{"sidecar/amaned": sum}

	updated, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(manifestPath, updated, 0o644); err != nil {
		return err
	}

	zipPath := filepath.Join(root, "dist", fmt.Sprintf("amane-%s.zip", version))
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		return err
	}
	return writeZip(zipPath, []string{
		filepath.Join(root, "module", "manifest.json"),
		filepath.Join(root, "module", "frontend"),
		filepath.Join(root, "module", "sidecar"),
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeZip(dest string, sources []string) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()
	for _, source := range sources {
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if err := addFile(zw, source, strings.TrimPrefix(filepath.ToSlash(source), filepath.ToSlash(mustRoot(source))+"/")); err != nil {
				return err
			}
			continue
		}
		err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			rel := strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(filepath.Dir(source))+"/")
			return addFile(zw, path, rel)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// mustRoot 让 manifest 这类单文件在 zip 根目录下只保留文件名。
func mustRoot(path string) string { return filepath.Dir(path) }

func addFile(zw *zip.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(name)
	header.Method = zip.Deflate
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, f)
	return err
}
