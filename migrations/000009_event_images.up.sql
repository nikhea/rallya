-- Event gallery images: every uploaded file with its Cloudinary metadata.
-- Rows cascade with the event; asset cleanup happens in service code.
CREATE TABLE IF NOT EXISTS event_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,

    url TEXT NOT NULL,
    public_id TEXT NOT NULL,

    format VARCHAR(20),
    bytes INTEGER NOT NULL DEFAULT 0,
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_event_images_event_id ON event_images (event_id);
