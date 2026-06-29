package middleware

import (
	"net/http"
	"strings"
	"sys-backend/config"
	"sys-backend/db"
	"sys-backend/service"

	"github.com/gin-gonic/gin"
)

const UserClaimsKey = "user_claims"

func JWTAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "缺少认证令牌"})
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "认证格式应为 Bearer <token>"})
			c.Abort()
			return
		}

		claims, err := service.ParseToken(config.Configs.Astra.Token, tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "认证令牌无效或已过期"})
			c.Abort()
			return
		}

		c.Set(UserClaimsKey, claims)
		c.Next()
	}
}

func RequireWrite() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, exists := c.Get(UserClaimsKey)
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "未认证"})
			c.Abort()
			return
		}

		jwtClaims, ok := claims.(*service.JWTClaims)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "内部错误"})
			c.Abort()
			return
		}

		if jwtClaims.Role != "readwrite" {
			c.JSON(http.StatusForbidden, gin.H{"detail": "权限不足"})
			c.Abort()
			return
		}

		// 验证密码
		password := c.GetHeader("X-Verify-Password")
		if password == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "需要提供密码"})
			c.Abort()
			return
		}

		user, err := db.GetUserByID(jwtClaims.UserID)
		if err != nil || !service.CheckPassword(password, user.PasswordHash) {
			c.JSON(http.StatusUnauthorized, gin.H{"detail": "密码错误"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func GetUserClaims(c *gin.Context) *service.JWTClaims {
	claims, exists := c.Get(UserClaimsKey)
	if !exists {
		return nil
	}
	jwtClaims, ok := claims.(*service.JWTClaims)
	if !ok {
		return nil
	}
	return jwtClaims
}
