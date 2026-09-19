package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/nikhea/rallya/internal/auth/dto"
	"github.com/nikhea/rallya/internal/auth/service"
)

// Handler adapts AuthService to Gin. No business logic here.
type Handler struct {
	svc *service.AuthService
}

// NewHandler builds the handler.
func NewHandler(svc *service.AuthService) *Handler { return &Handler{svc: svc} }

// Register creates a user + profile + credential and sends a verification email.
//
// @Summary		Register a new user
// @Description	Creates the identity, hashes the password with bcrypt, and emails a 24h verification link. Login is blocked until verified.
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			request	body		dto.Register	true	"Registration payload"
// @Success		201		{object}	dto.Message		"Verification email sent"
// @Failure		400		{object}	dto.ErrorResponse	"Validation error"
// @Failure		409		{object}	dto.ErrorResponse	"Email already registered"
// @Failure		500		{object}	dto.ErrorResponse	"Registration failed"
// @Router			/auth/register [post]
func (h *Handler) Register(c *gin.Context) {
	var in dto.Register
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.Register(in); err != nil {
		if errors.Is(err, service.ErrUserExists) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "registration failed"})
		return
	}
	c.JSON(http.StatusCreated, dto.Message{Message: "Verification email sent"})
}

// VerifyEmail confirms email ownership via the emailed token.
// Accepts the token in the JSON body or as ?token= for email link clicks.
//
// @Summary		Verify email address
// @Description	Confirms email ownership. Tokens are single-use and expire after 24h.
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			request	body		dto.VerifyEmail	false	"Verification payload"
// @Param			token	query		string				false	"Verification token (email link fallback)"	example(b7e2d1c0a9f84620c3d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708192a3)
// @Success		200		{object}	dto.Message		"Email verified"
// @Failure		400		{object}	dto.ErrorResponse	"Invalid or expired token"
// @Failure		409		{object}	dto.ErrorResponse	"Token already used"
// @Failure		500		{object}	dto.ErrorResponse	"Verification failed"
// @Router			/auth/verify-email [post]
func (h *Handler) VerifyEmail(c *gin.Context) {
	var in dto.VerifyEmail
	if err := c.ShouldBindJSON(&in); err != nil || in.Token == "" {
		in.Token = c.Query("token")
		if in.Token == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
			return
		}
	}
	if err := h.svc.VerifyEmail(in.Token); err != nil {
		switch {
		case errors.Is(err, service.ErrTokenUsed):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrInvalidToken):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "verification failed"})
		}
		return
	}
	c.JSON(http.StatusOK, dto.Message{Message: "Email verified"})
}

// VerifyByCode confirms email ownership via the 6-digit OTP.
// Wrong codes count against 5 attempts; expired/exhausted codes read as invalid.
//
// @Summary		Verify email with OTP code
// @Description	Confirms email ownership with the 6-digit code from the verification email. Codes expire after 10 minutes and allow 5 attempts.
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			request	body		dto.VerifyByCode	true	"OTP payload"
// @Success		200		{object}	dto.Message		"Email verified"
// @Failure		400		{object}	dto.ErrorResponse	"Invalid or expired code"
// @Failure		429		{object}	dto.ErrorResponse	"Too many wrong codes"
// @Failure		500		{object}	dto.ErrorResponse	"Verification failed"
// @Router			/auth/verify-code [post]
func (h *Handler) VerifyByCode(c *gin.Context) {
	var in dto.VerifyByCode
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.VerifyByCode(in.Email, in.Code); err != nil {
		switch {
		case errors.Is(err, service.ErrOTPAttemptsExceeded):
			c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrInvalidToken):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "verification failed"})
		}
		return
	}
	c.JSON(http.StatusOK, dto.Message{Message: "Email verified"})
}

// ResendVerification invalidates pending tokens and emails a fresh link.
// Throttled to one request per minute per user; unknown emails return 200.
//
// @Summary		Resend verification email
// @Description	Issues a fresh 24h verification token. Throttled (60s). Unknown emails still return 200 to avoid enumeration.
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			request	body		dto.ResendVerification	true	"Email payload"
// @Success		200		{object}	dto.Message			"Verification email sent"
// @Failure		400		{object}	dto.ErrorResponse	"Validation error"
// @Failure		429		{object}	dto.ErrorResponse	"Too many requests"
// @Failure		500		{object}	dto.ErrorResponse	"Resend failed"
// @Router			/auth/resend-verification [post]
func (h *Handler) ResendVerification(c *gin.Context) {
	var in dto.ResendVerification
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.ResendVerification(in.Email); err != nil {
		if errors.Is(err, service.ErrTooManyRequests) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "resend failed"})
		return
	}
	c.JSON(http.StatusOK, dto.Message{Message: "Verification email sent"})
}

