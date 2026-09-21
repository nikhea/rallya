package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	admindto "github.com/nikhea/rallya/internal/admin/dto"
	"github.com/nikhea/rallya/internal/iam"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
)

// postAdmin is the POST twin of adminReq (repairs are POST).
func postAdmin(t *testing.T, f *adminFixture, email, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest("POST", path, nil)
	if email != "" {
		req.Header.Set("Authorization", "Bearer "+f.tokens[email])
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestHTTPAdminRepairs(t *testing.T) {
	f := newAdminFixture(t)
	root, owner := "root@test.com", "xowner@test.com"

	// Non-superadmin is forbidden on repairs.
	if code, _ := postAdmin(t, f, owner, "/api/v1/admin/orgs/"+f.orgID+"/policies/reseed"); code != http.StatusForbidden {
		t.Fatalf("owner reseed: want 403, got %d", code)
	}
	if code, _ := postAdmin(t, f, owner, "/api/v1/admin/users/"+f.userID+"/policies/sync"); code != http.StatusForbidden {
		t.Fatalf("owner sync: want 403, got %d", code)
	}

	// Unknown targets 404.
	if code, _ := postAdmin(t, f, root, "/api/v1/admin/orgs/"+uuid.New().String()+"/policies/reseed"); code != http.StatusNotFound {
		t.Fatalf("unknown org reseed: want 404, got %d", code)
	}
	if code, _ := postAdmin(t, f, root, "/api/v1/admin/users/"+uuid.New().String()+"/policies/sync"); code != http.StatusNotFound {
		t.Fatalf("unknown user sync: want 404, got %d", code)
	}

	// Healthy org reseeds clean (idempotent, nothing added).
	code, body := postAdmin(t, f, root, "/api/v1/admin/orgs/"+f.orgID+"/policies/reseed")
	var reseed admindto.PolicyDiff
	_ = json.Unmarshal(body, &reseed)
	if code != http.StatusOK || reseed.Added != 0 || reseed.Removed != 0 || reseed.Total != 30 {
		t.Fatalf("reseed healthy: want 0/0/30, got %d %+v (%s)", code, reseed, body)
	}

	// Break one policy row behind the service's back; reseed must heal it.
	orgID := uuid.MustParse(f.orgID)
	before := reseed.Total
	if err := iam.LockWrite(func() error {
		_, err := f.enf.RemovePolicy("ADMIN", orgID.String(), "audit", "read")
		return err
	}); err != nil {
		t.Fatalf("break policy: %v", err)
	}
	code, body = postAdmin(t, f, root, "/api/v1/admin/orgs/"+f.orgID+"/policies/reseed")
	_ = json.Unmarshal(body, &reseed)
	if code != http.StatusOK || reseed.Added != 1 || reseed.Total != before {
		t.Fatalf("reseed repair: want added=1 total=%d, got %d %+v (%s)", before, code, reseed, body)
	}

	// Sync converges a healthy user cleanly.
	userID := uuid.MustParse(f.userID)
	code, body = postAdmin(t, f, root, "/api/v1/admin/users/"+f.userID+"/policies/sync")
	var sync admindto.PolicyDiff
	_ = json.Unmarshal(body, &sync)
	if code != http.StatusOK || sync.Added != 0 || sync.Removed != 0 || sync.Total != 1 {
		t.Fatalf("sync healthy: want 0/0/1, got %d %+v (%s)", code, sync, body)
	}

	// Stale grouping for a foreign org: sync sweeps it.
	staleOrg := uuid.New()
	staleRole := orgmodel.MemberRoleMember
	if err := iam.NewMembershipSyncer(f.enf).SyncMembership(userID, staleOrg, &staleRole); err != nil {
		t.Fatalf("plant stale: %v", err)
	}
	code, body = postAdmin(t, f, root, "/api/v1/admin/users/"+f.userID+"/policies/sync")
	_ = json.Unmarshal(body, &sync)
	if code != http.StatusOK || sync.Removed != 1 || sync.Total != 1 {
		t.Fatalf("sync sweep: want removed=1 total=1, got %d %+v (%s)", code, sync, body)
	}

	// Both repairs audited.
	for _, action := range []string{"admin.policies_reseeded", "admin.policies_synced"} {
		if got := auditCount(t, f, action); got < 1 {
			t.Fatalf("audit %s: want >=1, got %d", action, got)
		}
	}
}
