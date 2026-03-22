package db

import (
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Username string `gorm:"uniqueIndex"`
	Email    string `gorm:"uniqueIndex"`
	Password string
}
