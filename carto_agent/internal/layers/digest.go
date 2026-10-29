package layers

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func DataFiles(path string) ([]string, error) {
	info, e := os.Stat(path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("不是普通文件：%s", path)
	}
	files := []string{path}
	if strings.EqualFold(filepath.Ext(path), ".shp") {
		stem := strings.TrimSuffix(path, filepath.Ext(path))
		for _, ext := range []string{".shx", ".dbf", ".prj", ".cpg"} {
			p := stem + ext
			if _, e := os.Stat(p); e == nil {
				files = append(files, p)
			}
		}
	}
	sort.Strings(files)
	return files, nil
}
func Digest(path string) (string, error) {
	files, e := DataFiles(path)
	if e != nil {
		return "", e
	}
	h := sha256.New()
	for _, p := range files {
		fmt.Fprintf(h, "%s\x00", filepath.Base(p))
		f, e := os.Open(p)
		if e != nil {
			return "", e
		}
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return "", e
		}
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}
