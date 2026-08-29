package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRabbit struct {
	alive bool
}

func (m *mockRabbit) IsAlive() bool {
	return m.alive
}

func setupTestHealthHandler(t *testing.T) (*HealthHandler, sqlmock.Sqlmock, func()) {
	gin.SetMode(gin.TestMode)
	mockDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)

	mr, err := miniredis.Run()
	require.NoError(t, err)

	rds := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	h := NewHealthHandler(mockDB, rds, "http://localhost:8000", "http://localhost:8001")
	h.WithRabbitMQ(&mockRabbit{alive: true})

	cleanup := func() {
		mockDB.Close()
		rds.Close()
		mr.Close()
	}

	return h, mock, cleanup
}

func TestHealthHandler_Health_AllHealthy(t *testing.T) {
	h, mock, cleanup := setupTestHealthHandler(t)
	defer cleanup()

	mock.ExpectPing()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/health", nil)

	h.Health(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestHealthHandler_Health_PostgresDown(t *testing.T) {
	h, mock, cleanup := setupTestHealthHandler(t)
	defer cleanup()

	mock.ExpectPing().WillReturnError(sql.ErrConnDone)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/health", nil)

	h.Health(c)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "degraded")
	assert.Contains(t, w.Body.String(), "unhealthy")
}

func TestHealthHandler_Readiness_Unhealthy(t *testing.T) {
	h, mock, cleanup := setupTestHealthHandler(t)
	defer cleanup()

	h.WithRabbitMQ(&mockRabbit{alive: false})
	mock.ExpectPing()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/health/ready", nil)

	h.Readiness(c)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "not ready")
}

func TestHealthHandler_Liveness(t *testing.T) {
	h, _, cleanup := setupTestHealthHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/health/live", nil)

	h.Liveness(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}
