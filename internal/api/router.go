package router

import (
	"github.com/gin-gonic/gin"
	"github.com/moistello/backend/internal/api/handler"
	"github.com/moistello/backend/internal/api/middleware"
)

func SetupRouter(authHandler *handler.AuthHandler, userHandler *handler.UserHandler, pubKey []byte) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	v1 := r.Group("/v1")
	{
		// Auth routes
		authGroup := v1.Group("/auth")
		{
			authGroup.POST("/nonce", authHandler.Nonce)
			authGroup.POST("/verify", authHandler.Verify)
			authGroup.POST("/refresh", authHandler.Refresh)
			// REST standard session routes with backward compatibility aliases
			authGroup.DELETE("/sessions", middleware.AuthMiddleware(pubKey), authHandler.Logout)
			authGroup.DELETE("/sessions/:id", middleware.AuthMiddleware(pubKey), authHandler.RevokeSessionByID)
			authGroup.POST("/logout", middleware.AuthMiddleware(pubKey), authHandler.Logout)
		}

		// User & Profile routes
		usersGroup := v1.Group("/users", middleware.AuthMiddleware(pubKey))
		{
			usersGroup.POST("/username/claim", userHandler.ClaimName)
		}

		// Legacy alias for non-RESTful claim-name
		v1.POST("/claim-name", middleware.AuthMiddleware(pubKey), userHandler.ClaimName)
	}

	return r
}
