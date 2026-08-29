package handler

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/moistello/backend/internal/api/middleware"
	"github.com/moistello/backend/internal/domain/auth"
	"github.com/moistello/backend/internal/domain/email"
	"github.com/moistello/backend/internal/domain/totp"
	"github.com/moistello/backend/internal/domain/user"
	"github.com/moistello/backend/internal/domain/verification"
	"github.com/moistello/backend/internal/domain/wallet"
	"github.com/moistello/backend/pkg/response"
	"github.com/moistello/backend/pkg/stellar"
)

type AuthHandler struct {
	authService     auth.Service
	userService     user.Service
	walletSvc       wallet.Service
	totpService     *totp.Service
	verificationSvc *verification.Service
	emailSvc        *email.Service
	redisClient     *redis.Client
	userRepo        user.Repository
}

func NewAuthHandler(authSvc auth.Service, userSvc user.Service, walletSvc wallet.Service,
	totpSvc *totp.Service, verificationSvc *verification.Service, emailSvc *email.Service,
	redisClient *redis.Client, userRepo user.Repository) *AuthHandler {
	return &AuthHandler{
		authService:     authSvc,
		userService:     userSvc,
		walletSvc:       walletSvc,
		totpService:     totpSvc,
		verificationSvc: verificationSvc,
		emailSvc:        emailSvc,
		redisClient:     redisClient,
		userRepo:        userRepo,
	}
}

// @Summary Get authentication nonce
// @Description Returns a signed nonce for wallet authentication. The nonce must be signed with the wallet's private key and sent to /auth/verify.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param body body object true "Wallet address" { "walletAddress": "G..." }
// @Success 200 {object} response.Envelope{data=object{nonce=string}}
// @Failure 400 {object} response.Envelope
// @Router /auth/nonce [post]
func (h *AuthHandler) Nonce(c *gin.Context) {
	var req struct {
		WalletAddress string `json:"walletAddress" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if err := stellar.ValidateAddress(req.WalletAddress); err != nil {
		response.BadRequest(c, "invalid wallet address: "+err.Error())
		return
	}

	nonce, err := h.authService.GenerateNonce(c.Request.Context(), req.WalletAddress)
	if err != nil {
		response.InternalError(c, "failed to generate nonce")
		return
	}
	response.OK(c, gin.H{"nonce": nonce})
}

// @Summary Verify wallet authentication
// @Description Verifies a signed nonce and creates a session.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param body body object true "Signature payload" { "walletAddress": "G...", "signature": "..." }
// @Success 200 {object} response.Envelope{data=object{token=string,refreshToken=string}}
// @Failure 400 {object} response.Envelope
// @Failure 401 {object} response.Envelope
// @Router /auth/verify [post]
func (h *AuthHandler) Verify(c *gin.Context) {
	var req struct {
		WalletAddress string `json:"walletAddress" binding:"required"`
		Signature     string `json:"signature" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	u, err := h.userService.GetByWallet(c.Request.Context(), req.WalletAddress)
	if err != nil {
		response.NotFound(c, "account not found")
		return
	}

	valid, err := h.authService.VerifySignature(c.Request.Context(), req.WalletAddress, req.Signature)
	if err != nil || !valid {
		response.Unauthorized(c, "signature verification failed")
		return
	}

	pair, err := h.authService.CreateSession(c.Request.Context(), u.ID, string(u.Role), sessionTTLFromUser(u), deviceInfoFromContext(c))
	if err != nil {
		response.InternalError(c, "failed to create session")
		return
	}

	response.OK(c, gin.H{
		"token": pair.AccessToken, "refreshToken": pair.RefreshToken, "csrfToken": pair.CSRFToken, "user": u,
	})
}

// @Summary Refresh JWT tokens
// @Description Exchanges a valid refresh token for a new access token and refresh token pair.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param body body object true "Refresh token" { "refreshToken": "string" }
// @Success 200 {object} response.Envelope{data=object{token=string,refreshToken=string}}
// @Failure 400 {object} response.Envelope
// @Failure 401 {object} response.Envelope
// @Router /auth/refresh [post]
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refreshToken" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	tokenPair, err := h.authService.RefreshToken(c.Request.Context(), req.RefreshToken)
	if err != nil {
		response.Unauthorized(c, "invalid refresh token")
		return
	}
	response.OK(c, gin.H{"token": tokenPair.AccessToken, "refreshToken": tokenPair.RefreshToken, "csrfToken": tokenPair.CSRFToken})
}

