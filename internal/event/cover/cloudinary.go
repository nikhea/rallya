package cover

import (
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/media"
)

// Cloudinary stores event covers via the shared media client:
// rallya/events/<orgID>/<event>-<rand>. Callers needing other folders
// (avatars, galleries) should use internal/media directly.
type Cloudinary struct {
	m *media.Client
}

// NewCloudinary builds cover storage from the environment (see
// internal/media). Returns an error when unconfigured so main can
// fall back to Local.
func NewCloudinary() (*Cloudinary, error) {
	m, err := media.NewCloudinary()
	if err != nil {
		return nil, err
	}
	return &Cloudinary{m: m}, nil
}

// StoredImage is one persisted file with provider metadata.
type StoredImage struct {
	URL      string
	PublicID string
	Format   string
	Bytes    int
	Width    int
	Height   int
}

// Save uploads image bytes and returns the secure URL. ext is informational
// (service already validated MIME); Cloudinary detects the format itself.
func (c *Cloudinary) Save(orgID, eventID uuid.UUID, data io.Reader, ext string) (string, error) {
	stored, err := c.SaveImage(orgID, eventID, data, ext)
	if err != nil {
		return "", err
	}
	return stored.URL, nil
}

// SaveImage uploads and returns URL plus provider metadata.
func (c *Cloudinary) SaveImage(orgID, eventID uuid.UUID, data io.Reader, ext string) (*StoredImage, error) {
	raw, err := token.GenerateRawToken(8)
	if err != nil {
		return nil, err
	}
	publicID := fmt.Sprintf("rallya/events/%s/%s-%s", orgID.String(), eventID.String()[:8], raw[:12])
	res, err := c.m.Upload(data, publicID)
	if err != nil {
		return nil, err
	}
	format := res.Format
	if format == "" && len(ext) > 1 {
		format = ext[1:]
	}
	return &StoredImage{
		URL: res.SecureURL, PublicID: res.PublicID, Format: format,
		Bytes: res.Bytes, Width: res.Width, Height: res.Height,
	}, nil
}

// Delete destroys the asset behind a Cloudinary URL; anything else is ignored.
func (c *Cloudinary) Delete(url string) error {
	return c.m.Delete(url)
}
