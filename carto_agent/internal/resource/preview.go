package resource

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PreviewRenderer func(context.Context, *ResolvedResource, string) error

// Content can stay identical while its preview representation changes.
// Invalidate older map-like thumbnails without changing resource identity.
const previewRendererVersion = "qgis-symbol-legend/v2"

func previewStamp(digest string) string {
	return digest + "\n" + previewRendererVersion
}

func previewCurrent(root, digest string) bool {
	if regularFileExists(filepath.Join(root, ".preview-pending")) {
		return false
	}
	if rejectResourcePath(filepath.Join(root, "preview.png")) != nil || rejectResourcePath(filepath.Join(root, "preview.digest")) != nil {
		return false
	}
	stamp, err := os.ReadFile(filepath.Join(root, "preview.digest"))
	return err == nil && strings.TrimSpace(string(stamp)) == previewStamp(digest) && validatePNG(filepath.Join(root, "preview.png")) == nil
}
func (l *Library) previewRoot(r *ResolvedResource) string {
	root := r.Root
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		root = filepath.Dir(root)
	}
	if r.Owner == "library_builtin" {
		cache := l.previewCacheRoot(r)
		if regularFileExists(filepath.Join(cache, ".preview-pending")) || previewCurrent(cache, r.Digest) || !previewCurrent(root, r.Digest) {
			return cache
		}
	}
	return root
}

// QueuePreview persists a request made while QGIS is offline. The next
// connected worker will regenerate even if an older thumbnail still exists.
func (l *Library) QueuePreview(id string) (*ResolvedResource, error) {
	r, err := l.GetByID(id)
	if err != nil {
		return nil, err
	}
	root := l.previewRoot(r)
	if r.Owner == "library_builtin" {
		root = l.previewCacheRoot(r)
	}
	if err = rejectResourcePath(root); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if err = rejectResourcePath(filepath.Join(root, ".preview-pending")); err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(root, ".preview-pending"), []byte(r.Digest), 0600); err != nil {
		return nil, err
	}
	l.invalidate()
	r.PreviewStatus = "pending"
	return r, nil
}
func (l *Library) previewCacheRoot(r *ResolvedResource) string {
	return filepath.Join(l.UserDir, ".previews", r.ID, strings.TrimPrefix(r.Digest, "sha256:"))
}
func (l *Library) PreviewPath(id string) (string, bool, error) {
	r, err := l.GetByID(id)
	if err != nil {
		return "", false, err
	}
	if !IsPublicType(r.Type) {
		return "", false, fmt.Errorf("只能预览样式与版面")
	}
	root := l.previewRoot(r)
	if err = rejectResourcePath(root); err != nil {
		return "", false, err
	}
	return filepath.Join(root, "preview.png"), previewCurrent(root, r.Digest), nil
}
func (l *Library) GeneratePreview(ctx context.Context, id string, render PreviewRenderer, force bool) (*ResolvedResource, error) {
	r, err := l.GetByID(id)
	if err != nil {
		return nil, err
	}
	if HasErrors(r.Problems) {
		return nil, fmt.Errorf("资源校验未通过")
	}
	root := l.previewRoot(r)
	if !force && previewCurrent(root, r.Digest) {
		r.PreviewStatus = "ready"
		return r, nil
	}
	if render == nil {
		return r, nil
	}
	// Shipped assets are read-only even when the caller explicitly regenerates
	// an already valid packaged preview.
	if r.Owner == "library_builtin" {
		root = l.previewCacheRoot(r)
	}
	if err = rejectResourcePath(root); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(root, ".preview-*.png")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	if err = render(ctx, r, path); err != nil {
		return nil, err
	}
	if err = validatePNG(path); err != nil {
		return nil, err
	}
	// Do not publish a preview rendered from a file that changed while QGIS ran.
	current, err := l.GetByID(id)
	if err != nil {
		return nil, err
	}
	if current.Digest != r.Digest {
		return nil, fmt.Errorf("资源已变化，稍后重新生成预览")
	}
	if err = os.Rename(path, filepath.Join(root, "preview.png")); err != nil {
		return nil, err
	}
	stamp, err := os.CreateTemp(root, ".preview-stamp-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(stamp.Name())
	if _, err = stamp.WriteString(previewStamp(r.Digest) + "\n"); err != nil {
		stamp.Close()
		return nil, err
	}
	if err = stamp.Close(); err != nil {
		return nil, err
	}
	if err = os.Rename(stamp.Name(), filepath.Join(root, "preview.digest")); err != nil {
		return nil, err
	}
	if err = os.Remove(filepath.Join(root, ".preview-pending")); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	l.invalidate()
	r.PreviewStatus = "ready"
	return r, nil
}
