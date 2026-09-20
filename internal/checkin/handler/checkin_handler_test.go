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

	attendeemodel "github.com/nikhea/rallya/internal/attendee/model"
	"github.com/nikhea/rallya/internal/attendee/repository"
	attendeeservice "github.com/nikhea/rallya/internal/attendee/service"
	attendeeutils "github.com/nikhea/rallya/internal/attendee/utils"
	authdto "github.com/nikhea/rallya/internal/auth/dto"
	authmodel "github.com/nikhea/rallya/internal/auth/model"
	authrepo "github.com/nikhea/rallya/internal/auth/repository"
	authservice "github.com/nikhea/rallya/internal/auth/service"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/auth/token"
	checkin "github.com/nikhea/rallya/internal/checkin"
	checkindto "github.com/nikhea/rallya/internal/checkin/dto"
	"github.com/nikhea/rallya/internal/checkin/handler"
	checkinmodel "github.com/nikhea/rallya/internal/checkin/model"
	checkinrepo "github.com/nikhea/rallya/internal/checkin/repository"
	checkinservice "github.com/nikhea/rallya/internal/checkin/service"
	"github.com/nikhea/rallya/internal/event/cover"
	eventmodel "github.com/nikhea/rallya/internal/event/model"
	eventrepository "github.com/nikhea/rallya/internal/event/repository"
	eventservice "github.com/nikhea/rallya/internal/event/service"
	"github.com/nikhea/rallya/internal/iam"
	"github.com/nikhea/rallya/internal/notification/jobs"
	orgmodel "github.com/nikhea/rallya/internal/organization/model"
	orgrepository "github.com/nikhea/rallya/internal/organization/repository"
	orgservice "github.com/nikhea/rallya/internal/organization/service"
)

func init() { gin.SetMode(gin.TestMode) }

var doorSecret = []byte("door-http-test-secret-32b-padded!")

type doorFixture struct {
	router  *gin.Engine
	tokens  map[string]string
	qrCode  string
	attID   string
	eventID string
}

