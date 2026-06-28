package dbTable

import "time"

type Tenant struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string    `gorm:"uniqueIndex;size:100;not null" json:"name"`
	Namespace string    `gorm:"uniqueIndex;size:100;not null" json:"namespace"`
	Status    string    `gorm:"size:20;not null;default:'active'" json:"status"` // active, banned
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
