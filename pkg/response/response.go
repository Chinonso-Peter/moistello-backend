package response

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

type Envelope struct {
	Success   bool        `json:"success"`
	Code      string      `json:"code,omitempty"`
	Message   string      `json:"message,omitempty"`
	Details   any         `json:"details,omitempty"`
	RequestId string      `json:"requestId,omitempty"`
	Data      any         `json:"data,omitempty"`
	Meta      any         `json:"meta,omitempty"`
}

type PaginationMeta struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	TotalItems int `json:"totalItems"`
	TotalPages int `json:"totalPages"`
}

func NewPaginationMeta(page, limit, total int) PaginationMeta {
	totalPages := 0
	if limit > 0 {
		totalPages = (total + limit - 1) / limit
	}
	return PaginationMeta{
		Page:       page,
		Limit:      limit,
		TotalItems: total,
		TotalPages: totalPages,
	}
}

func getRequestID(c *gin.Context) string {
	if rid := c.GetHeader("X-Request-Id"); rid != "" {
		return rid
	}
	if rid, exists := c.Get("request_id"); exists {
		if s, ok := rid.(string); ok {
			return s
		}
	}
	return ""
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{
		Success:   true,
		Data:      data,
		RequestId: getRequestID(c),
	})
}

func OKWithMeta(c *gin.Context, data any, meta any) {
	c.JSON(http.StatusOK, Envelope{
		Success:   true,
		Data:      data,
		Meta:      meta,
		RequestId: getRequestID(c),
	})
}

func Error(c *gin.Context, status int, code, message string, details any) {
	c.JSON(status, Envelope{
		Success:   false,
		Code:      code,
		Message:   message,
		Details:   details,
		RequestId: getRequestID(c),
	})
}

func BadRequest(c *gin.Context, message string) {
	Error(c, http.StatusBadRequest, "BAD_REQUEST", message, nil)
}

func ValidationError(c *gin.Context, message string, details any) {
	Error(c, http.StatusBadRequest, "VALIDATION_ERROR", message, details)
}

func Unauthorized(c *gin.Context, message string) {
	Error(c, http.StatusUnauthorized, "UNAUTHORIZED", message, nil)
}

func Forbidden(c *gin.Context, message string) {
	Error(c, http.StatusForbidden, "FORBIDDEN", message, nil)
}

func NotFound(c *gin.Context, message string) {
	Error(c, http.StatusNotFound, "NOT_FOUND", message, nil)
}

func Conflict(c *gin.Context, message string) {
	Error(c, http.StatusConflict, "CONFLICT", message, nil)
}

func InternalError(c *gin.Context, message string) {
	Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", message, nil)
}
