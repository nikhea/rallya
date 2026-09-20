package config

import (
	"os"
	"strings"
)

// CloudinaryConfig carries image-upload credentials. All values come from
// the environment — never hardcode or log them.
type CloudinaryConfig struct {
	// URL is the full cloudinary://key:secret@cloud URL (preferred).
	URL string
	// CloudName, APIKey, APISecret are the split-out alternative.
	CloudName string
	APIKey    string
	APISecret string
	// UploadPreset is the unsigned upload preset (e.g. for browser-direct
	// or server-side unsigned uploads).
	UploadPreset string
}

// CloudinaryConfigFromEnv resolves Cloudinary settings.
func CloudinaryConfigFromEnv() CloudinaryConfig {
	return CloudinaryConfig{
		URL:          strings.TrimSpace(os.Getenv("CLOUDINARY_URL")),
		CloudName:    strings.TrimSpace(os.Getenv("CLOUD_NAME")),
		APIKey:       strings.TrimSpace(os.Getenv("CLOUD_API_KEY")),
		APISecret:    os.Getenv("CLOUD_API_SECRET"),
		UploadPreset: strings.TrimSpace(os.Getenv("CLOUDINARY_UPLOAD_PRESET")),
	}
}

// CloudinaryEnabled reports whether uploads can go to Cloudinary:
// full URL, or cloud name plus key/secret pair.
func CloudinaryEnabled() bool {
	cfg := CloudinaryConfigFromEnv()
	if cfg.URL != "" {
		return true
	}
	return cfg.CloudName != "" && cfg.APIKey != "" && cfg.APISecret != ""
}
