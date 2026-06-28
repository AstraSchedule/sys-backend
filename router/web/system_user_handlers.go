package web

import (
	"net/http"
	"strconv"
	"sys-backend/db"
	"sys-backend/model/dbTable"
	"sys-backend/service"

	"github.com/gin-gonic/gin"
)

func ListSystemUsers(c *gin.Context) {
	var users []dbTable.SystemUser
	if err := db.SysDB.Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": users})
}

type CreateSystemUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
	Role     string `json:"role" binding:"required,oneof=readwrite readonly"`
}

func CreateSystemUser(c *gin.Context) {
	var req CreateSystemUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效参数"})
		return
	}

	hash, err := service.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "密码哈希失败"})
		return
	}

	user := dbTable.SystemUser{
		Username:     req.Username,
		PasswordHash: hash,
		Role:         req.Role,
	}

	if err := db.SysDB.Create(&user).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"detail": "用户名已存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "user": user})
}

type UpdateSystemUserRequest struct {
	Username *string `json:"username"`
	Password *string `json:"password"`
	Role     *string `json:"role"`
}

func UpdateSystemUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的用户 ID"})
		return
	}

	var user dbTable.SystemUser
	if err := db.SysDB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}

	var req UpdateSystemUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效参数"})
		return
	}

	if req.Username != nil {
		user.Username = *req.Username
	}
	if req.Password != nil {
		if len(*req.Password) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"detail": "密码长度不能少于 6 位"})
			return
		}
		hash, err := service.HashPassword(*req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "密码哈希失败"})
			return
		}
		user.PasswordHash = hash
	}
	if req.Role != nil {
		user.Role = *req.Role
	}

	if err := db.SysDB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "user": user})
}

func DeleteSystemUser(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "无效的用户 ID"})
		return
	}

	result := db.SysDB.Delete(&dbTable.SystemUser{}, id)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "用户不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": 200, "deleted": result.RowsAffected})
}
