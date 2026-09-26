package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	attendeemodel "github.com/nikhea/rallya/internal/attendee/model"
	"github.com/nikhea/rallya/internal/attendee/repository"
	attendeeservice "github.com/nikhea/rallya/internal/attendee/service"
	auditmodel "github.com/nikhea/rallya/internal/audit/model"
	auditrepository "github.com/nikhea/rallya/internal/audit/repository"
	auditservice "github.com/nikhea/rallya/internal/audit/service"
	authdto "github.com/nikhea/rallya/internal/auth/dto"
	authmodel "github.com/nikhea/rallya/internal/auth/model"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	authservice "github.com/nikhea/rallya/internal/auth/service"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/auth/token"
	"github.com/nikhea/rallya/internal/event/cover"
	eventmodel "github.com/nikhea/rallya/internal/event/model"
	eventrepository "github.com/nikhea/rallya/internal/event/repository"
	eventservice "github.com/nikhea/rallya/internal/event/service"
	"github.com/nikhea/rallya/internal/iam"
	kit "github.com/nikhea/rallya/internal/kit"
	kitdto "github.com/nikhea/rallya/internal/kit/dto"
	"github.com/nikhea/rallya/internal/kit/handler"
	kitmodel "github.com/nikhea/rallya/internal/kit/model"
	kitrepo "github.com/nikhea/rallya/internal/kit/repository"
	kitservice "github.com/nikhea/rallya/internal/kit/service"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
	submodel "github.com/nikhea/rallya/internal/subscription/model"
)

func init() { gin.SetMode(gin.TestMode) }

type kitFullFixture struct {
	router  *gin.Engine
	tokens  map[string]string
	attID   string
	eventID string
	ownerID uuid.UUID
	db      *gorm.DB
	attRepo *repository.AttendeeRepository
	attSvc  *attendeeservice.AttendeeService
	kitSvc  *kitservice.KitService
}

func newKitFixture(t *testing.T) *kitFullFixture {
	t.Helper()
	db, authRepo, authSvc := testutil.Setup(t)
	for name, m := range map[string][]any{
		"org":      orgmodel.AllModels(),
		"events":   eventmodel.AllModels(),
		"attendee": attendeemodel.AllModels(),
		"kit":      kitmodel.AllModels(),
		"audit":    auditmodel.AllModels(),
	} {
		if err := db.AutoMigrate(m...); err != nil {
			t.Fatalf("migrate %s: %v", name, err)
		}
	}
	if err := db.AutoMigrate(&gormadapter.CasbinRule{}); err != nil {
		t.Fatalf("migrate casbin: %v", err)
	}
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uq_kit_collections_active ON kit_collections (kit_id, attendee_id) WHERE status <> 'VOIDED'")
	db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uq_kit_collections_idem ON kit_collections (kit_id, attendee_id, idempotency_key) WHERE idempotency_key <> ''")
	e, err := iam.NewEnforcer(db)
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}

	orgRepo := orgrepository.NewOrgRepository(db)
	orgSvc := orgservice.NewOrgService(orgRepo, authSvc)
	orgSvc.SetGroupSyncer(iam.NewMembershipSyncer(e))
	orgSvc.SetPolicySeeder(iam.NewOrgPolicySeeder(e))

	eventRepo := eventrepository.NewEventRepository(db)
	eventSvc := eventservice.NewEventService(eventRepo, orgSvc)
	eventSvc.SetEnqueuer(&jobs.FakeEnqueuer{})
	eventSvc.SetCoverStorage(cover.NewLocal(t.TempDir()))

	attRepo := repository.NewAttendeeRepository(db)
	attSvc := attendeeservice.NewAttendeeService(attRepo, authSvc, eventSvc, orgSvc)

	auditSvc := auditservice.NewAuditService(auditrepository.NewAuditRepository(db))

	kitSvc := kitservice.NewKitService(db, kitrepo.NewKitRepository(db), attSvc, eventSvc)
	kitSvc.SetAuditEmitter(auditSvc)
	kitSvc.SetEntitlementProvider(proKitPlans{})

	r := gin.New()
	kit.RegisterRoutes(r.Group("/api/v1"), handler.NewHandler(kitSvc), authRepo, orgRepo, e)

	f := &kitFullFixture{router: r, tokens: map[string]string{}, db: db, attRepo: attRepo, attSvc: attSvc, kitSvc: kitSvc}
	owner := mkKitUser(t, authRepo, authSvc, "kowner@test.com")
	mkKitUser(t, authRepo, authSvc, "kmember@test.com")
	mkKitUser(t, authRepo, authSvc, "kstranger@test.com")
	for _, email := range []string{"kowner@test.com", "kmember@test.com", "kstranger@test.com"} {
		pair, err := authSvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authdto.LoginContext{})
		if err != nil {
			t.Fatalf("login %s: %v", email, err)
		}
		f.tokens[email] = pair.AccessToken
	}
	if _, err := orgSvc.CreateOrg(owner, "Kit", "kitorg", ""); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := orgSvc.AddMember(owner, "kitorg", "kmember@test.com", orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ev, err := eventSvc.CreateEvent(owner, "kitorg", eventservice.CreateInput{Title: "Kit Fest"})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	minted, err := attSvc.MintForOrder(db, uuid.New(), owner, uuid.MustParse(ev.ID), "fan@test.com", "Fan", 1)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	// Door-ready: flip straight to CHECKED_IN through the attendee seam.
	f.attID = flipToCheckedIn(t, f, minted[0].ID.String())
	f.eventID = ev.ID
	f.ownerID = owner
	return f
}

