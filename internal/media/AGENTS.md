# AGENTS.md — Media (`internal/media/`)

Reusable file-upload infrastructure for all domains. Domain code sets naming
policy (folders, public IDs); this package owns transport only.

## Rules

- `Client` from env (`CLOUDINARY_URL` preferred, else key triple); `NewCloudinary`
  errors when unconfigured so callers fall back (e.g. local disk).
- `Upload(data, publicID)` returns the secure URL; signed uploads authenticate
  via API secret — unsigned presets apply only without a secret (presets must
  be whitelisted for unsigned use).
- `Delete(url)` derives the public ID back (with CDN `Invalidate`); foreign or
  unparseable URLs are ignored (nil) so mixed local/remote rows are safe.
- Never log credentials; never commit them (see root secrets rule).

## Tests

`go test ./internal/media/ -count=1` — URL parsing (versioned, query strings,
traversal, foreign hosts). Live upload/delete verified manually against the
real account (see event README smoke flow).
