package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nikhea/rallya/cmd/config"
	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditsvc "github.com/nikhea/rallya/internal/audit/service"
	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/event/cover"
	eventdto "github.com/nikhea/rallya/internal/event/dto"
	"github.com/nikhea/rallya/internal/event/model"
	"github.com/nikhea/rallya/internal/event/repository"
	"github.com/nikhea/rallya/internal/event/utils"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
	submodel "github.com/nikhea/rallya/internal/subscription/model"
	subservice "github.com/nikhea/rallya/internal/subscription/service"
)

// Cover size/type policy (Q11) and gallery limits.
const (
	maxCoverBytes   = 5 << 20 // 5MB
	maxGalleryFiles = 10
)

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

var allowedCoverTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// OrgResolver is implemented by the organization domain (adapter).
// Events never touch org tables directly.
type OrgResolver interface {
	ResolveOrgID(ref string) (uuid.UUID, error)
	OrgName(orgID uuid.UUID) (name string, err error)
	IsMember(userID, orgID uuid.UUID) bool
	NotifyTargets(orgID uuid.UUID) ([]orgdto.MemberNotify, error)
}

// CoverStorage persists cover/gallery images (local disk or Cloudinary).
type CoverStorage interface {
	Save(orgID, eventID uuid.UUID, data io.Reader, ext string) (url string, err error)
	SaveImage(orgID, eventID uuid.UUID, data io.Reader, ext string) (*cover.StoredImage, error)
	Delete(url string) error
}

