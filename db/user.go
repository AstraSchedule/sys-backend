package db

import (
	"sys-backend/model/dbTable"
)

func GetUserByID(id uint) (*dbTable.SystemUser, error) {
	var user dbTable.SystemUser
	if err := SysDB.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}
