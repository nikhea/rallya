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
	"github.com/nikhea/rallya/internal/auth/token"
	eventdto "github.com/nikhea/rallya/internal/event/dto"
	"github.com/nikhea/rallya/internal/event/model"
	"github.com/nikhea/rallya/internal/event/repository"
	"github.com/nikhea/rallya/internal/event/utils"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
)

// Cover size/type policy (Q11).
const (
	maxCoverBytes = 5 << 20 // 5MB
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

// CoverStorage persists cover images. Local disk for MVP;
// swap the implementation for S3 later (URLs stay stable).
type CoverStorage interface {
	Save(orgID, eventID uuid.UUID, data io.Reader, ext string) (url string, err error)
	Delete(url string) error
}

// EventService orchestrates event flows.
type EventService struct {
	repo     *repository.EventRepository
	orgs     OrgResolver
	enqueuer jobs.Enqueuer
	storage  CoverStorage
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
		err := s.repo.CreateEvent(nil, e)
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
func (s *EventService) GetPublicEvent(id uuid.UUID) (*eventdto.Event, error) {
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
	return toEvent(e), nil
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
func (s *EventService) UpdateEvent(orgRef, eventRef string, in UpdateInput) (*eventdto.Event, error) {
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return nil, ErrOrgUnresolved
	}
	e, err := s.resolveEvent(orgID, eventRef)
	if err != nil {
		return nil, err
	}
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
		if err := s.repo.UpdateEvent(nil, e); err != nil {
			return nil, err
		}
		if old != "" {
			s.deleteCover(old)
		}
		return toEvent(e), nil
	}
	if err := s.repo.UpdateEvent(nil, e); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrSlugTaken
		}
		return nil, err
	}
	return toEvent(e), nil
}

// Publish transitions DRAFT -> PUBLISHED and queues announcements to
// opted-in members transactionally with the transition.
func (s *EventService) Publish(orgRef, eventRef string) (*eventdto.Event, error) {
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
	return toEvent(e), nil
}

// Unpublish transitions PUBLISHED -> DRAFT.
func (s *EventService) Unpublish(orgRef, eventRef string) (*eventdto.Event, error) {
	return s.transition(orgRef, eventRef, model.EventStatusPublished, model.EventStatusDraft)
}

// Cancel transitions PUBLISHED -> CANCELLED (terminal).
func (s *EventService) Cancel(orgRef, eventRef string) (*eventdto.Event, error) {
	return s.transition(orgRef, eventRef, model.EventStatusPublished, model.EventStatusCancelled)
}

func (s *EventService) transition(orgRef, eventRef string, from, to model.EventStatus) (*eventdto.Event, error) {
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
	if err := s.repo.UpdateEvent(nil, e); err != nil {
		return nil, err
	}
	return toEvent(e), nil
}

// DeleteEvent hard-deletes an event (OWNER upstream) and its cover.
func (s *EventService) DeleteEvent(orgRef, eventRef string) error {
	orgID, err := s.orgs.ResolveOrgID(orgRef)
	if err != nil {
		return ErrOrgUnresolved
	}
	e, err := s.resolveEvent(orgID, eventRef)
	if err != nil {
		return err
	}
	cover := ""
	if e.CoverURL != nil {
		cover = *e.CoverURL
	}
	if err := s.repo.DeleteEvent(nil, e.ID); err != nil {
		return err
	}
	if cover != "" {
		s.deleteCover(cover)
	}
	return nil
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

func toEvent(e *model.Event) *eventdto.Event {
	return &eventdto.Event{
		ID: e.ID.String(), Title: e.Title, Slug: e.Slug,
		Description: e.Description, Venue: e.Venue, Location: e.Location,
		StartsAt: utils.FormatTimePtr(e.StartsAt), EndsAt: utils.FormatTimePtr(e.EndsAt),
		Capacity: e.Capacity, CoverURL: e.CoverURL, Status: string(e.Status),
		CreatedAt: utils.FormatTime(e.CreatedAt), UpdatedAt: utils.FormatTime(e.UpdatedAt),
	}
}
