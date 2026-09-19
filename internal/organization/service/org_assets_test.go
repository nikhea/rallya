package service_test

import (
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nikhea/rallya/internal/event/cover"
	eventmodel "github.com/nikhea/rallya/internal/event/model"
	"github.com/nikhea/rallya/internal/event/repository"
	eventservice "github.com/nikhea/rallya/internal/event/service"
)

type stubStorage struct {
	saved   map[string][]byte
	deleted []string
}

func (s *stubStorage) Save(orgID, eventID uuid.UUID, data io.Reader, ext string) (string, error) {
	st, err := s.SaveImage(orgID, eventID, data, ext)
	if err != nil {
		return "", err
	}
	return st.URL, nil
}

func (s *stubStorage) SaveImage(orgID, eventID uuid.UUID, data io.Reader, ext string) (*cover.StoredImage, error) {
	b, _ := io.ReadAll(data)
	url := "mem://" + orgID.String() + "/" + eventID.String()[:8] + ext
	s.saved[url] = b
	return &cover.StoredImage{URL: url, PublicID: url, Format: "png", Bytes: len(b)}, nil
}

func (s *stubStorage) Delete(url string) error {
	s.deleted = append(s.deleted, url)
	delete(s.saved, url)
	return nil
}

func TestDeleteOrgCleansEventAssets(t *testing.T) {
	f := newOrgFixture(t)
	if err := f.db.AutoMigrate(eventmodel.AllModels()...); err != nil {
		t.Fatalf("migrate events: %v", err)
	}
	erepo := repository.NewEventRepository(f.db)
	esvc := eventservice.NewEventService(erepo, f.svc)
	st := &stubStorage{saved: map[string][]byte{}}
	esvc.SetCoverStorage(st)
	f.svc.SetAssetCleaner(esvc)

	d, err := f.svc.CreateOrg(f.owner.ID, "Acme", "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	ev, err := esvc.CreateEvent(f.owner.ID, d.Slug, eventservice.CreateInput{Title: "Show"})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := esvc.SetCover(d.Slug, ev.Slug, strings.NewReader("img"), 3, "image/png"); err != nil {
		t.Fatalf("set cover: %v", err)
	}
	if err := f.svc.DeleteOrg(f.owner.ID, d.Slug); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(st.deleted) != 1 {
		t.Fatalf("expected 1 asset cleanup, got %v", st.deleted)
	}
}
