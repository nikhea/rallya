package service_test

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/event/cover"
	eventdto "github.com/nikhea/rallya/internal/event/dto"
	eventmodel "github.com/nikhea/rallya/internal/event/model"
	"github.com/nikhea/rallya/internal/event/repository"
	"github.com/nikhea/rallya/internal/event/service"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgdto "github.com/nikhea/rallya/internal/organization/dto"
)

// fakeOrgs implements service.OrgResolver in-memory.
type fakeOrgs struct {
	ids     map[string]uuid.UUID
	members map[uuid.UUID]map[uuid.UUID]bool
	targets map[uuid.UUID][]orgdto.MemberNotify
	names   map[uuid.UUID]string
}

func newFakeOrgs() *fakeOrgs {
	return &fakeOrgs{
		ids: map[string]uuid.UUID{}, members: map[uuid.UUID]map[uuid.UUID]bool{},
		targets: map[uuid.UUID][]orgdto.MemberNotify{}, names: map[uuid.UUID]string{},
	}
}

func (f *fakeOrgs) addOrg(slug, name string) uuid.UUID {
	id := uuid.New()
	f.ids[slug] = id
	f.names[id] = name
	return id
}

func (f *fakeOrgs) addMember(org, user uuid.UUID, email, mname string, notify bool) {
	if f.members[org] == nil {
		f.members[org] = map[uuid.UUID]bool{}
	}
	f.members[org][user] = true
	if notify {
		f.targets[org] = append(f.targets[org], orgdto.MemberNotify{UserID: user, Email: email, Name: mname})
	}
}

func (f *fakeOrgs) ResolveOrgID(ref string) (uuid.UUID, error) {
	if id, ok := f.ids[ref]; ok {
		return id, nil
	}
	return uuid.Nil, errNoOrg
}

func (f *fakeOrgs) OrgName(orgID uuid.UUID) (string, error) {
	if n, ok := f.names[orgID]; ok {
		return n, nil
	}
	return "", errNoOrg
}

func (f *fakeOrgs) IsMember(userID, orgID uuid.UUID) bool {
	return f.members[orgID][userID]
}

func (f *fakeOrgs) NotifyTargets(orgID uuid.UUID) ([]orgdto.MemberNotify, error) {
	return f.targets[orgID], nil
}

var errNoOrg = errors.New("no org")

// fakeStorage records cover saves in memory.
type fakeStorage struct {
	saved    map[string][]byte
	deleted  []string
	failSave bool
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{saved: map[string][]byte{}}
}

func (f *fakeStorage) Save(orgID, eventID uuid.UUID, data io.Reader, ext string) (string, error) {
	if f.failSave {
		return "", errors.New("storage boom")
	}
	b, _ := io.ReadAll(data)
	url := "/uploads/events/" + orgID.String() + "/" + eventID.String()[:8] + ext
	f.saved[url] = b
	return url, nil
}

func (f *fakeStorage) SaveImage(orgID, eventID uuid.UUID, data io.Reader, ext string) (*cover.StoredImage, error) {
	if f.failSave {
		return nil, errors.New("storage boom")
	}
	b, _ := io.ReadAll(data)
	url := "/uploads/events/" + orgID.String() + "/" + eventID.String()[:8] + "-" + string(rune('a'+len(f.saved))) + ext
	f.saved[url] = b
	format := strings.TrimPrefix(ext, ".")
	return &cover.StoredImage{URL: url, PublicID: strings.TrimPrefix(url, "/uploads/"), Format: format, Bytes: len(b), Width: 100, Height: 80}, nil
}

func (f *fakeStorage) Delete(url string) error {
	f.deleted = append(f.deleted, url)
	delete(f.saved, url)
	return nil
}

type eventFixture struct {
	svc     *service.EventService
	orgs    *fakeOrgs
	fake    *jobs.FakeEnqueuer
	storage *fakeStorage
	org     uuid.UUID
	user    uuid.UUID
}

func newEventFixture(t *testing.T) *eventFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	if err := db.AutoMigrate(eventmodel.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewEventRepository(db)
	orgs := newFakeOrgs()
	svc := service.NewEventService(repo, orgs)
	fake := &jobs.FakeEnqueuer{}
	svc.SetEnqueuer(fake)
	storage := newFakeStorage()
	svc.SetCoverStorage(storage)
	org := orgs.addOrg("acme", "Acme")
	user := uuid.New()
	orgs.addMember(org, user, "owner@test.com", "Owner", true)
	return &eventFixture{svc: svc, orgs: orgs, fake: fake, storage: storage, org: org, user: user}
}

