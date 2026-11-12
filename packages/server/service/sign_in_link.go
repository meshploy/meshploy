package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// SignInLinkService makes and spends one-time sign-in links: a person signed
// in to the console by a link an operator made for them, holding the
// installation's sign-in link key. The key is the proof of who the person is,
// so a link skips the password and the second factor; it is spent once, and
// lapses within minutes.
type SignInLinkService struct {
	db *gorm.DB
}

const (
	// SignInLinkTTL is how long a link can be used. The operator making one
	// sends the browser to it at once.
	SignInLinkTTL = 5 * time.Minute
	// signInLinkKept is how long a spent or lapsed link is kept before it is
	// removed, which is only so that a late attempt is answered as spent.
	signInLinkKept = 24 * time.Hour
	// signInLinkPrefix marks the token, as mcli- marks a CLI's.
	signInLinkPrefix = "mlnk-"
)

var (
	ErrSignInLinkNoPerson = errors.New("no one here signs in with that address")
	ErrSignInLinkSpent    = errors.New("this sign-in link was already used or has expired: sign in instead")
)

// Create makes a link for the person who signs in with email and returns its
// token, shown once. An agent has no address and so never gets one.
func (s *SignInLinkService) Create(ctx context.Context, email string) (string, time.Time, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", time.Time{}, ErrSignInLinkNoPerson
	}
	var user db.User
	err := s.db.WithContext(ctx).
		Where("LOWER(email) = LOWER(?) AND kind = ?", email, db.UserHuman).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", time.Time{}, ErrSignInLinkNoPerson
	}
	if err != nil {
		return "", time.Time{}, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	token := signInLinkPrefix + hex.EncodeToString(b)
	now := time.Now()
	expires := now.Add(SignInLinkTTL)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at < ?", now.Add(-signInLinkKept)).Delete(&db.SignInLink{}).Error; err != nil {
			return err
		}
		return tx.Create(&db.SignInLink{UserID: user.ID, TokenHash: hashToken(token), ExpiresAt: expires}).Error
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// Spend uses a link up and returns whom it signs in. Of two attempts at once,
// one wins.
func (s *SignInLinkService) Spend(ctx context.Context, token string) (uuid.UUID, error) {
	if !strings.HasPrefix(token, signInLinkPrefix) {
		return uuid.Nil, ErrSignInLinkSpent
	}
	var link db.SignInLink
	if err := s.db.WithContext(ctx).First(&link, "token_hash = ?", hashToken(token)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return uuid.Nil, ErrSignInLinkSpent
		}
		return uuid.Nil, err
	}
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&db.SignInLink{}).
		Where("id = ? AND used_at IS NULL AND expires_at > ?", link.ID, now).
		Update("used_at", &now)
	if res.Error != nil {
		return uuid.Nil, res.Error
	}
	if res.RowsAffected == 0 {
		return uuid.Nil, ErrSignInLinkSpent
	}
	// Someone removed since the link was made signs in to nothing.
	var n int64
	if err := s.db.WithContext(ctx).Model(&db.User{}).Where("id = ? AND kind = ?", link.UserID, db.UserHuman).Count(&n).Error; err != nil {
		return uuid.Nil, err
	}
	if n == 0 {
		return uuid.Nil, ErrSignInLinkSpent
	}
	return link.UserID, nil
}
