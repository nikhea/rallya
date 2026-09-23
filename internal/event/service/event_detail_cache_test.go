package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	eventdto "github.com/nikhea/rallya/internal/event/dto"
	"github.com/nikhea/rallya/internal/event/service"
)

// fakeDetailCache implements service.DetailCache over a JSON map so tests
// exercise the same encode/decode boundary as the Redis store.
type fakeDetailCache struct {
	items   map[string][]byte
	gets    int
	sets    int
	dels    []string
	failGet bool
	failSet bool
	failDel bool
}

func newFakeDetailCache() *fakeDetailCache {
	return &fakeDetailCache{items: map[string][]byte{}}
}

func (f *fakeDetailCache) Get(_ context.Context, key string, dst any) (bool, error) {
	f.gets++
	if f.failGet {
		return false, errors.New("cache boom")
	}
	raw, ok := f.items[key]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return false, err
	}
	return true, nil
}

func (f *fakeDetailCache) Set(_ context.Context, key string, val any, _ time.Duration) error {
	f.sets++
	if f.failSet {
		return errors.New("cache boom")
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return err
	}
	f.items[key] = raw
	return nil
}

func (f *fakeDetailCache) Del(_ context.Context, keys ...string) error {
	if f.failDel {
		return errors.New("cache boom")
	}
	for _, k := range keys {
		delete(f.items, k)
		f.dels = append(f.dels, k)
	}
	return nil
}

func (f *fakeDetailCache) has(id uuid.UUID) bool {
	_, ok := f.items["evt:pub:"+id.String()]
	return ok
}

// publishFixture creates a draft and publishes it (announcements fan out
// to zero targets on the fake orgs).
func publishFixture(t *testing.T, f *eventFixture, title string) eventdto.Event {
	t.Helper()
	d, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: title})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pub, err := f.svc.Publish(f.user, "acme", d.Slug)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return *pub
}

func TestPublicEventCacheMissPopulateHit(t *testing.T) {
	f := newEventFixture(t)
	dc := newFakeDetailCache()
	f.svc.SetDetailCache(dc)
	pub := publishFixture(t, f, "Cached Show")
	id := mustParseUUID(t, pub.ID)

	// Miss: serves from Postgres and populates.
	first, err := f.svc.GetPublicEvent(id)
	if err != nil || first.Title != "Cached Show" {
		t.Fatalf("miss: %+v %v", first, err)
	}
	if dc.sets != 1 || !dc.has(id) {
		t.Fatalf("miss must populate: sets=%d cached=%v", dc.sets, dc.has(id))
	}
	// Hit: forge the entry and prove the DB is bypassed.
	forged, _ := json.Marshal(eventdto.Event{ID: pub.ID, Title: "Forged", Status: "PUBLISHED"})
	dc.items["evt:pub:"+id.String()] = forged
	second, err := f.svc.GetPublicEvent(id)
	if err != nil || second.Title != "Forged" {
		t.Fatalf("hit: %+v %v", second, err)
	}
	if dc.sets != 1 {
		t.Fatalf("hit must not repopulate: sets=%d", dc.sets)
	}
}

func TestPublicEventCachePurgeOnUpdate(t *testing.T) {
	f := newEventFixture(t)
	dc := newFakeDetailCache()
	f.svc.SetDetailCache(dc)
	pub := publishFixture(t, f, "Before")
	id := mustParseUUID(t, pub.ID)

	if _, err := f.svc.GetPublicEvent(id); err != nil {
		t.Fatalf("prime: %v", err)
	}
	if !dc.has(id) {
		t.Fatalf("prime must populate")
	}
	newTitle := "After"
	if _, err := f.svc.UpdateEvent(f.user, "acme", pub.Slug, service.UpdateInput{Title: &newTitle}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if dc.has(id) {
		t.Fatalf("update must purge")
	}
	got, err := f.svc.GetPublicEvent(id)
	if err != nil || got.Title != "After" {
		t.Fatalf("post-purge: %+v %v", got, err)
	}
}

func TestPublicEventCachePurgeOnLifecycle(t *testing.T) {
	f := newEventFixture(t)
	dc := newFakeDetailCache()
	f.svc.SetDetailCache(dc)
	pub := publishFixture(t, f, "Lifecycle")
	id := mustParseUUID(t, pub.ID)

	if _, err := f.svc.GetPublicEvent(id); err != nil {
		t.Fatalf("prime: %v", err)
	}
	// Unpublish hides the event: entry purged, public read 404s.
	if _, err := f.svc.Unpublish(f.user, "acme", pub.Slug); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	if dc.has(id) {
		t.Fatalf("unpublish must purge")
	}
	if _, err := f.svc.GetPublicEvent(id); !errors.Is(err, service.ErrEventNotFound) {
		t.Fatalf("unpublished must 404, got %v", err)
	}
	if dc.has(id) {
		t.Fatalf("draft must never populate")
	}
	// Republish restores the public read.
	if _, err := f.svc.Publish(f.user, "acme", pub.Slug); err != nil {
		t.Fatalf("republish: %v", err)
	}
	if _, err := f.svc.GetPublicEvent(id); err != nil {
		t.Fatalf("republished: %v", err)
	}
}

func TestPublicEventCacheFailOpen(t *testing.T) {
	f := newEventFixture(t)
	dc := newFakeDetailCache()
	dc.failGet, dc.failSet, dc.failDel = true, true, true
	f.svc.SetDetailCache(dc)
	pub := publishFixture(t, f, "Resilient")
	id := mustParseUUID(t, pub.ID)

	// Broken cache: reads still serve from Postgres.
	got, err := f.svc.GetPublicEvent(id)
	if err != nil || got.Title != "Resilient" {
		t.Fatalf("fail-open read: %+v %v", got, err)
	}
	// Broken purge: writes still succeed.
	newTitle := "Resilient 2"
	upd, err := f.svc.UpdateEvent(f.user, "acme", pub.Slug, service.UpdateInput{Title: &newTitle})
	if err != nil || upd.Title != "Resilient 2" {
		t.Fatalf("fail-open write: %+v %v", upd, err)
	}
}

func TestPublicEventDraftNeverCached(t *testing.T) {
	f := newEventFixture(t)
	dc := newFakeDetailCache()
	f.svc.SetDetailCache(dc)
	d, err := f.svc.CreateEvent(f.user, "acme", service.CreateInput{Title: "Secret"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.svc.GetPublicEvent(mustParseUUID(t, d.ID)); !errors.Is(err, service.ErrEventNotFound) {
		t.Fatalf("draft must 404, got %v", err)
	}
	if dc.sets != 0 || len(dc.items) != 0 {
		t.Fatalf("draft must never populate: sets=%d items=%d", dc.sets, len(dc.items))
	}
}
