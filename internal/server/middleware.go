package server

import (
	"context"
	"encoding/json"
	"net/http"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
)

type contextKey string

const whoIsContextKey contextKey = "whois"

func WhoIsFromContext(ctx context.Context) *apitype.WhoIsResponse {
	v, _ := ctx.Value(whoIsContextKey).(*apitype.WhoIsResponse)
	return v
}

func WhoIsMiddleware(lc *local.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			who, err := lc.WhoIs(r.Context(), r.RemoteAddr)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized: " + err.Error()})
				return
			}

			if who.Node == nil || who.UserProfile == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "incomplete identity"})
				return
			}

			if who.Node.IsTagged() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "tagged nodes cannot request grants"})
				return
			}

			ctx := context.WithValue(r.Context(), whoIsContextKey, who)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
