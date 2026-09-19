// Package media is reusable file-upload infrastructure for all domains.
// Today it wraps Cloudinary (images); tomorrow S3 or other backends slot
// in behind the same Client shape. Domain code (covers, avatars, galleries)
// builds its own thin naming/storage policy on top — see internal/event/cover.
package media

import (
	"context"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"

	"github.com/nikhea/rallya/cmd/config"
)

// Client uploads to and destroys assets in Cloudinary.
type Client struct {
	cld    *cloudinary.Cloudinary
	cloud  string
	preset string
	signed bool
}

var versionPrefix = regexp.MustCompile(`^v\d+/(.+)$`)

// NewCloudinary builds a Client from the environment. Prefers CLOUDINARY_URL,
// else CLOUD_NAME/KEY/SECRET. Returns an error when unconfigured so callers
// can fall back (e.g. local disk).
func NewCloudinary() (*Client, error) {
	cfg := config.CloudinaryConfigFromEnv()
	var (
		cld *cloudinary.Cloudinary
		err error
	)
	if cfg.URL != "" {
		cld, err = cloudinary.NewFromURL(cfg.URL)
	} else if cfg.CloudName != "" && cfg.APIKey != "" && cfg.APISecret != "" {
		cld, err = cloudinary.NewFromParams(cfg.CloudName, cfg.APIKey, cfg.APISecret)
	} else {
		return nil, fmt.Errorf("cloudinary unconfigured (set CLOUDINARY_URL or CLOUD_NAME/KEY/SECRET)")
	}
	if err != nil {
		return nil, err
	}
	cloud := cfg.CloudName
	signed := cfg.APISecret != ""
	if cloud == "" {
		cloud, _ = CloudNameFromURL(cfg.URL)
	}
	return &Client{cld: cld, cloud: cloud, preset: cfg.UploadPreset, signed: signed}, nil
}

// UploadResult carries the delivery URL plus provider metadata.
type UploadResult struct {
	SecureURL string
	PublicID  string
	Format    string
	Bytes     int
	Width     int
	Height    int
}

// Upload stores data under publicID (e.g. "rallya/events/<org>/<file>")
// and returns delivery URL plus metadata. Callers choose naming; Cloudinary
// detects the format itself.
func (c *Client) Upload(data io.Reader, publicID string) (*UploadResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	params := uploader.UploadParams{PublicID: publicID}
	if c.preset != "" && !c.signed {
		// Unsigned uploads require a whitelisted preset; signed uploads
		// authenticate via API secret instead.
		params.UploadPreset = c.preset
	}
	resp, err := c.cld.Upload.Upload(ctx, data, params)
	if err != nil {
		return nil, fmt.Errorf("cloudinary upload: %w", err)
	}
	if resp.SecureURL == "" {
		return nil, fmt.Errorf("cloudinary upload: empty secure url")
	}
	return &UploadResult{
		SecureURL: resp.SecureURL, PublicID: resp.PublicID,
		Format: resp.Format, Bytes: resp.Bytes,
		Width: resp.Width, Height: resp.Height,
	}, nil
}

// Delete destroys the asset behind a Cloudinary delivery URL (with CDN
// invalidation). Foreign URLs and unparseable inputs are ignored (nil) so
// mixed local/remote rows are safe.
func (c *Client) Delete(url string) error {
	id, ok := PublicIDFromURL(url, c.cloud)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	invalidate := true
	if _, err := c.cld.Upload.Destroy(ctx, uploader.DestroyParams{PublicID: id, Invalidate: &invalidate}); err != nil {
		return fmt.Errorf("cloudinary destroy %s: %w", id, err)
	}
	return nil
}

// Cloud returns the configured cloud name (for logging/diagnostics).
func (c *Client) Cloud() string { return c.cloud }

// PublicIDFromURL extracts "folder/name" from a Cloudinary delivery URL:
// https://res.cloudinary.com/<cloud>/image/upload/[v123/]folder/name.jpg
func PublicIDFromURL(url, cloud string) (string, bool) {
	if url == "" || cloud == "" {
		return "", false
	}
	marker := "res.cloudinary.com/" + cloud + "/image/upload/"
	i := strings.Index(url, marker)
	if i < 0 {
		return "", false
	}
	rest := url[i+len(marker):]
	if m := versionPrefix.FindStringSubmatch(rest); m != nil {
		rest = m[1]
	}
	// Strip format extension and query string.
	if q := strings.Index(rest, "?"); q >= 0 {
		rest = rest[:q]
	}
	if dot := strings.LastIndex(rest, "."); dot >= 0 {
		rest = rest[:dot]
	}
	rest = strings.Trim(rest, "/")
	if rest == "" || strings.Contains(rest, "..") {
		return "", false
	}
	return path.Clean(rest), true
}

// CloudNameFromURL parses the cloud name out of a cloudinary:// URL.
func CloudNameFromURL(raw string) (string, bool) {
	// cloudinary://key:secret@cloud[/...]
	at := strings.LastIndex(raw, "@")
	if at < 0 {
		return "", false
	}
	rest := raw[at+1:]
	if i := strings.Index(rest, "/"); i >= 0 {
		rest = rest[:i]
	}
	if rest == "" {
		return "", false
	}
	return rest, true
}
