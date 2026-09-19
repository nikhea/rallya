package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	auth "github.com/nikhea/rallya/internal/auth"
	"github.com/nikhea/rallya/internal/auth/dto"
	"github.com/nikhea/rallya/internal/auth/handler"
	"github.com/nikhea/rallya/internal/auth/model"
	"github.com/nikhea/rallya/internal/auth/repository"
	"github.com/nikhea/rallya/internal/auth/testutil"
	"github.com/nikhea/rallya/internal/auth/token"
)

func init() { gin.SetMode(gin.TestMode) }

type fixture struct {
	router *gin.Engine
	repo   *repository.AuthRepository
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	_, repo, svc := testutil.Setup(t)
	h := handler.NewHandler(svc)
	r := gin.New()
	auth.RegisterRoutes(r.Group("/api/v1/auth"), h, repo)
	return fixture{router: r, repo: repo}
}

func doRequest(t *testing.T, f fixture, method, path, body, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func TestHTTPRegisterLoginMeFlow(t *testing.T) {
	f := newFixture(t)

	// Register -> 201.
	w := doRequest(t, f, "POST", "/api/v1/auth/register",
		`{"email":"jane@test.com","password":"Str0ngP@ssw0rd!","firstName":"Jane","lastName":"Doe"}`, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: got %d (%s)", w.Code, w.Body.String())
	}
	// Duplicate -> 409.
	w = doRequest(t, f, "POST", "/api/v1/auth/register",
		`{"email":"jane@test.com","password":"Str0ngP@ssw0rd!"}`, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate: got %d (%s)", w.Code, w.Body.String())
	}
	// Bad payload -> 400.
	w = doRequest(t, f, "POST", "/api/v1/auth/register",
		`{"email":"not-an-email","password":"short"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid payload: got %d (%s)", w.Code, w.Body.String())
	}

	// Login before verification -> 403 EMAIL_NOT_VERIFIED.
	w = doRequest(t, f, "POST", "/api/v1/auth/login",
		`{"email":"jane@test.com","password":"Str0ngP@ssw0rd!"}`, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("unverified login: got %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "EMAIL_NOT_VERIFIED") {
		t.Fatalf("missing EMAIL_NOT_VERIFIED code: %s", w.Body.String())
	}

	// Verify via seeded known-token row, then login -> 200.
	u, err := f.repo.GetUserByEmail("jane@test.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	rawVerify := "http-verify-token-001"
	if err := f.repo.CreateEmailVerification(nil, &model.EmailVerification{
		UserID:    u.ID,
		TokenHash: token.HashToken(rawVerify),
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed verification: %v", err)
	}
	w = doRequest(t, f, "POST", "/api/v1/auth/verify-email",
		`{"token":"`+rawVerify+`"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("verify: got %d (%s)", w.Code, w.Body.String())
	}

	w = doRequest(t, f, "POST", "/api/v1/auth/login",
		`{"email":"jane@test.com","password":"Str0ngP@ssw0rd!"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("login: got %d (%s)", w.Code, w.Body.String())
	}
	var pair dto.TokenPair
	if err := json.Unmarshal(w.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode pair: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected token pair")
	}
	bearer := "Bearer " + pair.AccessToken

	// Me with token -> 200 with profile.
	w = doRequest(t, f, "GET", "/api/v1/auth/me", "", bearer)
	if w.Code != http.StatusOK {
		t.Fatalf("me: got %d (%s)", w.Code, w.Body.String())
	}
	var me dto.Me
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.Email != "jane@test.com" || !me.EmailVerified {
		t.Fatalf("unexpected me: %+v", me)
	}
	if me.Profile.FirstName == nil || *me.Profile.FirstName != "Jane" {
		t.Fatalf("expected profile name, got %+v", me.Profile)
	}

	// Me without token -> 401; garbage token -> 401.
	if w := doRequest(t, f, "GET", "/api/v1/auth/me", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous me: got %d", w.Code)
	}
	if w := doRequest(t, f, "GET", "/api/v1/auth/me", "", "Bearer garbage"); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad token me: got %d", w.Code)
	}

	// Refresh -> 200 with rotated pair.
	w = doRequest(t, f, "POST", "/api/v1/auth/refresh",
		`{"refreshToken":"`+pair.RefreshToken+`"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("refresh: got %d (%s)", w.Code, w.Body.String())
	}
	var pair2 dto.TokenPair
	if err := json.Unmarshal(w.Body.Bytes(), &pair2); err != nil {
		t.Fatalf("decode pair2: %v", err)
	}

	// Logout with rotated access token -> 200, then me -> 401.
	w = doRequest(t, f, "POST", "/api/v1/auth/logout", "", "Bearer "+pair2.AccessToken)
	if w.Code != http.StatusOK {
		t.Fatalf("logout: got %d (%s)", w.Code, w.Body.String())
	}
	if w := doRequest(t, f, "GET", "/api/v1/auth/me", "", "Bearer "+pair2.AccessToken); w.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: got %d", w.Code)
	}
}

func TestHTTPPasswordResetFlow(t *testing.T) {
	f := newFixture(t)

	w := doRequest(t, f, "POST", "/api/v1/auth/register",
		`{"email":"reset@test.com","password":"Str0ngP@ssw0rd!"}`, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: got %d (%s)", w.Code, w.Body.String())
	}

	// Forgot always 200, even for unknown emails.
	w = doRequest(t, f, "POST", "/api/v1/auth/forgot-password",
		`{"email":"ghost@test.com"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("forgot unknown: got %d", w.Code)
	}
	w = doRequest(t, f, "POST", "/api/v1/auth/forgot-password",
		`{"email":"reset@test.com"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("forgot: got %d (%s)", w.Code, w.Body.String())
	}

	// Reset with seeded known-token row.
	u, err := f.repo.GetUserByEmail("reset@test.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	rawReset := "http-reset-token-001"
	if err := f.repo.CreatePasswordReset(nil, &model.PasswordReset{
		UserID:    u.ID,
		TokenHash: token.HashToken(rawReset),
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed reset: %v", err)
	}
	w = doRequest(t, f, "POST", "/api/v1/auth/reset-password",
		`{"token":"`+rawReset+`","newPassword":"N3wStr0ngP@ss!"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("reset: got %d (%s)", w.Code, w.Body.String())
	}
	// Reuse -> 409.
	w = doRequest(t, f, "POST", "/api/v1/auth/reset-password",
		`{"token":"`+rawReset+`","newPassword":"Another1!x"}`, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("reset reuse: got %d (%s)", w.Code, w.Body.String())
	}
	// Bad token -> 400.
	w = doRequest(t, f, "POST", "/api/v1/auth/reset-password",
		`{"token":"bogus","newPassword":"Another1!x"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("reset bogus: got %d (%s)", w.Code, w.Body.String())
	}
}

func TestHTTPVerifyCodeFlow(t *testing.T) {
	f := newFixture(t)

	w := doRequest(t, f, "POST", "/api/v1/auth/register",
		`{"email":"otp@test.com","password":"Str0ngP@ssw0rd!","firstName":"Otp"}`, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: got %d (%s)", w.Code, w.Body.String())
	}

	// Bad payload -> 400.
	w = doRequest(t, f, "POST", "/api/v1/auth/verify-code",
		`{"email":"otp@test.com","code":"12"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("short code: got %d", w.Code)
	}

	u, err := f.repo.GetUserByEmail("otp@test.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	const code = "482910"
	if err := f.repo.CreateOTPCode(nil, &model.EmailOTPCode{
		UserID:    u.ID,
		CodeHash:  token.HashToken(code),
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed otp: %v", err)
	}

	// Wrong code -> 400.
	w = doRequest(t, f, "POST", "/api/v1/auth/verify-code",
		`{"email":"otp@test.com","code":"000000"}`, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("wrong code: got %d (%s)", w.Code, w.Body.String())
	}

	// Right code -> 200, then login works.
	w = doRequest(t, f, "POST", "/api/v1/auth/verify-code",
		`{"email":"otp@test.com","code":"`+code+`"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("verify: got %d (%s)", w.Code, w.Body.String())
	}
	w = doRequest(t, f, "POST", "/api/v1/auth/login",
		`{"email":"otp@test.com","password":"Str0ngP@ssw0rd!"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("login: got %d (%s)", w.Code, w.Body.String())
	}
}
