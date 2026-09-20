package cover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSaveDeleteRoundtrip(t *testing.T) {
	base := t.TempDir()
	l := NewLocal(base)
	org, ev := uuid.New(), uuid.New()

	url, err := l.Save(org, ev, strings.NewReader("imagedata"), ".png")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.HasPrefix(url, "/uploads/events/") || !strings.HasSuffix(url, ".png") {
		t.Fatalf("bad url %q", url)
	}
	rel, _ := strings.CutPrefix(url, "/uploads/")
	if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("file missing: %v", err)
	}
	if err := l.Delete(url); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatal("file should be gone")
	}
	// Delete idempotent + traversal-safe.
	if err := l.Delete(url); err != nil {
		t.Fatalf("re-delete: %v", err)
	}
	if err := l.Delete("/uploads/../../etc/passwd"); err != nil {
		t.Fatalf("traversal must be ignored, got %v", err)
	}
	if err := l.Delete("https://cdn.example.com/x.png"); err != nil {
		t.Fatalf("external must be ignored, got %v", err)
	}
}
