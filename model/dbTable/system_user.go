package dbTable

import "time"

type SystemUser struct {
	ID             uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Username       string    `gorm:"uniqueIndex;size:50;not null" json:"username"`
	PasswordHash   string    `gorm:"size:255;not null" json:"-"`
	Role           string    `gorm:"size:20;not null;default:'readonly'" json:"role"` // readwrite, readonly
	MustChangePwd  bool      `gorm:"default:false" json:"must_change_pwd"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