// DetailCache is a cache-aside store for public event payloads.
// Declared by this consumer; implemented by internal/cache (wired in
// cmd/api). Nil-safe when unwired: reads fall through to Postgres and
// purges are no-ops, so tests and Redis-less boots behave identically.
type DetailCache interface {
	Get(ctx context.Context, key string, dst any) (bool, error)
	Set(ctx context.Context, key string, val any, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

// publicEventTTL bounds worst-case staleness after a write whose purge
// raced or failed (purges run post-commit, log on error, fail open).
const publicEventTTL = time.Minute

// publicEventKey namespaces cached public payloads by event ID. Only
// published payloads are stored (drafts 404 outsiders, so a shared key
// can never leak them).
func publicEventKey(id uuid.UUID) string {
	return "evt:pub:" + id.String()
}

// EventService orchestrates event flows.
type EventService struct {
	repo     *repository.EventRepository
	orgs     OrgResolver
	enqueuer jobs.Enqueuer
	storage  CoverStorage
	auditor  auditsvc.Emitter
	dcache   DetailCache
	plans    EntitlementProvider
}

// EntitlementProvider resolves subscription entitlements (implemented by
// the subscription domain; nil resolves Free — fail-closed restrictive).
type EntitlementProvider interface {
	EntitlementFor(orgID uuid.UUID) (submodel.Entitlement, error)
}

// SetEntitlementProvider wires plan quota enforcement.
func (s *EventService) SetEntitlementProvider(p EntitlementProvider) { s.plans = p }

// entitlement resolves the org's tier (Free when unwired or on error).
func (s *EventService) entitlement(orgID uuid.UUID) submodel.Entitlement {
	if s.plans == nil {
		return submodel.FreeEntitlement()
	}
	ent, err := s.plans.EntitlementFor(orgID)
	if err != nil {
		return submodel.FreeEntitlement()
	}
	return ent
}

// NewEventService builds the service. orgs is required; enqueuer/storage
// are wired via setters and nil-safe when absent (tests).
func NewEventService(repo *repository.EventRepository, orgs OrgResolver) *EventService {
	return &EventService{repo: repo, orgs: orgs}
}

// SetEnqueuer wires River job insertion.
func (s *EventService) SetEnqueuer(e jobs.Enqueuer) { s.enqueuer = e }

// SetCoverStorage wires cover persistence.
func (s *EventService) SetCoverStorage(c CoverStorage) { s.storage = c }

// SetAuditEmitter wires audit-trail emission (nil-safe when absent).
func (s *EventService) SetAuditEmitter(a auditsvc.Emitter) { s.auditor = a }

// SetDetailCache wires the public-detail cache (nil-safe when absent).
func (s *EventService) SetDetailCache(c DetailCache) { s.dcache = c }

// emit records one audit entry (nil-safe when unwired). Call inside the
// action's tx so entry and mutation commit atomically.
func (s *EventService) emit(tx *gorm.DB, e auditsvc.Entry) error {
	if s.auditor == nil {
		return nil
	}
	return s.auditor.EmitTx(tx, e)
}

// ---------- create / read ----------

// CreateInput carries validated create fields.
type CreateInput struct {
	Title       string
	Slug        string
	Description *string
	Venue       *string
	Location    *string
	StartsAt    *time.Time
	EndsAt      *time.Time
	Capacity    *int
}

// CreateEvent creates a DRAFT event under an org (ADMIN+ enforced upstream).
// Empty slugs auto-uniquify per org (name, name-2, ...).
func (s *EventService) CreateEvent(creatorID uuid.UUID, orgRef string, in CreateInput) (*eventdto.Event, error) {
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return nil, ErrOrgUnresolved
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, ErrInvalidTitle
	}
	if n, err := s.repo.CountActiveByOrg(orgID); err != nil {
		return nil, err
	} else if n >= int64(s.entitlement(orgID).Limits.MaxEvents) {
		return nil, subservice.ErrUpgradeRequired
	}
	if err := checkDates(in.StartsAt, in.EndsAt); err != nil {
		return nil, err
	}
	if in.Capacity != nil && *in.Capacity <= 0 {
		return nil, ErrInvalidCap
	}
	auto := strings.TrimSpace(in.Slug) == ""
	var base string
	if auto {
		base = utils.Slugify(title)
		if base == "" {
			base = "event"
		}
	} else {
		base = strings.ToLower(strings.TrimSpace(in.Slug))
		if !slugRe.MatchString(base) {
			return nil, ErrInvalidSlug
		}
	}
	e := &model.Event{
		OrganizationID: orgID, Title: title, Status: model.EventStatusDraft,
		Description: in.Description, Venue: in.Venue, Location: in.Location,
		StartsAt: utcPtr(in.StartsAt), EndsAt: utcPtr(in.EndsAt), Capacity: in.Capacity,
		CreatedBy: &creatorID,
	}
	for attempt := 0; attempt < 4; attempt++ {
		slug := base
		if auto {
			var err error
			if slug, err = s.uniqueSlug(orgID, base); err != nil {
				return nil, err
			}
		}
		e.Slug = slug
		err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
			if err := s.repo.CreateEvent(tx, e); err != nil {
				return err
			}
			return s.emit(tx, auditsvc.Entry{
				OrgID: &orgID, ActorID: &creatorID,
				Action: "event.created", ObjectType: auditmodel.ObjectEvent, ObjectID: &e.ID,
				After: map[string]any{"title": title, "slug": slug},
			})
		})
		if err == nil {
			return toEvent(e), nil
		}
		if !isUniqueViolation(err) || !auto {
			if isUniqueViolation(err) {
				return nil, ErrSlugTaken
			}
			return nil, err
		}
		// Lost a slug race: re-probe next attempt.
	}
	return nil, ErrSlugTaken
}

// GetEvent returns an event: published is public; drafts require membership
// (non-members get not-found: stealth). callerID nil = anonymous.
func (s *EventService) GetEvent(callerID *uuid.UUID, orgRef, eventRef string) (*eventdto.Event, error) {
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return nil, ErrOrgUnresolved
	}
	e, err := s.resolveEvent(orgID, eventRef)
	if err != nil {
		return nil, err
	}
	if e.Published() {
		return toEvent(e), nil
	}
	if callerID == nil || !s.orgs.IsMember(*callerID, orgID) {
		return nil, ErrEventNotFound
	}
	return toEvent(e), nil
}

// GetPublicEvent returns a published event by ID (no org context).
// Cache-aside on the published payload: hits skip Postgres, misses
// populate. Drafts 404 and are never stored. Cache failures fail open
// (warn + fall through) — a cache outage must not break reads.
func (s *EventService) GetPublicEvent(id uuid.UUID) (*eventdto.Event, error) {
	if s.dcache != nil {
		var cached eventdto.Event
		hit, err := s.dcache.Get(context.Background(), publicEventKey(id), &cached)
		if err != nil {
			slog.Warn("event detail cache read failed, falling through", "eventID", id, "error", err)
		} else if hit {
			return &cached, nil
		}
	}
	e, err := s.repo.GetEventByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEventNotFound
		}
		return nil, err
	}
	if !e.Published() {
		return nil, ErrEventNotFound
	}
	out := toEvent(e)
	if s.dcache != nil {
		if err := s.dcache.Set(context.Background(), publicEventKey(id), out, publicEventTTL); err != nil {
			slog.Warn("event detail cache write failed", "eventID", id, "error", err)
		}
	}
	return out, nil
}

