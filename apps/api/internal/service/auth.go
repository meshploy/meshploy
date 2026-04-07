package service

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthService struct {
	db                  *gorm.DB
	onFirstRegistration func(ctx context.Context, orgID uuid.UUID)
}

type RegisterInput struct {
	Username string
	Email    string
	Password string
}

type LoginInput struct {
	Email    string
	Password string
}

// Register creates a new user and provisions a default organization with the
// user as owner — all within a single transaction.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*db.User, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &db.User{
		Username: in.Username,
		Email:    in.Email,
		Password: string(hashed),
	}

	var org db.Organization
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}

		org = db.Organization{
			Name: in.Username + "'s Organization",
			Slug: in.Username,
		}
		if err := tx.Create(&org).Error; err != nil {
			return err
		}

		return tx.Create(&db.OrganizationMember{
			OrganizationID: org.ID,
			UserID:         user.ID,
			Role:           db.RoleOwner,
		}).Error
	})
	if err != nil {
		return nil, err
	}

	if s.onFirstRegistration != nil {
		go s.onFirstRegistration(context.Background(), org.ID)
	}

	return user, nil
}

// Login validates credentials and returns a signed JWT on success.
func (s *AuthService) Login(ctx context.Context, in LoginInput, jwtSecret string) (string, error) {
	var user db.User
	err := s.db.WithContext(ctx).Where("email = ?", in.Email).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errors.New("invalid credentials")
		}
		return "", err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(in.Password)); err != nil {
		return "", errors.New("invalid credentials")
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": user.ID.String(),
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	})

	return token.SignedString([]byte(jwtSecret))
}
