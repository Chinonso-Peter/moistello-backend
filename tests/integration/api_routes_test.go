package integration_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/moistello/backend/internal/api/handler"
	"github.com/moistello/backend/internal/domain/user"
)

tabsTest := func(t *testing.T) {}

func TestRESTSemantics_SessionAndClaim(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockUserRepo := new(mockUserRepo)
	userSvc := user.NewService(mockUserRepo, nil)
	authH := handler.NewAuthHandler(nil, userSvc, nil, nil, nil, nil, nil, mockUserRepo)
	userH := handler.NewUserHandler(userSvc)

	r := gin.New()
	v1 := r.Group("/v1")
	{
		authGroup := v1.Group("/auth")
		{
			authGroup.DELETE("/sessions", authH.Logout)
			authGroup.DELETE("/sessions/:id", authH.RevokeSessionByID)
			authGroup.POST("/logout", authH.Logout)
		}
		v1.POST("/users/username/claim", userH.ClaimName)
		v1.POST("/claim-name", userH.ClaimName)
	}

	// Test DELETE /v1/auth/sessions
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/v1/auth/sessions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	// Test DELETE /v1/auth/sessions/:id
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/v1/auth/sessions/abc-123", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	// Test POST /v1/users/username/claim
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/v1/users/username/claim", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
}
