package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"sys-backend/config"
	"sys-backend/db"
	"sys-backend/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormsqlite "github.com/libtnb/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestMiddleware() {
	gin.SetMode(gin.TestMode)

	config.Configs = config.Config{
		Astra: config.AstraConfig{
			Token: "test-secret",
		},
	}

	db.DB, _ = gorm.Open(gormsqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
}

func TestJWTAuthMiddleware_NoAuth(t *testing.T) {
	setupTestMiddleware()

	router := gin.New()
	router.Use(JWTAuthMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestJWTAuthMiddleware_InvalidFormat(t *testing.T) {
	setupTestMiddleware()

	router := gin.New()
	router.Use(JWTAuthMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "InvalidToken")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestJWTAuthMiddleware_InvalidToken(t *testing.T) {
	setupTestMiddleware()

	router := gin.New()
	router.Use(JWTAuthMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestJWTAuthMiddleware_ValidToken(t *testing.T) {
	setupTestMiddleware()

	// Create a valid token
	token, err := service.GenerateToken(config.Configs.Astra.Token, 1, "testuser", "admin", 24)
	require.NoError(t, err)

	router := gin.New()
	router.Use(JWTAuthMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireWrite_NoClaims(t *testing.T) {
	setupTestMiddleware()

	router := gin.New()
	router.Use(RequireWrite())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireWrite_WrongRole(t *testing.T) {
	setupTestMiddleware()

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(UserClaimsKey, &service.JWTClaims{
			Role: "readonly",
		})
		c.Next()
	})
	router.Use(RequireWrite())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestGetUserClaims_NoClaims(t *testing.T) {
	setupTestMiddleware()

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		claims := GetUserClaims(c)
		assert.Nil(t, claims)
		c.JSON(200, gin.H{"message": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetUserClaims_InvalidClaims(t *testing.T) {
	setupTestMiddleware()

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		c.Set(UserClaimsKey, "invalid")
		claims := GetUserClaims(c)
		assert.Nil(t, claims)
		c.JSON(200, gin.H{"message": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetUserClaims_ValidClaims(t *testing.T) {
	setupTestMiddleware()

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		c.Set(UserClaimsKey, &service.JWTClaims{
			UserID:   1,
			Username: "testuser",
			Role:     "admin",
		})
		claims := GetUserClaims(c)
		assert.NotNil(t, claims)
		assert.Equal(t, uint(1), claims.UserID)
		c.JSON(200, gin.H{"message": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}