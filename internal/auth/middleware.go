package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dsandor/memory/internal/storage"
)

// TeamContext holds the resolved identity for the current request.
type TeamContext struct {
	TeamID string
	KeyID  string // set for API key requests
	UserID string // set for session requests
	Role   string // member|curator|admin|superadmin
}

type contextKey struct{}

// GetTeamContext retrieves the injected TeamContext from a request context.
func GetTeamContext(ctx context.Context) TeamContext {
	if tc, ok := ctx.Value(contextKey{}).(TeamContext); ok {
		return tc
	}
	return TeamContext{}
}

// InjectSuperadmin returns a context with a superadmin TeamContext injected.
// Used by the dev bypass middleware — never call from production auth paths.
func InjectSuperadmin(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey{}, TeamContext{Role: "superadmin"})
}

// AuthStore is the minimal storage interface needed by RequireAuth.
type AuthStore interface {
	GetAPIKeyByHash(ctx context.Context, hash string) (*storage.APIKey, error)
	GetSession(ctx context.Context, tokenHash string) (*storage.Session, error)
	TouchAPIKey(ctx context.Context, id string) error
}

// HashSHA256 returns the hex-encoded SHA-256 of the input string.
func HashSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func writeUnauth(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
}

// RequireAuth resolves a bearer token or session cookie into a TeamContext.
// Returns 401 if neither is present or valid.
func RequireAuth(store AuthStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// 1. Try Bearer token
			if authHdr := r.Header.Get("Authorization"); strings.HasPrefix(authHdr, "Bearer ") {
				raw := strings.TrimPrefix(authHdr, "Bearer ")
				hash := HashSHA256(raw)
				key, err := store.GetAPIKeyByHash(ctx, hash)
				if err == nil {
					go store.TouchAPIKey(context.Background(), key.ID) //nolint:errcheck
					tc := TeamContext{TeamID: key.TeamID, KeyID: key.ID, Role: key.Role}
					next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, contextKey{}, tc)))
					return
				}
			}

			// 2. Try session cookie
			if cookie, err := r.Cookie("session"); err == nil {
				hash := HashSHA256(cookie.Value)
				sess, err := store.GetSession(ctx, hash)
				if err == nil && sess.ExpiresAt.After(time.Now()) {
					tc := TeamContext{UserID: sess.UserID}
					next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, contextKey{}, tc)))
					return
				}
			}

			writeUnauth(w, "authentication required")
		})
	}
}

// roleRank maps roles to a numeric rank for comparison.
func roleRank(role string) int {
	switch role {
	case "superadmin":
		return 4
	case "admin":
		return 3
	case "curator":
		return 2
	case "member":
		return 1
	}
	return 0
}

func requireRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc := GetTeamContext(r.Context())
			if roleRank(tc.Role) < roleRank(minRole) {
				writeForbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireCurator gates routes to curator, admin, or superadmin.
func RequireCurator() func(http.Handler) http.Handler { return requireRole("curator") }

// RequireAdmin gates routes to admin or superadmin.
func RequireAdmin() func(http.Handler) http.Handler { return requireRole("admin") }

// RequireSuperadmin gates routes to superadmin only.
func RequireSuperadmin() func(http.Handler) http.Handler { return requireRole("superadmin") }
