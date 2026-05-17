package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/pquerna/otp/totp"
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
	Email       string
	Password    string
	DeviceToken string
}

type LoginResult struct {
	Token        string
	TOTPRequired bool
	MFAToken     string
}

type CompleteTOTPLoginResult struct {
	Token       string
	DeviceToken string // non-empty only when trust_device was true
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

// Login validates credentials. If the user has 2FA enabled it returns an
// MFA token (short-lived, mfa_pending claim) instead of a full JWT.
func (s *AuthService) Login(ctx context.Context, in LoginInput, jwtSecret string) (LoginResult, error) {
	var user db.User
	err := s.db.WithContext(ctx).Where("email = ?", in.Email).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return LoginResult{}, errors.New("invalid credentials")
		}
		return LoginResult{}, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(in.Password)); err != nil {
		return LoginResult{}, errors.New("invalid credentials")
	}

	if user.TOTPEnabled {
		if in.DeviceToken != "" && s.validateDeviceToken(ctx, user.ID, in.DeviceToken) {
			token, err := signFullJWT(user.ID.String(), jwtSecret)
			if err != nil {
				return LoginResult{}, err
			}
			return LoginResult{Token: token}, nil
		}

		mfaTok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"uid":         user.ID.String(),
			"mfa_pending": true,
			"exp":         time.Now().Add(5 * time.Minute).Unix(),
		})
		mfaStr, err := mfaTok.SignedString([]byte(jwtSecret))
		if err != nil {
			return LoginResult{}, err
		}
		return LoginResult{TOTPRequired: true, MFAToken: mfaStr}, nil
	}

	token, err := signFullJWT(user.ID.String(), jwtSecret)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token}, nil
}

// CompleteTOTPLogin validates the MFA token + TOTP code and returns a full JWT.
// If trustDevice is true it also issues a 30-day device token.
func (s *AuthService) CompleteTOTPLogin(ctx context.Context, mfaToken, code, jwtSecret string, trustDevice bool, deviceName string) (CompleteTOTPLoginResult, error) {
	tok, err := jwt.Parse(mfaToken, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(jwtSecret), nil
	})
	if err != nil || !tok.Valid {
		return CompleteTOTPLoginResult{}, errors.New("invalid or expired MFA token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok || claims["mfa_pending"] != true {
		return CompleteTOTPLoginResult{}, errors.New("invalid MFA token")
	}
	userIDStr, _ := claims["uid"].(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return CompleteTOTPLoginResult{}, errors.New("invalid MFA token")
	}

	var user db.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return CompleteTOTPLoginResult{}, errors.New("user not found")
	}
	if !user.TOTPEnabled || string(user.TOTPSecret) == "" {
		return CompleteTOTPLoginResult{}, errors.New("2FA not enabled")
	}
	if !totp.Validate(code, string(user.TOTPSecret)) {
		return CompleteTOTPLoginResult{}, errors.New("invalid code")
	}

	fullToken, err := signFullJWT(userIDStr, jwtSecret)
	if err != nil {
		return CompleteTOTPLoginResult{}, err
	}

	result := CompleteTOTPLoginResult{Token: fullToken}
	if trustDevice {
		dt, err := s.TrustDevice(ctx, userID, deviceName)
		if err == nil {
			result.DeviceToken = dt
		}
	}
	return result, nil
}

// TrustDevice creates a new trusted device record and returns the raw token to store as a cookie.
func (s *AuthService) TrustDevice(ctx context.Context, userID uuid.UUID, deviceName string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	rawToken := hex.EncodeToString(b)
	td := db.TrustedDevice{
		UserID:     userID,
		TokenHash:  hashToken(rawToken),
		DeviceName: deviceName,
		ExpiresAt:  time.Now().Add(30 * 24 * time.Hour),
	}
	if err := s.db.WithContext(ctx).Create(&td).Error; err != nil {
		return "", err
	}
	return rawToken, nil
}

func (s *AuthService) validateDeviceToken(ctx context.Context, userID uuid.UUID, rawToken string) bool {
	var td db.TrustedDevice
	err := s.db.WithContext(ctx).Where(
		"user_id = ? AND token_hash = ? AND expires_at > NOW()",
		userID, hashToken(rawToken),
	).First(&td).Error
	return err == nil
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// ChangePassword verifies the current password and updates it to the new one.
func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	var user db.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(currentPassword)); err != nil {
		return errors.New("current password is incorrect")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&user).Update("password", string(hashed)).Error
}

// GetMe returns the current user by ID.
func (s *AuthService) GetMe(ctx context.Context, userID uuid.UUID) (*db.User, error) {
	var user db.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// SetupTOTP generates a new TOTP secret, persists it (not yet enabled), and
// returns the otpauth:// URL (for QR code) and the raw base32 secret.
func (s *AuthService) SetupTOTP(ctx context.Context, userID uuid.UUID) (otpURL, secret string, err error) {
	var user db.User
	if err = s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Meshploy",
		AccountName: user.Email,
	})
	if err != nil {
		return
	}

	secret = key.Secret()
	if err = s.db.WithContext(ctx).Model(&user).Update("totp_secret", db.EncryptedString(secret)).Error; err != nil {
		return
	}

	return key.URL(), secret, nil
}

// VerifyAndEnableTOTP verifies the given TOTP code against the pending secret
// and, if valid, marks 2FA as enabled for the user.
func (s *AuthService) VerifyAndEnableTOTP(ctx context.Context, userID uuid.UUID, code string) error {
	var user db.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return err
	}
	if string(user.TOTPSecret) == "" {
		return errors.New("run /me/totp/setup first")
	}
	if !totp.Validate(code, string(user.TOTPSecret)) {
		return errors.New("invalid code")
	}
	return s.db.WithContext(ctx).Model(&user).Update("totp_enabled", true).Error
}

// DisableTOTP verifies the current TOTP code and clears all 2FA data.
func (s *AuthService) DisableTOTP(ctx context.Context, userID uuid.UUID, code string) error {
	var user db.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return err
	}
	if !user.TOTPEnabled {
		return errors.New("2FA is not enabled")
	}
	if !totp.Validate(code, string(user.TOTPSecret)) {
		return errors.New("invalid code")
	}
	return s.db.WithContext(ctx).Model(&user).Updates(map[string]any{
		"totp_enabled": false,
		"totp_secret":  "",
	}).Error
}

func signFullJWT(userIDStr, jwtSecret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": userIDStr,
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	})
	return token.SignedString([]byte(jwtSecret))
}