// ListPublic lists published events (filters + sort + pagination).
func (s *EventService) ListPublic(f eventdto.EventFilter, limit, offset int, sort string) ([]eventdto.Event, int64, error) {
	rf, err := s.parseFilter(f, nil, false)
	if err != nil {
		return nil, 0, err
	}
	return s.list(rf, limit, offset, sort)
}

// ListOrgEvents lists an org's events including drafts (member-gated upstream).
func (s *EventService) ListOrgEvents(orgRef string, f eventdto.EventFilter, limit, offset int, sort string) ([]eventdto.Event, int64, error) {
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return nil, 0, ErrOrgUnresolved
	}
	rf, err := s.parseFilter(f, &orgID, true)
	if err != nil {
		return nil, 0, err
	}
	return s.list(rf, limit, offset, sort)
}

// parseFilter converts wire filter strings into typed repository filters.
func (s *EventService) parseFilter(f eventdto.EventFilter, orgID *uuid.UUID, drafts bool) (repository.EventFilter, error) {
	rf := repository.EventFilter{IncludeDrafts: drafts, Query: f.Query}
	if orgID != nil {
		rf.OrganizationID = orgID
	} else if f.OrganizationID != nil {
		id, err := s.orgs.ResolveOrgID(*f.OrganizationID)
		if err != nil {
			return rf, ErrOrgUnresolved
		}
		rf.OrganizationID = &id
	}
	if f.Status != nil {
		st := model.EventStatus(strings.ToUpper(strings.TrimSpace(*f.Status)))
		if !st.Valid() {
			return rf, ErrEventNotFound
		}
		rf.Status = &st
	}
	if f.From != nil {
		t, err := time.Parse(time.RFC3339, *f.From)
		if err != nil {
			return rf, ErrEventNotFound
		}
		rf.From = &t
	}
	if f.To != nil {
		t, err := time.Parse(time.RFC3339, *f.To)
		if err != nil {
			return rf, ErrEventNotFound
		}
		rf.To = &t
	}
	return rf, nil
}

func (s *EventService) list(f repository.EventFilter, limit, offset int, sort string) ([]eventdto.Event, int64, error) {
	es, total, err := s.repo.ListEvents(f, limit, offset, sort)
	if err != nil {
		return nil, 0, err
	}
	out := make([]eventdto.Event, 0, len(es))
	for _, e := range es {
		out = append(out, *toEvent(&e))
	}
	return out, total, nil
}

// ---------- update / lifecycle ----------

// UpdateInput carries optional update fields (nil = unchanged).
type UpdateInput struct {
	Title       *string
	Slug        *string
	Description *string
	Venue       *string
	Location    *string
	StartsAt    *time.Time
	EndsAt      *time.Time
	Capacity    *int
	ClearCover  bool
}

// UpdateEvent edits a draft or published event (ADMIN+ upstream).
// Status changes go through publish/unpublish/cancel, not here.
func (s *EventService) UpdateEvent(actorID uuid.UUID, orgRef, eventRef string, in UpdateInput) (*eventdto.Event, error) {
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return nil, ErrOrgUnresolved
	}
	e, err := s.resolveEvent(orgID, eventRef)
	if err != nil {
		return nil, err
	}
	before := eventSnapshot(e)
	if in.Title != nil {
		if strings.TrimSpace(*in.Title) == "" {
			return nil, ErrInvalidTitle
		}
		e.Title = strings.TrimSpace(*in.Title)
	}
	if in.Slug != nil {
		slug := strings.ToLower(strings.TrimSpace(*in.Slug))
		if !slugRe.MatchString(slug) {
			return nil, ErrInvalidSlug
		}
		e.Slug = slug
	}
	if in.Description != nil {
		e.Description = in.Description
	}
	if in.Venue != nil {
		e.Venue = in.Venue
	}
	if in.Location != nil {
		e.Location = in.Location
	}
	starts, ends := e.StartsAt, e.EndsAt
	if in.StartsAt != nil {
		starts = utcPtr(in.StartsAt)
	}
	if in.EndsAt != nil {
		ends = utcPtr(in.EndsAt)
	}
	if err := checkDates(starts, ends); err != nil {
		return nil, err
	}
	e.StartsAt, e.EndsAt = starts, ends
	if in.Capacity != nil {
		if *in.Capacity <= 0 {
			return nil, ErrInvalidCap
		}
		e.Capacity = in.Capacity
	}
	if in.ClearCover {
		old := ""
		if e.CoverURL != nil {
			old = *e.CoverURL
		}
		e.CoverURL = nil
		if err := s.saveWithAudit(actorID, orgID, e, before); err != nil {
			return nil, err
		}
		if old != "" {
			s.deleteCover(old)
		}
		s.purgePublicEvent(e.ID)
		return toEvent(e), nil
	}
	if err := s.saveWithAudit(actorID, orgID, e, before); err != nil {
		return nil, err
	}
	s.purgePublicEvent(e.ID)
	return toEvent(e), nil
}

