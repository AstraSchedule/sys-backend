package web

import (
	"net/http"
	"sys-backend/config"
	"sys-backend/db"
	"sys-backend/middleware"
	"sys-backend/model/dbTable"
	"sys-backend/service"

	"github.com/gin-gonic/gin"
)

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
	NewUsername string `json:"new_username"`
}

func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效参数"})
		return
	}

	var user dbTable.SystemUser
	if err := db.SysDB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "用户名或密码错误"})
		return
	}

	if !service.CheckPassword(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "用户名或密码错误"})
		return
	}

	token, err := service.GenerateToken(config.Configs.Astra.Token, user.ID, user.Username, user.Role, 24)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "生成令牌失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":           token,
		"must_change_pwd": user.MustChangePwd,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

func GetMe(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未认证"})
		return
	}

	var user dbTable.SystemUser
	if err := db.SysDB.First(&user, claims.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":              user.ID,
		"username":        user.Username,
		"role":            user.Role,
		"must_change_pwd": user.MustChangePwd,
	})
}

func ChangePassword(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未认证"})
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效参数"})
		return
	}

	var user dbTable.SystemUser
	if err := db.SysDB.First(&user, claims.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}

	if !service.CheckPassword(req.OldPassword, user.PasswordHash) {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "旧密码错误"})
		return
	}

	if req.NewUsername != "" && req.NewUsername != user.Username {
		if len(req.NewUsername) < 3 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "用户名长度不能少于 3 位"})
			return
		}
		user.Username = req.NewUsername
	}

	hash, err := service.HashPassword(req.NewPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "密码哈希失败"})
		return
	}
	user.PasswordHash = hash
	user.MustChangePwd = false

	if err := db.SysDB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "更新失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "修改成功"})
}

func VerifyPassword(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "未认证"})
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效参数"})
		return
	}

	user, err := db.GetUserByID(claims.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "用户不存在"})
		return
	}

	if !service.CheckPassword(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "密码错误"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "message": "验证通过"})
}
