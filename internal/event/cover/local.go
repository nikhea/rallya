// Package cover persists event cover images on local disk.
// Layout: <base>/events/<orgID>/<rand>.<ext>, served read-only at /uploads.
// URLs stay stable so a future S3 swap only changes this package.
package cover

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nikhea/rallya/internal/auth/token"
)

// Local stores covers under base dir (e.g. ./uploads).
type Local struct {
	base string
}

// NewLocal builds local storage rooted at base.
func NewLocal(base string) *Local { return &Local{base: base} }

// Save writes data and returns the public URL path.
func (l *Local) Save(orgID, eventID uuid.UUID, data io.Reader, ext string) (string, error) {
	stored, err := l.SaveImage(orgID, eventID, data, ext)
	if err != nil {
		return "", err
	}
	return stored.URL, nil
}

// SaveImage writes data, counting bytes (dimensions unknown locally).
func (l *Local) SaveImage(orgID, eventID uuid.UUID, data io.Reader, ext string) (*StoredImage, error) {
	raw, err := token.GenerateRawToken(8)
	if err != nil {
		return nil, err
	}
	rel := filepath.Join("events", orgID.String(), fmt.Sprintf("%s-%s%s", eventID.String()[:8], raw[:12], ext))
	abs := filepath.Join(l.base, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	n, err := io.Copy(f, data)
	if err != nil {
		_ = os.Remove(abs)
		return nil, err
	}
	format := ""
	if len(ext) > 1 {
		format = ext[1:]
	}
	url := "/uploads/" + filepath.ToSlash(rel)
	return &StoredImage{
		URL: url, PublicID: filepath.ToSlash(rel),
		Format: format, Bytes: int(n),
	}, nil
}

// Delete removes a previously stored URL. Unknown/external URLs are ignored.
func (l *Local) Delete(url string) error {
	rel, ok := strings.CutPrefix(url, "/uploads/")
	if !ok || strings.Contains(rel, "..") {
		return nil
	}
	if err := os.Remove(filepath.Join(l.base, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
