package server

import (
	"context"
	"encoding/json"
	"net/http"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
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
				// WhoIs fails for VIP service connections where RemoteAddr
				// is localhost. Fall back to Tailscale-User-* headers
				// injected by the serve proxy in HTTP service mode.
				who = whoIsFromHeaders(r)
			}

			if who == nil || who.UserProfile == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}

			if who.Node != nil && who.Node.IsTagged() {
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

// whoIsFromHeaders builds a WhoIsResponse from Tailscale-User-* headers
// set by the Tailscale serve proxy for HTTP service mode connections.
func whoIsFromHeaders(r *http.Request) *apitype.WhoIsResponse {
	login := r.Header.Get("Tailscale-User-Login")
	if login == "" {
		return nil
	}
	return &apitype.WhoIsResponse{
		UserProfile: &tailcfg.UserProfile{
			LoginName:   login,
			DisplayName: r.Header.Get("Tailscale-User-Name"),
		},
		Node: &tailcfg.Node{},
	}
}