// @Summary Get current user profile
// @Description Returns the authenticated user's profile. Requires Bearer token. Replaces the old POST /auth/me endpoint.
// @Tags Authentication
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=object{user=object}}
// @Failure 401 {object} response.Envelope
// @Router /me [get]
func (h *AuthHandler) Me(c *gin.Context) {
	userID := middleware.GetUserID(c)
	u, err := h.userService.GetByID(c.Request.Context(), userID)
	if err != nil {
		response.Unauthorized(c, "user not found")
		return
	}
	response.OK(c, gin.H{"user": u})
}

// @Summary Logout / Terminate Session
// @Description Invalidates the current session and all refresh tokens. REST standard: DELETE /v1/auth/sessions
// @Tags Authentication
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=object{success=bool}}
// @Router /auth/sessions [delete]
func (h *AuthHandler) Logout(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		response.Unauthorized(c, "missing or invalid token")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	userID := middleware.GetUserID(c)
	ctx := c.Request.Context()

	if h.redisClient != nil {
		// 1. Blocklist the access token
		if expiry, err := middleware.ExtractTokenExpiry(token); err == nil {
			middleware.BlocklistToken(ctx, h.redisClient, token, expiry)
		}

		// 2. Delete all user sessions from Redis
		if userID != "" {
			userSessionsKey := fmt.Sprintf("user:sessions:%s", userID)
			sessionHashes, err := h.redisClient.SMembers(ctx, userSessionsKey).Result()
			if err == nil {
				pipe := h.redisClient.Pipeline()
				for _, hash := range sessionHashes {
					pipe.Del(ctx, fmt.Sprintf("session:%s", hash))
				}
				pipe.Del(ctx, userSessionsKey)
				pipe.Exec(ctx)
			}

			// 3. Set blocklist key for any missed sessions
			middleware.BlocklistUserRefreshTokens(ctx, h.redisClient, userID)
		}

		// 4. If refresh token was provided in body, also delete that specific session
		var req struct {
			RefreshToken string `json:"refreshToken"`
		}
		if err := c.ShouldBindJSON(&req); err == nil && req.RefreshToken != "" {
			tokenHash := sha256HashForLogout(req.RefreshToken)
			sessionKey := fmt.Sprintf("session:%s", tokenHash)
			h.redisClient.Del(ctx, sessionKey)
		}
	}

	response.OK(c, gin.H{"success": true})
}

// @Summary Revoke specific session by ID/hash
// @Description Revokes a specific session.
// @Tags Authentication
// @Produce json
// @Security BearerAuth
// @Param id path string true "Session Hash / ID"
// @Success 200 {object} response.Envelope{data=object{success=bool}}
// @Router /auth/sessions/{id} [delete]
func (h *AuthHandler) RevokeSessionByID(c *gin.Context) {
	sessionID := c.Param("id")
	userID := middleware.GetUserID(c)
	ctx := c.Request.Context()

	if h.redisClient != nil && sessionID != "" {
		// If the id is a session hash, delete it directly, or remove from user sessions
		sessionKey := fmt.Sprintf("session:%s", sessionID)
		h.redisClient.Del(ctx, sessionKey)
		if userID != "" {
			userSessionsKey := fmt.Sprintf("user:sessions:%s", userID)
			h.redisClient.SRem(ctx, userSessionsKey, sessionID)
		}
	}

	response.OK(c, gin.H{"success": true})
}
