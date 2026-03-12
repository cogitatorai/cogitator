package orchestrator

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const ctxAccountID contextKey = "account_id"
const ctxIsOperator contextKey = "is_operator"

// generateToken creates a signed JWT for the given orchestrator account.
func (s *Server) generateToken(accountID, email string) (string, error) {
	claims := jwt.MapClaims{
		"sub":   accountID,
		"email": email,
		"exp":   time.Now().Add(24 * time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

// requireAuth is middleware that validates a Bearer JWT and injects the
// account ID into the request context.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, `{"error":"missing or invalid authorization header"}`, http.StatusUnauthorized)
			return
		}
		tokenStr := strings.TrimPrefix(auth, "Bearer ")

		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return s.jwtSecret, nil
		})
		if err != nil || !token.Valid {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, `{"error":"invalid token claims"}`, http.StatusUnauthorized)
			return
		}

		accountID, _ := claims.GetSubject()
		if accountID == "" {
			http.Error(w, `{"error":"missing subject in token"}`, http.StatusUnauthorized)
			return
		}

		var isOperator bool
		_ = s.db.db.QueryRow(`SELECT is_operator FROM accounts WHERE id = ?`, accountID).Scan(&isOperator)

		ctx := context.WithValue(r.Context(), ctxAccountID, accountID)
		ctx = context.WithValue(ctx, ctxIsOperator, isOperator)
		next(w, r.WithContext(ctx))
	}
}

// requireOperator is middleware that ensures the authenticated account has
// operator privileges. It composes on top of requireAuth.
func (s *Server) requireOperator(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		isOp, _ := r.Context().Value(ctxIsOperator).(bool)
		if !isOp {
			jsonError(w, "forbidden: operator access required", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// requireInternal is middleware that validates the X-Internal-Secret header
// using constant-time comparison.
func (s *Server) requireInternal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := r.Header.Get("X-Internal-Secret")
		if subtle.ConstantTimeCompare([]byte(secret), []byte(s.cfg.InternalSecret)) != 1 {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
