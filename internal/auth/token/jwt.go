package token

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims carries the access-token payload. sid binds the token to a
// row in sessions so logout / reset-password can revoke it.
type Claims struct {
	jwt.RegisteredClaims
	SessionID uuid.UUID `json:"sid"`
}

// GenerateAccessToken mints a short-lived JWT for userID bound to sessionID.
func GenerateAccessToken(secret []byte, ttl time.Duration, userID, sessionID uuid.UUID) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		SessionID: sessionID,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(secret)
}

// ParseAccessToken validates signature/expiry and returns identity.
func ParseAccessToken(secret []byte, raw string) (userID, sessionID uuid.UUID, err error) {
	claims := &Claims{}
	tok, err := jwt.ParseWithClaims(raw, claims, func(_ *jwt.Token) (any, error) {
		return secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if !tok.Valid {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invalid token")
	}
	userID, err = uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invalid sub claim: %w", err)
	}
	if claims.SessionID == uuid.Nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("missing sid claim")
	}
	return userID, claims.SessionID, nil
}