func TestCreateSlugSequenceAndValidation(t *testing.T) {
	f := newEventFixture(t)
	mk := func(title string) string {
		d, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: title})
		if err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
		return d.Slug
	}
	if got := []string{mk("Summer Fest"), mk("Summer Fest"), mk("Summer Fest")}; got[0] != "summer-fest" || got[1] != "summer-fest-2" || got[2] != "summer-fest-3" {
		t.Fatalf("slugs = %v", got)
	}
	if _, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: "  "}); !errors.Is(err, service.ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle, got %v", err)
	}
	if _, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: "X", Slug: "BAD SLUG!"}); !errors.Is(err, service.ErrInvalidSlug) {
		t.Fatalf("expected ErrInvalidSlug, got %v", err)
	}
	if _, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: "X", Slug: "summer-fest"}); !errors.Is(err, service.ErrSlugTaken) {
		t.Fatalf("expected ErrSlugTaken, got %v", err)
	}
	if _, err := f.svc.CreateEvent(f.user, "nope", service.CreateInput{Title: "X"}); !errors.Is(err, service.ErrOrgUnresolved) {
		t.Fatalf("expected ErrOrgUnresolved, got %v", err)
	}
	// Dates: ends before starts rejected; capacity enforced.
	past := time.Now().Add(-2 * time.Hour)
	now := time.Now()
	if _, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: "X", StartsAt: &now, EndsAt: &past}); !errors.Is(err, service.ErrInvalidDates) {
		t.Fatalf("expected ErrInvalidDates, got %v", err)
	}
	zero := 0
	if _, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: "X", Capacity: &zero}); !errors.Is(err, service.ErrInvalidCap) {
		t.Fatalf("expected ErrInvalidCap, got %v", err)
	}
}

func TestDraftStealthAndPublicReads(t *testing.T) {
	f := newEventFixture(t)
	d, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: "Secret"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	stranger := uuid.New()
	// Draft: owner sees, stranger gets not-found, anon gets not-found.
	if _, err := f.svc.GetEvent(&f.user, "acme", d.Slug); err != nil {
		t.Fatalf("owner get draft: %v", err)
	}
	if _, err := f.svc.GetEvent(&stranger, "acme", d.Slug); !errors.Is(err, service.ErrEventNotFound) {
		t.Fatalf("expected ErrEventNotFound, got %v", err)
	}
	if _, err := f.svc.GetEvent(nil, "acme", d.Slug); !errors.Is(err, service.ErrEventNotFound) {
		t.Fatalf("expected ErrEventNotFound anon, got %v", err)
	}
	// Public surface hides drafts.
	if _, err := f.svc.GetPublicEvent(mustParseUUID(t, d.ID)); !errors.Is(err, service.ErrEventNotFound) {
		t.Fatalf("expected ErrEventNotFound public, got %v", err)
	}
	items, total, err := f.svc.ListPublic(eventdto.EventFilter{}, 20, 0, "")
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("public list: %d %v", total, err)
	}
}