// eventSnapshot captures the audited fields of an event row.
func eventSnapshot(e *model.Event) map[string]any {
	snap := map[string]any{"title": e.Title, "slug": e.Slug, "status": string(e.Status)}
	if e.Venue != nil {
		snap["venue"] = *e.Venue
	}
	if e.Capacity != nil {
		snap["capacity"] = *e.Capacity
	}
	return snap
}

// saveWithAudit persists the row and emits event.updated with the field
// diff (no-op rows still save; empty diffs emit after-only status context).
func (s *EventService) saveWithAudit(actorID, orgID uuid.UUID, e *model.Event, before map[string]any) error {
	after := eventSnapshot(e)
	diffBefore, diffAfter := map[string]any{}, map[string]any{}
	for k, b := range before {
		if a, ok := after[k]; !ok || a != b {
			diffBefore[k] = b
			if a, ok := after[k]; ok {
				diffAfter[k] = a
			}
		}
	}
	for k, a := range after {
		if _, ok := before[k]; !ok {
			diffAfter[k] = a
		}
	}
	return s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.UpdateEvent(tx, e); err != nil {
			if isUniqueViolation(err) {
				return ErrSlugTaken
			}
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &orgID, ActorID: &actorID,
			Action: "event.updated", ObjectType: auditmodel.ObjectEvent, ObjectID: &e.ID,
			Before: diffBefore, After: diffAfter,
		})
	})
}

