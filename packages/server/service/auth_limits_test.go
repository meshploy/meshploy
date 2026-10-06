package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// Guessing a password, or a second factor, stops after a few tries for that
// account, wherever the guesses come from.
func TestSignInGuessesAreLimitedPerAccount(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "limits", Email: "limits@example.com", Password: "password123"})
	require.NoError(t, err)

	// Ten wrong passwords are refused as wrong; the eleventh is not even tried.
	for i := 0; i < 10; i++ {
		_, err := svcs.Auth.Login(ctx, service.LoginInput{Email: "limits@example.com", Password: "wrong"}, "s")
		require.False(t, errors.Is(err, service.ErrTooManyAttempts), "try %d was refused as too many", i+1)
	}
	_, err = svcs.Auth.Login(ctx, service.LoginInput{Email: "LIMITS@example.com", Password: "password123"}, "s")
	require.ErrorIs(t, err, service.ErrTooManyAttempts, "the address, in any case, is out of tries")

	// The second factor: five wrong codes, then no more.
	require.NoError(t, gdb.Model(&meshdb.User{}).Where("id = ?", user.ID).Update("totp_enabled", true).Error)
	pending, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": user.ID.String(), "mfa_pending": true, "exp": time.Now().Add(5 * time.Minute).Unix(),
	}).SignedString([]byte("s"))
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		_, err := svcs.Auth.CompleteTOTPLogin(ctx, pending, "000000", "s", false, service.Client{})
		require.False(t, errors.Is(err, service.ErrTooManyAttempts), "code try %d was refused as too many", i+1)
	}
	_, err = svcs.Auth.CompleteTOTPLogin(ctx, pending, "000000", "s", false, service.Client{})
	require.ErrorIs(t, err, service.ErrTooManyAttempts)
	_, err = svcs.Auth.CompleteRecoveryLogin(ctx, pending, "abcd-efgh", "s", service.Client{})
	require.ErrorIs(t, err, service.ErrTooManyAttempts, "recovery codes share the person's allowance")
}