func TestLifecyclePublishUnpublishCancel(t *testing.T) {
	f := newEventFixture(t)
	d, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: "Show"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Cancel from draft -> invalid.
	if _, err := f.svc.Cancel(f.user, "acme", d.Slug); !errors.Is(err, service.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
	// Publish -> announcements to opted-in only.
	outsider := uuid.New()
	f.orgs.addMember(f.org, outsider, "quiet@test.com", "Quiet", false)
	loud := uuid.New()
	f.orgs.addMember(f.org, loud, "loud@test.com", "Loud", true)
	_ = outsider
	pub, err := f.svc.Publish(f.user, "acme", d.Slug)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.Status != "PUBLISHED" {
		t.Fatalf("status = %s", pub.Status)
	}
	announced := f.fake.OfKind("send_event_published_email")
	emails := map[string]bool{}
	for _, j := range announced {
		args, ok := j.(jobs.SendEventPublishedEmailArgs)
		if !ok {
			t.Fatalf("bad args %T", j)
		}
		emails[args.Email] = true
		if args.EventTitle != "Show" || args.EventLink == "" {
			t.Fatalf("bad args: %+v", args)
		}
	}
	// owner (opted in via addMember default true) + loud; quiet excluded.
	if !emails["owner@test.com"] || !emails["loud@test.com"] || emails["quiet@test.com"] {
		t.Fatalf("announcement targeting wrong: %v", emails)
	}
	// Double publish -> invalid.
	if _, err := f.svc.Publish(f.user, "acme", d.Slug); !errors.Is(err, service.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
	// Unpublish -> draft, then cancel-from-draft invalid, publish, cancel.
	if _, err := f.svc.Unpublish(f.user, "acme", d.Slug); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	if _, err := f.svc.Cancel(f.user, "acme", d.Slug); !errors.Is(err, service.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
	if _, err := f.svc.Publish(f.user, "acme", d.Slug); err != nil {
		t.Fatalf("republish: %v", err)
	}
	if _, err := f.svc.Cancel(f.user, "acme", d.Slug); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// Cancelled is terminal.
	if _, err := f.svc.Publish(f.user, "acme", d.Slug); !errors.Is(err, service.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
	// Published (then cancelled) visibility via public API while published.
}

func TestUpdateDeleteAndCovers(t *testing.T) {
	f := newEventFixture(t)
	d, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: "Old"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	newTitle := "New"
	upd, err := f.svc.UpdateEvent(f.user, "acme", d.Slug, service.UpdateInput{Title: &newTitle})
	if err != nil || upd.Title != "New" {
		t.Fatalf("update: %+v %v", upd, err)
	}
	bad := ""
	if _, err := f.svc.UpdateEvent(f.user, "acme", d.Slug, service.UpdateInput{Title: &bad}); !errors.Is(err, service.ErrInvalidTitle) {
		t.Fatalf("expected ErrInvalidTitle, got %v", err)
	}
	// Cover validation.
	if _, err := f.svc.SetCover("acme", d.Slug, strings.NewReader("x"), 0, "image/png"); !errors.Is(err, service.ErrCoverRequired) {
		t.Fatalf("expected ErrCoverRequired, got %v", err)
	}
	if _, err := f.svc.SetCover("acme", d.Slug, strings.NewReader("x"), 6<<20, "image/png"); !errors.Is(err, service.ErrCoverTooLarge) {
		t.Fatalf("expected ErrCoverTooLarge, got %v", err)
	}
	if _, err := f.svc.SetCover("acme", d.Slug, strings.NewReader("x"), 100, "application/pdf"); !errors.Is(err, service.ErrCoverType) {
		t.Fatalf("expected ErrCoverType, got %v", err)
	}
	withCover, err := f.svc.SetCover("acme", d.Slug, strings.NewReader("imagedata"), 9, "image/png")
	if err != nil || withCover.CoverURL == nil || !strings.HasSuffix(*withCover.CoverURL, ".png") {
		t.Fatalf("cover: %+v %v", withCover, err)
	}
	firstURL := *withCover.CoverURL
	withCover2, err := f.svc.SetCover("acme", d.Slug, strings.NewReader("img2"), 4, "image/jpeg")
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if len(f.storage.deleted) != 1 || f.storage.deleted[0] != firstURL {
		t.Fatalf("expected old cover deleted, got %v", f.storage.deleted)
	}
	_ = withCover2
	// Delete removes cover too.
	if err := f.svc.DeleteEvent(f.user, "acme", d.Slug); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(f.storage.deleted) != 2 {
		t.Fatalf("expected cover cleanup, got %v", f.storage.deleted)
	}
	if _, err := f.svc.GetEvent(&f.user, "acme", d.Slug); !errors.Is(err, service.ErrEventNotFound) {
		t.Fatalf("expected ErrEventNotFound, got %v", err)
	}
}

func TestListFilters(t *testing.T) {
	f := newEventFixture(t)
	future := time.Now().Add(48 * time.Hour).UTC()
	past := time.Now().Add(-48 * time.Hour).UTC()
	mk := func(title string, at *time.Time, pub bool) {
		d, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: title, StartsAt: at})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if pub {
			if _, err := f.svc.Publish(f.user, "acme", d.Slug); err != nil {
				t.Fatalf("publish: %v", err)
			}
		}
	}
	mk("Future Fest", &future, true)
	mk("Old Fest", &past, true)
	mk("Draft Fest", &future, false)

	items, total, err := f.svc.ListPublic(eventdto.EventFilter{}, 20, 0, "")
	if err != nil || total != 2 {
		t.Fatalf("public total=%d err=%v", total, err)
	}
	_ = items
	// Date range excludes the past event.
	from := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	items, total, err = f.svc.ListPublic(eventdto.EventFilter{From: &from}, 20, 0, "")
	if err != nil || total != 1 || items[0].Title != "Future Fest" {
		t.Fatalf("from filter: %+v %d %v", items, total, err)
	}
	// Search.
	q := "old"
	items, total, err = f.svc.ListPublic(eventdto.EventFilter{Query: q}, 20, 0, "")
	if err != nil || total != 1 || items[0].Title != "Old Fest" {
		t.Fatalf("search: %+v %d %v", items, total, err)
	}
	// Org listing includes drafts.
	items, total, err = f.svc.ListOrgEvents("acme", eventdto.EventFilter{}, 20, 0, "")
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("org list: %d %v", total, err)
	}
	// Bad status/date strings.
	bad := "nope"
	if _, _, err := f.svc.ListPublic(eventdto.EventFilter{Status: &bad}, 20, 0, ""); err == nil {
		t.Fatal("expected error for bad status")
	}
	// Unknown org.
	if _, _, err := f.svc.ListOrgEvents("ghost", eventdto.EventFilter{}, 20, 0, ""); err == nil {
		t.Fatal("expected error for unknown org")
	}
}

func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return id
}