// Publish transitions DRAFT -> PUBLISHED and queues announcements to
// opted-in members transactionally with the transition.
func (s *EventService) Publish(actorID uuid.UUID, orgRef, eventRef string) (*eventdto.Event, error) {
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return nil, ErrOrgUnresolved
	}
	e, err := s.resolveEvent(orgID, eventRef)
	if err != nil {
		return nil, err
	}
	if e.Status != model.EventStatusDraft {
		return nil, ErrInvalidStatus
	}
	e.Status = model.EventStatusPublished
	targets, err := s.orgs.NotifyTargets(orgID)
	if err != nil {
		return nil, err
	}
	orgName, err := s.orgs.OrgName(orgID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.UpdateEvent(tx, e); err != nil {
			return err
		}
		if err := s.emit(tx, auditsvc.Entry{
			OrgID: &orgID, ActorID: &actorID,
			Action: "event.published", ObjectType: auditmodel.ObjectEvent, ObjectID: &e.ID,
			Before: map[string]any{"status": string(model.EventStatusDraft)},
			After:  map[string]any{"status": string(model.EventStatusPublished)},
		}); err != nil {
			return err
		}
		for _, t := range targets {
			if err := s.enqueueTx(tx, jobs.SendEventPublishedEmailArgs{
				EventID: e.ID, EventTitle: e.Title,
				OrgName: orgName, Email: t.Email, Name: t.Name,
				EventLink: config.AppURL() + "/events/" + e.ID.String(),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	s.purgePublicEvent(e.ID)
	return toEvent(e), nil
}

// Unpublish transitions PUBLISHED -> DRAFT.
func (s *EventService) Unpublish(actorID uuid.UUID, orgRef, eventRef string) (*eventdto.Event, error) {
	return s.transition(actorID, orgRef, eventRef, model.EventStatusPublished, model.EventStatusDraft)
}

// Cancel transitions PUBLISHED -> CANCELLED (terminal).
func (s *EventService) Cancel(actorID uuid.UUID, orgRef, eventRef string) (*eventdto.Event, error) {
	return s.transition(actorID, orgRef, eventRef, model.EventStatusPublished, model.EventStatusCancelled)
}

func (s *EventService) transition(actorID uuid.UUID, orgRef, eventRef string, from, to model.EventStatus) (*eventdto.Event, error) {
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return nil, ErrOrgUnresolved
	}
	e, err := s.resolveEvent(orgID, eventRef)
	if err != nil {
		return nil, err
	}
	if e.Status != from {
		return nil, ErrInvalidStatus
	}
	e.Status = to
	action := "event.unpublished"
	if to == model.EventStatusCancelled {
		action = "event.cancelled"
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.repo.UpdateEvent(tx, e); err != nil {
			return err
		}
		return s.emit(tx, auditsvc.Entry{
			OrgID: &orgID, ActorID: &actorID,
			Action: action, ObjectType: auditmodel.ObjectEvent, ObjectID: &e.ID,
			Before: map[string]any{"status": string(from)},
			After:  map[string]any{"status": string(to)},
		})
	}); err != nil {
		return nil, err
	}
	s.purgePublicEvent(e.ID)
	return toEvent(e), nil
}

// DeleteEvent hard-deletes an event (OWNER upstream) with its cover and
// gallery assets (rows cascade in DDL; provider cleanup best-effort).
func (s *EventService) DeleteEvent(actorID uuid.UUID, orgRef, eventRef string) error {
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return ErrOrgUnresolved
	}
	e, err := s.resolveEvent(orgID, eventRef)
	if err != nil {
		return err
	}
	var assets []string
	if e.CoverURL != nil && *e.CoverURL != "" {
		assets = append(assets, *e.CoverURL)
	}
	if imgs, err := s.repo.ListImagesByEvent(e.ID); err == nil {
		for _, img := range imgs {
			assets = append(assets, img.URL)
		}
	}
	if err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.emit(tx, auditsvc.Entry{
			OrgID: &orgID, ActorID: &actorID,
			Action: "event.deleted", ObjectType: auditmodel.ObjectEvent, ObjectID: &e.ID,
			Before: map[string]any{"title": e.Title, "slug": e.Slug},
		}); err != nil {
			return err
		}
		return s.repo.DeleteEvent(tx, e.ID)
	}); err != nil {
		return err
	}
	s.purgePublicEvent(e.ID)
	for _, url := range assets {
		s.deleteCover(url)
	}
	return nil
}

// ---------- gallery ----------

// GalleryUpload is one validated file for the gallery.
type GalleryUpload struct {
	Data        io.Reader
	Size        int64
	ContentType string
}

