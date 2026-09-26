package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	authmodel "github.com/nikhea/rallya/internal/auth/model"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	"github.com/nikhea/rallya/internal/organization/service"
	submodel "github.com/nikhea/rallya/internal/subscription/model"
	subservice "github.com/nikhea/rallya/internal/subscription/service"
)

// fakePlans is a scripted EntitlementProvider (unknown orgs resolve Free).
type fakePlans struct {
	byOrg map[uuid.UUID]submodel.Entitlement
}

func (f *fakePlans) EntitlementFor(orgID uuid.UUID) (submodel.Entitlement, error) {
	if ent, ok := f.byOrg[orgID]; ok {
		return ent, nil
	}
	return submodel.FreeEntitlement(), nil
}

// proPlans grants Pro everywhere (custom-role suites predate plans).
type proPlans struct{}

func (proPlans) EntitlementFor(_ uuid.UUID) (submodel.Entitlement, error) {
	return submodel.EntitlementForPlan(submodel.PlanPro), nil
}

// proEntitlements grants the Pro tier everywhere (custom-role suites predate plans).
func proEntitlements() proPlans { return proPlans{} }

func TestMemberQuotaFree(t *testing.T) {
	f := newOrgFixture(t)
	f.svc.SetEntitlementProvider(&fakePlans{})
	d, err := f.svc.CreateOrg(f.owner.ID, "Quota", "quota", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Free allows 5 members incl. owner: 4 more succeed, 6th trips 402.
	emails := []string{"a@t.com", "b@t.com", "c@t.com", "d@t.com", "e@t.com"}
	for i, email := range emails {
		f.users.Add(&authmodel.User{Email: email, EmailVerified: true, Status: authmodel.UserStatusActive})
		_, err := f.svc.AddMember(f.owner.ID, d.Slug, email, orgmodel.MemberRoleMember)
		if i < 4 && err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
		if i == 4 && err != subservice.ErrUpgradeRequired {
			t.Fatalf("6th member: want ErrUpgradeRequired, got %v", err)
		}
	}
}

func TestRoleFlagFree(t *testing.T) {
	f := newOrgFixture(t)
	f.svc.SetEntitlementProvider(&fakePlans{})
	d, err := f.svc.CreateOrg(f.owner.ID, "Quota", "quota", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = f.svc.DefineRole(f.owner.ID, d.Slug, "door", []service.RolePermission{{Object: "checkin", Action: "create"}})
	if err != subservice.ErrUpgradeRequired {
		t.Fatalf("free define: want ErrUpgradeRequired, got %v", err)
	}
}

// stubApiKeyStore satisfies the wiring assertion (plan gate trips first).
type stubApiKeyStore struct{}

func (stubApiKeyStore) CreateApiKey(_ *gorm.DB, _ *authmodel.ApiKey) error {
	return errors.New("stub")
}

func (stubApiKeyStore) GetApiKeyByID(_ uuid.UUID) (*authmodel.ApiKey, error) {
	return nil, errors.New("stub")
}

func (stubApiKeyStore) ListApiKeysByOrg(_ uuid.UUID, _, _ int) ([]authmodel.ApiKey, int64, error) {
	return nil, 0, errors.New("stub")
}

func (stubApiKeyStore) RevokeApiKey(_ *gorm.DB, _ uuid.UUID, _ time.Time) error {
	return errors.New("stub")
}

func TestApiKeyFlagFree(t *testing.T) {
	f := newOrgFixture(t)
	f.svc.SetEntitlementProvider(&fakePlans{})
	f.svc.SetApiKeyStore(stubApiKeyStore{})
	d, err := f.svc.CreateOrg(f.owner.ID, "Quota", "quota", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = f.svc.CreateApiKey(f.owner.ID, d.Slug, "tablet", []string{"checkin:create"}, nil)
	if err != subservice.ErrUpgradeRequired {
		t.Fatalf("free mint: want ErrUpgradeRequired, got %v", err)
	}
}
