package auth

import (
	"github.com/gin-gonic/gin"

	"github.com/nikhea/rallya/internal/auth/handler"
	"github.com/nikhea/rallya/internal/auth/repository"
)

// RegisterRoutes wires the Auth HTTP endpoints (OAuth/MFA excluded).
//
//	POST /api/v1/auth/register
//	POST /api/v1/auth/verify-email
//	POST /api/v1/auth/verify-code
//	POST /api/v1/auth/resend-verification
//	POST /api/v1/auth/login
//	POST /api/v1/auth/refresh
//	POST /api/v1/auth/logout (auth)
//	GET  /api/v1/auth/me (auth)
//	POST /api/v1/auth/forgot-password
//	POST /api/v1/auth/reset-password
func RegisterRoutes(g *gin.RouterGroup, h *handler.Handler, repo *repository.AuthRepository) {
	g.POST("/register", h.Register)
	g.POST("/verify-email", h.VerifyEmail)
	g.GET("/verify-email", h.VerifyEmail)
	g.POST("/verify-code", h.VerifyByCode)
	g.POST("/resend-verification", h.ResendVerification)
	g.POST("/login", h.Login)
	g.POST("/refresh", h.Refresh)
	g.POST("/forgot-password", h.ForgotPassword)
	g.POST("/reset-password", h.ResetPassword)

	authed := g.Group("")
	authed.Use(handler.RequireAuth(repo))
	authed.POST("/logout", h.Logout)
	authed.GET("/me", h.Me)
}
