package handler

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	"github.com/moistello/backend/pkg/apperrors"
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

// Register starts the email-based registration flow.  It accepts an email +
// password, checks for existing accounts, and dispatches a 6-digit OTP to the
// provided email address.  The pending registration is stored in Redis until
// RegisterVerify confirms the OTP.
//
// Email is stored using user.HashEmail (full SHA-256 hex) — the single
// canonical representation used by FindByEmail and UpdateProfile.
//
// @Summary Start email registration
// @Tags Authentication
// @Accept json
// @Produce json
// @Param body body object true "Registration payload" {"email":"string","password":"string"}
// @Success 201 {object} response.Envelope
// @Failure 400 {object} response.Envelope
// @Failure 409 {object} response.Envelope
// @Router /auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required,min=8"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// Derive a deterministic wallet address for email-based accounts.
	// Using the first 16 bytes of the SHA-256 digest gives a compact,
	// unique, non-reversible identifier.
	emailHash := sha256.Sum256([]byte(req.Email))
	walletAddr := "EMAIL:" + hex.EncodeToString(emailHash[:16])

	// Check for existing account using the wallet address key.
	existing, err := h.userRepo.FindByWalletAddress(c.Request.Context(), walletAddr)
	if err != nil && err != apperrors.ErrNotFound {
		response.InternalError(c, "failed to check existing account")
		return
	}
	if existing != nil {
		response.Conflict(c, "account already exists")
		return
	}

	// Hash the password before storing in pending registration.
	passwordHash, err := h.authService.HashPassword(req.Password)
	if err != nil {
		response.InternalError(c, "failed to process password")
		return
	}

	// Store the pending registration and send the OTP.
	pending := verification.PendingRegistration{
		Email:        req.Email,
		PasswordHash: passwordHash,
		WalletAddr:   walletAddr,
	}
	if err := h.verificationSvc.StorePendingRegistration(c.Request.Context(), req.Email, pending); err != nil {
		response.InternalError(c, "failed to store pending registration")
		return
	}

	if err := h.verificationSvc.SendOTP(c.Request.Context(), req.Email); err != nil {
		response.InternalError(c, "failed to send verification code")
		return
	}

	response.Created(c, gin.H{"message": "verification code sent to your email"})
}

// RegisterVerify completes the email registration flow by confirming the OTP.
// On success it creates the user record (with hashed email), creates the
// on-chain wallet, and issues a session.
//
// @Summary Complete email registration
// @Tags Authentication
// @Accept json
// @Produce json
// @Param body body object true "Verification payload" {"email":"string","code":"string"}
// @Success 201 {object} response.Envelope
// @Failure 400 {object} response.Envelope
// @Router /auth/register/verify [post]
func (h *AuthHandler) RegisterVerify(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
		Code  string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// Verify the OTP.
	if err := h.verificationSvc.VerifyOTP(c.Request.Context(), req.Email, req.Code); err != nil {
		response.BadRequest(c, "invalid or expired verification code")
		return
	}

	// Retrieve the pending registration payload.
	pending, err := h.verificationSvc.GetPendingRegistration(c.Request.Context(), req.Email)
	if err != nil {
		response.BadRequest(c, "registration session expired; please register again")
		return
	}

	ctx := c.Request.Context()

	// Hash the email using the single canonical transform: full SHA-256 hex.
	// This matches FindByEmail and UpdateProfile so all paths are consistent.
	hashedEmail := user.HashEmail(req.Email)

	now := time.Now().UTC()
	u := &user.User{
		ID:                uuid.New(),
		WalletAddress:     pending.WalletAddr,
		PreferredLanguage: "en",
		Role:              user.RoleUser,
		Email:             &hashedEmail,
		EmailVerified:     true,
		MoiScore:          0,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	// Store the password hash if provided.
	if pending.PasswordHash != "" {
		u.PasswordHash = sql.NullString{String: pending.PasswordHash, Valid: true}
	}

	if err := h.userRepo.Create(ctx, u); err != nil {
		if err == apperrors.ErrConflict {
			response.Conflict(c, "account already exists")
			return
		}
		response.InternalError(c, "failed to create account")
		return
	}

	// Derive wallet seed and create the on-chain wallet.
	if h.walletSvc != nil {
		seed, seedErr := h.walletSvc.DeriveWalletSeed(ctx, req.Email)
		if seedErr == nil {
			seedBytes := []byte(seed)
			// Ignore wallet creation errors — user is already persisted.
			_, _ = h.walletSvc.CreateWallet(ctx, u.ID.String(), seedBytes)
		}
	}

	// Clean up the pending registration.
	_ = h.verificationSvc.DeletePendingRegistration(ctx, req.Email)

	// Issue a session.
	pair, err := h.authService.CreateSession(ctx, u.ID, string(u.Role), sessionTTLFromUser(u), deviceInfoFromContext(c))
	if err != nil {
		response.InternalError(c, "failed to create session")
		return
	}

	response.Created(c, gin.H{
		"token":        pair.AccessToken,
		"refreshToken": pair.RefreshToken,
		"csrfToken":    pair.CSRFToken,
	})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// emailWalletAddress returns the deterministic pseudo-wallet address used for
// email-based accounts.  It is a stable, non-reversible identifier derived
// from the first 16 bytes of the email's SHA-256 digest.
func emailWalletAddress(email string) string {
	h := sha256.Sum256([]byte(email))
	return "EMAIL:" + hex.EncodeToString(h[:16])
}

// sha256HashForLogout returns a hex-encoded SHA-256 digest of s; used to
// derive the Redis session key when revoking refresh tokens on logout.
func sha256HashForLogout(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// sessionTTLFromUser returns the per-user access-token TTL in minutes,
// falling back to 15 minutes when the user has not configured a custom TTL.
func sessionTTLFromUser(u *user.User) int {
	if u != nil && u.SessionTTLMinutes > 0 {
		return u.SessionTTLMinutes
	}
	return 15
}

// deviceInfoFromContext extracts a best-effort device/browser label from the
// User-Agent header for session tracking.
func deviceInfoFromContext(c *gin.Context) string {
	ua := c.GetHeader("User-Agent")
	if len(ua) > 200 {
		return ua[:200]
	}
	return ua
}