// AddGalleryImages uploads files, stores rows, and — when the event has no
// cover yet — clones the first image as the cover photo. All-or-nothing:
// uploaded assets are cleaned up if the row transaction fails.
func (s *EventService) AddGalleryImages(orgRef, eventRef string, uploads []GalleryUpload) ([]eventdto.EventImage, error) {
	if s.storage == nil {
		return nil, ErrCoverRequired
	}
	if len(uploads) == 0 {
		return nil, ErrCoverRequired
	}
	if len(uploads) > maxGalleryFiles {
		return nil, ErrTooManyFiles
	}
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return nil, ErrOrgUnresolved
	}
	e, err := s.resolveEvent(orgID, eventRef)
	if err != nil {
		return nil, err
	}
	stagedUploads := make([]stagedUpload, 0, len(uploads))
	for _, up := range uploads {
		ext, err := checkCover(up.Size, up.ContentType)
		if err != nil {
			s.cleanupStaged(stagedUploads)
			return nil, err
		}
		stored, err := s.storage.SaveImage(orgID, e.ID, up.Data, ext)
		if err != nil {
			s.cleanupStaged(stagedUploads)
			return nil, err
		}
		stagedUploads = append(stagedUploads, stagedUpload{stored: stored, ext: ext})
	}
	rows := make([]model.EventImage, 0, len(stagedUploads))
	for _, st := range stagedUploads {
		rows = append(rows, model.EventImage{
			EventID: e.ID, URL: st.stored.URL, PublicID: st.stored.PublicID,
			Format: st.stored.Format, Bytes: st.stored.Bytes,
			Width: st.stored.Width, Height: st.stored.Height,
		})
	}
	coverCloned := e.CoverURL == nil || *e.CoverURL == ""
	err = s.repo.DB().Transaction(func(tx *gorm.DB) error {
		for i := range rows {
			if err := s.repo.CreateImage(tx, &rows[i]); err != nil {
				return err
			}
		}
		if coverCloned {
			url := rows[0].URL
			e.CoverURL = &url
			if err := s.repo.UpdateEvent(tx, e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.cleanupStaged(stagedUploads)
		return nil, err
	}
	// Gallery rows are not in the detail payload — but a cloned cover is,
	// so only that case purges.
	if coverCloned {
		s.purgePublicEvent(e.ID)
	}
	out := make([]eventdto.EventImage, 0, len(rows))
	for _, r := range rows {
		out = append(out, *toEventImage(r))
	}
	return out, nil
}

// stagedUpload pairs an uploaded asset with its validated extension
// so failures can clean up provider-side files.
type stagedUpload struct {
	stored *cover.StoredImage
	ext    string
}

// ListImages returns an event's gallery, oldest first.
func (s *EventService) ListImages(orgRef, eventRef string) ([]eventdto.EventImage, error) {
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return nil, ErrOrgUnresolved
	}
	e, err := s.resolveEvent(orgID, eventRef)
	if err != nil {
		return nil, err
	}
	imgs, err := s.repo.ListImagesByEvent(e.ID)
	if err != nil {
		return nil, err
	}
	out := make([]eventdto.EventImage, 0, len(imgs))
	for _, img := range imgs {
		out = append(out, *toEventImage(img))
	}
	return out, nil
}

// cleanupStaged deletes uploaded assets after a failed gallery transaction.
func (s *EventService) cleanupStaged(staged []stagedUpload) {
	for _, st := range staged {
		s.deleteCover(st.stored.URL)
	}
}

func toEventImage(img model.EventImage) *eventdto.EventImage {
	return &eventdto.EventImage{
		ID: img.ID.String(), URL: img.URL, PublicID: img.PublicID,
		Format: img.Format, Bytes: img.Bytes, Width: img.Width, Height: img.Height,
		CreatedAt: utils.FormatTime(img.CreatedAt),
	}
}

// DeleteOrgAssets removes every stored asset of an org (covers + gallery).
// Called by org delete after rows cascade; best-effort with warn logs.
func (s *EventService) DeleteOrgAssets(orgID uuid.UUID) error {
	urls, err := s.ListOrgAssets(orgID)
	if err != nil {
		return err
	}
	for _, url := range urls {
		s.deleteCover(url)
	}
	return nil
}

// ListOrgAssets returns every stored asset URL of an org (covers + gallery).
func (s *EventService) ListOrgAssets(orgID uuid.UUID) ([]string, error) {
	es, _, err := s.repo.ListEvents(repository.EventFilter{
		OrganizationID: &orgID, IncludeDrafts: true,
	}, 100000, 0, "")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		if e.CoverURL != nil && *e.CoverURL != "" {
			out = append(out, *e.CoverURL)
		}
		imgs, err := s.repo.ListImagesByEvent(e.ID)
		if err != nil {
			return nil, err
		}
		for _, img := range imgs {
			out = append(out, img.URL)
		}
	}
	return out, nil
}

// ---------- covers ----------

// SetCover validates and stores a cover image, replacing any existing one.
// contentType must be sniffed from bytes (http.DetectContentType), never
// trusted from the client.
func (s *EventService) SetCover(orgRef, eventRef string, data io.Reader, size int64, contentType string) (*eventdto.Event, error) {
	if s.storage == nil {
		return nil, ErrCoverRequired
	}
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return nil, ErrOrgUnresolved
	}
	e, err := s.resolveEvent(orgID, eventRef)
	if err != nil {
		return nil, err
	}
	ext, err := checkCover(size, contentType)
	if err != nil {
		return nil, err
	}
	url, err := s.storage.Save(orgID, e.ID, data, ext)
	if err != nil {
		return nil, err
	}
	old := ""
	if e.CoverURL != nil {
		old = *e.CoverURL
	}
	e.CoverURL = &url
	if err := s.repo.UpdateEvent(nil, e); err != nil {
		s.deleteCover(url)
		return nil, err
	}
	if old != "" && old != url {
		s.deleteCover(old)
	}
	s.purgePublicEvent(e.ID)
	return toEvent(e), nil
}