func newDoorFixture(t *testing.T) *doorFixture {
	t.Helper()
	db, authRepo, authSvc := testutil.Setup(t)
	if err := db.AutoMigrate(orgmodel.AllModels()...); err != nil {
		t.Fatalf("migrate org: %v", err)
	}
	if err := db.AutoMigrate(eventmodel.AllModels()...); err != nil {
		t.Fatalf("migrate events: %v", err)
	}
	if err := db.AutoMigrate(attendeemodel.AllModels()...); err != nil {
		t.Fatalf("migrate attendees: %v", err)
	}
	if err := db.AutoMigrate(checkinmodel.AllModels()...); err != nil {
		t.Fatalf("migrate checkin: %v", err)
	}
	if err := db.AutoMigrate(&gormadapter.CasbinRule{}); err != nil {
		t.Fatalf("migrate casbin: %v", err)
	}
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

	checkRepo := checkinrepo.NewCheckinRepository(db)
	checkSvc := checkinservice.NewCheckinService(db, checkRepo, attSvc, eventSvc)
	checkSvc.SetQRSecret(doorSecret)

	r := gin.New()
	checkin.RegisterRoutes(r.Group("/api/v1"), handler.NewHandler(checkSvc), authRepo, orgRepo, e)

	f := &doorFixture{router: r, tokens: map[string]string{}}
	owner := mkDoorUser(t, authRepo, authSvc, "downer@test.com")
	mkDoorUser(t, authRepo, authSvc, "dmember@test.com")
	mkDoorUser(t, authRepo, authSvc, "dstranger@test.com")
	for _, email := range []string{"downer@test.com", "dmember@test.com", "dstranger@test.com"} {
		pair, err := authSvc.Login(authdto.Login{Email: email, Password: "Str0ngP@ssw0rd!"}, authdto.LoginContext{})
		if err != nil {
			t.Fatalf("login %s: %v", email, err)
		}
		f.tokens[email] = pair.AccessToken
	}
	if _, err := orgSvc.CreateOrg(owner, "Door", "door", ""); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := orgSvc.AddMember(owner, "door", "dmember@test.com", orgmodel.MemberRoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ev, err := eventSvc.CreateEvent(owner, "door", eventservice.CreateInput{Title: "Gala"})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	minted, err := attSvc.MintForOrder(db, uuid.New(), owner, uuid.MustParse(ev.ID), "fan@test.com", "Fan", 1)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	f.qrCode = attendeeutils.BuildPayload(minted[0].ID, minted[0].QRToken, doorSecret)
	f.attID = minted[0].ID.String()
	f.eventID = ev.ID
	return f
}

func mkDoorUser(t *testing.T, authRepo *authrepo.AuthRepository, authSvc *authservice.AuthService, email string) uuid.UUID {
	t.Helper()
	name := "D"
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

func doorReq(t *testing.T, f *doorFixture, email, method, path, body string) (int, []byte) {
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

func TestHTTPDoorScan(t *testing.T) {
	f := newDoorFixture(t)
	scan := "/api/v1/orgs/door/events/gala/checkin"

	code, body := doorReq(t, f, "downer@test.com", "POST", scan, `{"code":`+qstr(f.qrCode)+`}`)
	if code != http.StatusOK {
		t.Fatalf("scan: want 200, got %d (%s)", code, body)
	}
	var r checkindto.ScanResult
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r.Outcome != string(checkinmodel.OutcomeCheckedIn) {
		t.Fatalf("want CHECKED_IN, got %s", r.Outcome)
	}

	// Rescan reports ALREADY_CHECKED_IN.
	code, body = doorReq(t, f, "downer@test.com", "POST", "/api/v1/orgs/door/events/gala/checkin", `{"code":`+qstr(f.qrCode)+`}`)
	var r2 checkindto.ScanResult
	_ = json.Unmarshal(body, &r2)
	if code != http.StatusOK || r2.Outcome != string(checkinmodel.OutcomeAlreadyCheckedIn) {
		t.Fatalf("rescan: want 200 ALREADY_CHECKED_IN, got %d %s", code, r2.Outcome)
	}

	// Forged code is a 200 INVALID_CODE, not a 4xx.
	code, body = doorReq(t, f, "downer@test.com", "POST", "/api/v1/orgs/door/events/gala/checkin", `{"code":"junk"}`)
	var r3 checkindto.ScanResult
	_ = json.Unmarshal(body, &r3)
	if code != http.StatusOK || r3.Outcome != string(checkinmodel.OutcomeInvalidCode) {
		t.Fatalf("forged: want 200 INVALID_CODE, got %d %s", code, r3.Outcome)
	}

	// MEMBER cannot scan (403); anonymous cannot (401).
	code, _ = doorReq(t, f, "dmember@test.com", "POST", "/api/v1/orgs/door/events/gala/checkin", `{"code":"junk"}`)
	if code != http.StatusForbidden {
		t.Fatalf("member scan: want 403, got %d", code)
	}
	code, _ = doorReq(t, f, "", "POST", "/api/v1/orgs/door/events/gala/checkin", `{"code":"junk"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("anon scan: want 401, got %d", code)
	}

	// Stranger's org is invisible (stealth 404).
	code, _ = doorReq(t, f, "dstranger@test.com", "POST", "/api/v1/orgs/door/events/gala/checkin", `{"code":"junk"}`)
	if code != http.StatusNotFound {
		t.Fatalf("stranger: want 404, got %d", code)
	}
}

func TestHTTPDoorBatchManualStats(t *testing.T) {
	f := newDoorFixture(t)
	base := "/api/v1/orgs/door/events/gala/checkin"

	// Batch: one fresh, one junk.
	code, body := doorReq(t, f, "downer@test.com", "POST", base+"/batch",
		`{"codes":[`+qstr(f.qrCode)+`,"junk"]}`)
	if code != http.StatusOK {
		t.Fatalf("batch: want 200, got %d (%s)", code, body)
	}
	var batch checkindto.BatchResult
	if err := json.Unmarshal(body, &batch); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(batch.Results) != 2 ||
		batch.Results[0].Outcome != string(checkinmodel.OutcomeCheckedIn) ||
		batch.Results[1].Outcome != string(checkinmodel.OutcomeInvalidCode) {
		t.Fatalf("bad batch envelope: %+v", batch)
	}

	// Over the cap.
	big := `{"codes":[` + strings.TrimRight(strings.Repeat(`"junk",`, checkinservice.MaxBatchSize+1), ",") + `]}`
	code, _ = doorReq(t, f, "downer@test.com", "POST", base+"/batch", big)
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize batch: want 413, got %d", code)
	}

	// Stats reflect the door so far.
	code, body = doorReq(t, f, "downer@test.com", "GET", base+"/stats", "")
	var stats checkindto.StatsResponse
	_ = json.Unmarshal(body, &stats)
	if code != http.StatusOK || stats.CheckedIn != 1 || stats.Total != 1 {
		t.Fatalf("stats: want 200 1/1, got %d %+v", code, stats)
	}

	// Manual fallback by roster ID (fresh row needed — rescan would ALREADY).
	code, body = doorReq(t, f, "downer@test.com", "POST", base, `{"attendeeId":"`+f.attID+`"}`)
	var manual checkindto.ScanResult
	_ = json.Unmarshal(body, &manual)
	if code != http.StatusOK || manual.Outcome != string(checkinmodel.OutcomeAlreadyCheckedIn) ||
		manual.Method != string(checkinmodel.MethodManual) {
		t.Fatalf("manual rescan: want 200 ALREADY manual, got %d %+v", code, manual)
	}
}

func qstr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