func mkKitUser(t *testing.T, authRepo *authrepo.AuthRepository, authSvc *authservice.AuthService, email string) uuid.UUID {
	t.Helper()
	name := "K"
	if err := authSvc.Register(authdto.Register{Email: email, Password: "Str0ngP@ssw0rd!", FirstName: &name}); err != nil {
		t.Fatalf("register: %v", err)
	}
	u, err := authRepo.GetUserByEmail(email)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	raw := "verify-" + email
	if err := authRepo.CreateEmailVerification(nil, &authmodel.EmailVerification{
		UserID: u.ID, TokenHash: token.HashToken(raw), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := authSvc.VerifyEmail(raw); err != nil {
		t.Fatalf("verify: %v", err)
	}
	return u.ID
}

func kitReq(t *testing.T, f *kitFullFixture, email, method, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if email != "" {
		req.Header.Set("Authorization", "Bearer "+f.tokens[email])
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestHTTPKitLifecycle(t *testing.T) {
	f := newKitFixture(t)
	base := "/api/v1/orgs/kitorg/events/kit-fest/kits"

	// Define.
	code, body := kitReq(t, f, "kowner@test.com", "POST", base, `{"name":"VIP pack","quantityTotal":2}`)
	if code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", code, body)
	}
	var k kitdto.KitResponse
	if err := json.Unmarshal(body, &k); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if k.Remaining != 2 {
		t.Fatalf("fresh remaining = %d, want 2", k.Remaining)
	}

	// Bad quantity.
	code, _ = kitReq(t, f, "kowner@test.com", "POST", base, `{"name":"Zero","quantityTotal":0}`)
	if code != http.StatusBadRequest {
		t.Fatalf("zero qty: want 400, got %d", code)
	}

	// List carries tallies.
	code, body = kitReq(t, f, "kowner@test.com", "GET", base, "")
	var listed []kitdto.KitResponse
	_ = json.Unmarshal(body, &listed)
	if code != http.StatusOK || len(listed) != 1 {
		t.Fatalf("list: want 200x1, got %d (%s)", code, body)
	}

	// Collect immediately.
	code, body = kitReq(t, f, "kowner@test.com", "POST", base+"/"+k.ID+"/collect", `{"attendeeId":"`+f.attID+`"}`)
	var c kitdto.CollectionResponse
	_ = json.Unmarshal(body, &c)
	if code != http.StatusCreated || c.Status != "COLLECTED" || c.CollectedAt == nil {
		t.Fatalf("collect: want 201 COLLECTED stamped, got %d (%s)", code, body)
	}

	// Reserve + pickup on a second attendee.
	minted := mintCheckedIn(t, f)
	code, body = kitReq(t, f, "kowner@test.com", "POST", base+"/"+k.ID+"/collect", `{"attendeeId":"`+minted+`","reserve":true}`)
	var r kitdto.CollectionResponse
	_ = json.Unmarshal(body, &r)
	if code != http.StatusCreated || r.Status != "PENDING" {
		t.Fatalf("reserve: want 201 PENDING, got %d (%s)", code, body)
	}
	code, body = kitReq(t, f, "kowner@test.com", "POST",
		"/api/v1/orgs/kitorg/events/kit-fest/kit-collections/"+r.ID+"/collect", "")
	var m kitdto.CollectionResponse
	_ = json.Unmarshal(body, &m)
	if code != http.StatusOK || m.Status != "COLLECTED" {
		t.Fatalf("pickup: want 200 COLLECTED, got %d (%s)", code, body)
	}

	// Stock exhausted now (2/2): third attendee gets 409.
	third := mintCheckedIn(t, f)
	code, _ = kitReq(t, f, "kowner@test.com", "POST", base+"/"+k.ID+"/collect", `{"attendeeId":"`+third+`"}`)
	if code != http.StatusConflict {
		t.Fatalf("exhausted: want 409, got %d", code)
	}

	// Filtered reads.
	code, body = kitReq(t, f, "kowner@test.com", "GET",
		"/api/v1/orgs/kitorg/events/kit-fest/kit-collections?status=COLLECTED", "")
	var list kitdto.CollectionListResponse
	_ = json.Unmarshal(body, &list)
	if code != http.StatusOK || list.Total != 2 {
		t.Fatalf("filter: want 200 total=2, got %d (%s)", code, body)
	}
	code, body = kitReq(t, f, "kowner@test.com", "GET",
		"/api/v1/orgs/kitorg/events/kit-fest/kit-collections?attendeeId="+f.attID, "")
	var mine kitdto.CollectionListResponse
	_ = json.Unmarshal(body, &mine)
	if code != http.StatusOK || mine.Total != 1 || mine.Items[0].KitName != "VIP pack" {
		t.Fatalf("attendee filter: want 1 named row, got %d (%s)", code, body)
	}

	// Void frees stock: collect the third attendee now.
	code, _ = kitReq(t, f, "kowner@test.com", "POST",
		"/api/v1/orgs/kitorg/events/kit-fest/kit-collections/"+c.ID+"/void", "")
	if code != http.StatusOK {
		t.Fatalf("void: want 200, got %d", code)
	}
	code, body = kitReq(t, f, "kowner@test.com", "POST", base+"/"+k.ID+"/collect", `{"attendeeId":"`+third+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("re-issue after void: want 201, got %d (%s)", code, body)
	}

	// Audit trail landed (create + collects + reserve + pickup + void).
	var n int64
	if err := f.db.Model(&auditmodel.AuditEvent{}).Count(&n).Error; err != nil {
		t.Fatalf("audit count: %v", err)
	}
	if n < 5 {
		t.Fatalf("want >=5 audit entries, got %d", n)
	}
}

func TestHTTPKitGates(t *testing.T) {
	f := newKitFixture(t)
	base := "/api/v1/orgs/kitorg/events/kit-fest/kits"

	// MEMBER has no kit grants (403); anonymous is 401.
	code, _ := kitReq(t, f, "kmember@test.com", "GET", base, "")
	if code != http.StatusForbidden {
		t.Fatalf("member read: want 403, got %d", code)
	}
	code, _ = kitReq(t, f, "", "GET", base, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("anon: want 401, got %d", code)
	}
	// Stranger's org is invisible (stealth 404).
	code, _ = kitReq(t, f, "kstranger@test.com", "GET", base, "")
	if code != http.StatusNotFound {
		t.Fatalf("stranger: want 404, got %d", code)
	}

	// Unchecked-in attendee cannot collect (422).
	raw := mintRegistered(t, f)
	_, body := kitReq(t, f, "kowner@test.com", "POST", base, `{"name":"Shirt","quantityTotal":5}`)
	var k kitdto.KitResponse
	_ = json.Unmarshal(body, &k)
	code, _ = kitReq(t, f, "kowner@test.com", "POST", base+"/"+k.ID+"/collect", `{"attendeeId":"`+raw+`"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("unckecked collect: want 422, got %d", code)
	}
}

// proKitPlans grants Pro everywhere (kit HTTP suites predate plans).
type proKitPlans struct{}

func (proKitPlans) EntitlementFor(_ uuid.UUID) (submodel.Entitlement, error) {
	return submodel.EntitlementForPlan(submodel.PlanPro), nil
}

// flipToCheckedIn flips one attendee row straight to CHECKED_IN.
func flipToCheckedIn(t *testing.T, f *kitFullFixture, id string) string {
	t.Helper()
	a, err := f.attRepo.GetAttendeeByID(uuid.MustParse(id))
	if err != nil {
		t.Fatalf("load attendee: %v", err)
	}
	a.Status = attendeemodel.AttendeeStatusCheckedIn
	now := time.Now().UTC()
	a.CheckedInAt = &now
	if err := f.attRepo.UpdateAttendee(nil, a); err != nil {
		t.Fatalf("flip: %v", err)
	}
	return id
}

// mintCheckedIn mints a REGISTERED row and flips it CHECKED_IN.
func mintCheckedIn(t *testing.T, f *kitFullFixture) string {
	t.Helper()
	minted, err := f.attSvc.MintForOrder(f.db, uuid.New(), f.ownerID, uuid.MustParse(f.eventID), "fan2@test.com", "Fan", 1)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return flipToCheckedIn(t, f, minted[0].ID.String())
}

func mintRegistered(t *testing.T, f *kitFullFixture) string {
	t.Helper()
	minted, err := f.attSvc.MintForOrder(f.db, uuid.New(), f.ownerID, uuid.MustParse(f.eventID), "fan3@test.com", "Fan", 1)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return minted[0].ID.String()
}