// checkCover validates size and sniffed MIME type.
func checkCover(size int64, contentType string) (string, error) {
	if size <= 0 {
		return "", ErrCoverRequired
	}
	if size > maxCoverBytes {
		return "", ErrCoverTooLarge
	}
	ext, ok := allowedCoverTypes[contentType]
	if !ok {
		return "", ErrCoverType
	}
	return ext, nil
}

// ---------- helpers ----------

func (s *EventService) resolveEvent(orgID uuid.UUID, ref string) (*model.Event, error) {
	ref = strings.TrimSpace(ref)
	if id, err := uuid.Parse(ref); err == nil {
		e, err := s.repo.GetEventByID(id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrEventNotFound
			}
			return nil, err
		}
		if e.OrganizationID != orgID {
			return nil, ErrEventNotFound
		}
		return e, nil
	}
	e, err := s.repo.GetEventBySlug(orgID, strings.ToLower(ref))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEventNotFound
		}
		return nil, err
	}
	return e, nil
}

func (s *EventService) uniqueSlug(orgID uuid.UUID, base string) (string, error) {
	if _, err := s.repo.GetEventBySlug(orgID, base); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return base, nil
		}
		return "", err
	}
	for i := 2; i < 100; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if _, err := s.repo.GetEventBySlug(orgID, cand); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return cand, nil
			}
			return "", err
		}
	}
	raw, err := token.GenerateRawToken(3)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", base, raw[:6]), nil
}

func checkDates(starts, ends *time.Time) error {
	if starts != nil && ends != nil && !ends.UTC().After(starts.UTC()) {
		return ErrInvalidDates
	}
	return nil
}

// utcPtr normalizes an optional timestamp to UTC.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "UNIQUE constraint failed")
}

func (s *EventService) enqueueTx(tx *gorm.DB, args interface{ Kind() string }) error {
	if s.enqueuer == nil {
		return nil
	}
	return s.enqueuer.EnqueueTx(context.Background(), tx, args)
}

func (s *EventService) deleteCover(url string) {
	if s.storage == nil || url == "" {
		return
	}
	if err := s.storage.Delete(url); err != nil {
		slog.Warn("cover delete failed", "url", url, "error", err)
	}
}

// purgePublicEvent drops the cached public payload after a committed
// write. Best-effort post-commit: failures log and self-heal via
// publicEventTTL. Called for every mutation that can change what
// GetPublicEvent serves (create needs none — drafts are never cached).
func (s *EventService) purgePublicEvent(id uuid.UUID) {
	if s.dcache == nil {
		return
	}
	if err := s.dcache.Del(context.Background(), publicEventKey(id)); err != nil {
		slog.Warn("event detail cache purge failed", "eventID", id, "error", err)
	}
}

func toEvent(e *model.Event) *eventdto.Event {
	return &eventdto.Event{
		ID: e.ID.String(), Title: e.Title, Slug: e.Slug,
		Description: e.Description, Venue: e.Venue, Location: e.Location,
		StartsAt: utils.FormatTimePtr(e.StartsAt), EndsAt: utils.FormatTimePtr(e.EndsAt),
		Capacity: e.Capacity, CoverURL: e.CoverURL, Status: string(e.Status),
		CreatedAt: utils.FormatTime(e.CreatedAt), UpdatedAt: utils.FormatTime(e.UpdatedAt),
	}
}

// OrgOf returns the owning org of an event (internal cross-domain use).
func (s *EventService) OrgOf(eventID uuid.UUID) (uuid.UUID, error) {
	e, err := s.repo.GetEventByID(eventID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return uuid.Nil, ErrEventNotFound
		}
		return uuid.Nil, err
	}
	return e.OrganizationID, nil
}

// EventTitle returns an event title (internal cross-domain use).
func (s *EventService) EventTitle(eventID uuid.UUID) (string, error) {
	e, err := s.repo.GetEventByID(eventID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrEventNotFound
		}
		return "", err
	}
	return e.Title, nil
}

// ResolveEventID resolves an event ref within an org (internal cross-domain
// use: attendee routes carry org context + event slug/UUID).
func (s *EventService) ResolveEventID(orgID uuid.UUID, ref string) (uuid.UUID, error) {
	e, err := s.resolveEvent(orgID, ref)
	if err != nil {
		return uuid.Nil, err
	}
	return e.ID, nil
}