func TestGalleryUploadListAndAutoCover(t *testing.T) {
	f := newEventFixture(t)
	d, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: "Gallery Fest"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	mkUpload := func(body string) service.GalleryUpload {
		return service.GalleryUpload{Data: strings.NewReader(body), Size: int64(len(body)), ContentType: "image/png"}
	}
	// Empty + too many rejected.
	if _, err := f.svc.AddGalleryImages("acme", d.Slug, nil); !errors.Is(err, service.ErrCoverRequired) {
		t.Fatalf("expected ErrCoverRequired, got %v", err)
	}
	many := make([]service.GalleryUpload, 11)
	for i := range many {
		many[i] = mkUpload("x")
	}
	if _, err := f.svc.AddGalleryImages("acme", d.Slug, many); !errors.Is(err, service.ErrTooManyFiles) {
		t.Fatalf("expected ErrTooManyFiles, got %v", err)
	}
	// Bad file in batch rejected, nothing stored.
	if _, err := f.svc.AddGalleryImages("acme", d.Slug, []service.GalleryUpload{
		mkUpload("good"), {Data: strings.NewReader("bad"), Size: 10, ContentType: "application/pdf"},
	}); !errors.Is(err, service.ErrCoverType) {
		t.Fatalf("expected ErrCoverType, got %v", err)
	}
	// Two valid uploads: rows stored, first cloned as cover (none existed).
	images, err := f.svc.AddGalleryImages("acme", d.Slug, []service.GalleryUpload{mkUpload("img1data"), mkUpload("img2data")})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if len(images) != 2 || images[0].Format != "png" || images[0].Bytes != len("img1data") || images[0].Width != 100 {
		t.Fatalf("unexpected images: %+v", images)
	}
	got, err := f.svc.GetEvent(&f.user, "acme", d.Slug)
	if err != nil || got.CoverURL == nil || *got.CoverURL != images[0].URL {
		t.Fatalf("expected first image cloned as cover: %+v %v", got, err)
	}
	// List returns both, oldest first.
	listed, err := f.svc.ListImages("acme", d.Slug)
	if err != nil || len(listed) != 2 || listed[0].URL != images[0].URL {
		t.Fatalf("list: %+v %v", listed, err)
	}
	// Third upload does NOT override the existing cover.
	more, err := f.svc.AddGalleryImages("acme", d.Slug, []service.GalleryUpload{mkUpload("img3")})
	if err != nil || len(more) != 1 {
		t.Fatalf("upload3: %+v %v", more, err)
	}
	got, _ = f.svc.GetEvent(&f.user, "acme", d.Slug)
	if *got.CoverURL != images[0].URL {
		t.Fatalf("cover must stay first image, got %v", got.CoverURL)
	}
	// Unknown org/event.
	if _, err := f.svc.ListImages("ghost", d.Slug); !errors.Is(err, service.ErrOrgUnresolved) {
		t.Fatalf("expected ErrOrgUnresolved, got %v", err)
	}
	if _, err := f.svc.ListImages("acme", "ghost"); !errors.Is(err, service.ErrEventNotFound) {
		t.Fatalf("expected ErrEventNotFound, got %v", err)
	}
}

func TestDeleteEventCleansGalleryAssets(t *testing.T) {
	f := newEventFixture(t)
	d, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: "Temp Fest"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	mk := func(body string) service.GalleryUpload {
		return service.GalleryUpload{Data: strings.NewReader(body), Size: int64(len(body)), ContentType: "image/png"}
	}
	if _, err := f.svc.AddGalleryImages("acme", d.Slug, []service.GalleryUpload{mk("a"), mk("b")}); err != nil {
		t.Fatalf("upload: %v", err)
	}
	before := len(f.storage.deleted)
	if err := f.svc.DeleteEvent(f.user, "acme", d.Slug); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// cover clone + 2 gallery assets cleaned.
	if len(f.storage.deleted)-before != 3 {
		t.Fatalf("expected 3 asset cleanups, got %v", f.storage.deleted)
	}
}
