package service

import (
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Query results show values as Postgres prints them, not as Go structs.
func TestPGTextPrintsValuesAsPostgresDoes(t *testing.T) {
	id := uuid.MustParse("4d4447d7-a946-4f5f-9e72-2c82f1d61c14")
	at := time.Date(2026, 9, 30, 14, 30, 0, 0, time.UTC)
	for _, c := range []struct {
		in   any
		want any
	}{
		{pgtype.Numeric{Int: big.NewInt(124000), Exp: -2, Valid: true}, "1240.00"},
		{pgtype.Numeric{}, nil},
		{[16]byte(id), id.String()},
		{at, "2026-09-30T14:30:00Z"},
		{[]byte("abc"), "abc"},
		{int64(3), "3"},
		{nil, nil},
	} {
		if got := pgText(c.in); got != c.want {
			t.Errorf("pgText(%#v) = %#v, want %#v", c.in, got, c.want)
		}
	}
}