// Login validates credentials and mints a session + token pair.
// Blocked with 403 EMAIL_NOT_VERIFIED until the email is verified.
//
// @Summary		Log in
// @Description	Verifies the bcrypt password, enforces ACTIVE status + verified email, then returns an access JWT and a rotating refresh token.
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			request		body		dto.Login		true	"Login payload"
// @Param			X-Device-Name	header		string			false	"Device label for the session"	example(Chrome Laptop)
// @Success		200			{object}	dto.TokenPair	"Access + refresh tokens"
// @Failure		400			{object}	dto.ErrorResponse
// @Failure		401			{object}	dto.ErrorResponse				"Invalid email or password"
// @Failure		403			{object}	dto.EmailNotVerifiedResponse	"Email not verified / account suspended"
// @Failure		500			{object}	dto.ErrorResponse				"Login failed"
// @Router			/auth/login [post]
func (h *Handler) Login(c *gin.Context) {
	var in dto.Login
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	pair, err := h.svc.Login(in, service.LoginContext{
		IPAddress:  c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
		DeviceName: c.GetHeader("X-Device-Name"),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrEmailNotVerified):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error(), "code": "EMAIL_NOT_VERIFIED"})
		case errors.Is(err, service.ErrAccountSuspended),
			errors.Is(err, service.ErrAccountDeleted),
			errors.Is(err, service.ErrAccountInactive):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		}
		return
	}
	c.JSON(http.StatusOK, pair)
}

// Refresh rotates a refresh token into a fresh access + refresh pair.
// Reuse of a revoked token revokes the whole session (theft signal).
//
// @Summary		Rotate refresh token
// @Description	Single-use rotation: the presented token is revoked and a new pair issued. Reuse triggers session-wide revocation.
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			request	body		dto.Refresh	true	"Refresh payload"
// @Success		200		{object}	dto.TokenPair
// @Failure		400		{object}	dto.ErrorResponse	"Validation error"
// @Failure		401		{object}	dto.ErrorResponse	"Invalid or expired refresh token"
// @Router			/auth/refresh [post]
func (h *Handler) Refresh(c *gin.Context) {
	var in dto.Refresh
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	pair, err := h.svc.Refresh(in.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired refresh token"})
		return
	}
	c.JSON(http.StatusOK, pair)
}

// Logout revokes the current session and its refresh tokens. Idempotent.
//
// @Summary		Log out current session
// @Description	Revokes the session bound to the access token plus its refresh rows.
// @Tags			auth
// @Produce		json
// @Security		BearerAuth
// @Param			Authorization	header		string		true	"Bearer access token"	example(Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxYTIzYiJ9.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c)
// @Success		200			{object}	dto.Message		"Logged out"
// @Failure		401			{object}	dto.ErrorResponse	"Unauthorized"
// @Failure		500			{object}	dto.ErrorResponse	"Logout failed"
// @Router			/auth/logout [post]
func (h *Handler) Logout(c *gin.Context) {
	sid, ok := SessionFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if err := h.svc.Logout(sid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "logout failed"})
		return
	}
	c.JSON(http.StatusOK, dto.Message{Message: "Logged out"})
}

// Me returns the authenticated user's identity + profile.
//
// @Summary		Get current user
// @Description	Returns id, email, verification flag, and profile names for the access-token owner.
// @Tags			auth
// @Produce		json
// @Security		BearerAuth
// @Param			Authorization	header		string	true	"Bearer access token"	example(Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxYTIzYiJ9.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c)
// @Success		200			{object}	dto.Me	"Current user"
// @Failure		401			{object}	dto.ErrorResponse	"Unauthorized"
// @Router			/auth/me [get]
func (h *Handler) Me(c *gin.Context) {
	uid, ok := UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	me, err := h.svc.Me(uid)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, me)
}

// ForgotPassword starts the reset flow. Always 200 to avoid enumeration.
//
// @Summary		Request password reset
// @Description	Invalidates prior reset tokens and emails a 1h single-use link if the account exists. Always returns 200.
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			request	body		dto.ForgotPassword	true	"Email payload"
// @Success		200		{object}	dto.Message		"If the email exists, a reset link was sent"
// @Failure		400		{object}	dto.ErrorResponse	"Validation error"
// @Router			/auth/forgot-password [post]
func (h *Handler) ForgotPassword(c *gin.Context) {
	var in dto.ForgotPassword
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	_ = h.svc.ForgotPassword(in.Email)
	c.JSON(http.StatusOK, dto.Message{Message: "If the email exists, a reset link was sent"})
}

// ResetPassword consumes a reset token, sets the new password, and revokes
// every session so the user re-logs in everywhere.
//
// @Summary		Reset password
// @Description	Single-use 1h token. On success all sessions + refresh tokens are revoked.
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			request	body		dto.ResetPassword	true	"Reset payload"
// @Success		200		{object}	dto.Message		"Password reset successful"
// @Failure		400		{object}	dto.ErrorResponse	"Invalid or expired token"
// @Failure		409		{object}	dto.ErrorResponse	"Token already used"
// @Failure		500		{object}	dto.ErrorResponse	"Reset failed"
// @Router			/auth/reset-password [post]
func (h *Handler) ResetPassword(c *gin.Context) {
	var in dto.ResetPassword
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.ResetPassword(in.Token, in.NewPassword); err != nil {
		switch {
		case errors.Is(err, service.ErrTokenUsed):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrInvalidToken):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "reset failed"})
		}
		return
	}
	c.JSON(http.StatusOK, dto.Message{Message: "Password reset successful"})
}
