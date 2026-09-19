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
	raw, err := token.GenerateRawToken(8)
	if err != nil {
		return "", err
	}
	rel := filepath.Join("events", orgID.String(), fmt.Sprintf("%s-%s%s", eventID.String()[:8], raw[:12], ext))
	abs := filepath.Join(l.base, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, data); err != nil {
		_ = os.Remove(abs)
		return "", err
	}
	return "/uploads/" + filepath.ToSlash(rel), nil
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
