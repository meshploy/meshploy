package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// principal is who Auth takes a bearer token to be, if anyone.
func principal(t *testing.T, bearer string) (uuid.UUID, bool) {
	t.Helper()
	var got uuid.UUID
	var ok bool
	h := Auth("s", nil, nil, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got, ok = UserFromContext(r.Context()) }))
	req := httptest.NewRequest("GET", "/api/v1/orgs", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got, ok
}

func sign(t *testing.T, method jwt.SigningMethod, claims jwt.MapClaims, key any) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Only a full session is a session: not the token a password earns before
// the second factor, not one that never lapses, not one signed another way.
func TestOnlyAFullSessionSignsIn(t *testing.T) {
	uid := uuid.New()
	exp := time.Now().Add(time.Hour).Unix()

	if got, ok := principal(t, sign(t, jwt.SigningMethodHS256, jwt.MapClaims{"uid": uid.String(), "exp": exp}, []byte("s"))); !ok || got != uid {
		t.Fatal("a full session was refused")
	}
	if _, ok := principal(t, sign(t, jwt.SigningMethodHS256, jwt.MapClaims{"uid": uid.String(), "mfa_pending": true, "exp": exp}, []byte("s"))); ok {
		t.Error("the two-factor pending token signed in: two-factor sign-in skipped")
	}
	if _, ok := principal(t, sign(t, jwt.SigningMethodHS256, jwt.MapClaims{"uid": uid.String()}, []byte("s"))); ok {
		t.Error("a token with no expiry signed in")
	}
	if _, ok := principal(t, sign(t, jwt.SigningMethodHS512, jwt.MapClaims{"uid": uid.String(), "exp": exp}, []byte("s"))); ok {
		t.Error("a token signed with another method signed in")
	}
	if _, ok := principal(t, sign(t, jwt.SigningMethodHS256, jwt.MapClaims{"uid": uid.String(), "exp": time.Now().Add(-time.Minute).Unix()}, []byte("s"))); ok {
		t.Error("an expired session signed in")
	}
}
