package server

import (
	"io/fs"
	"net/http"

	"github.com/rajsinghtech/tailgrant/internal/grant"
	"go.temporal.io/sdk/client"
	"tailscale.com/client/local"
	tailscale "tailscale.com/client/tailscale/v2"
)

func NewRouter(lc *local.Client, tc client.Client, tsClient *tailscale.Client, grantTypes grant.GrantTypeStore, taskQueue string, staticFS fs.FS) http.Handler {
	h := &Handlers{
		TemporalClient: tc,
		TSClient:       tsClient,
		GrantTypes:     grantTypes,
		TaskQueue:      taskQueue,
	}

	mux := http.NewServeMux()

	// API routes behind WhoIs auth
	api := http.NewServeMux()
	api.HandleFunc("POST /api/grants", h.HandleCreateGrant)
	api.HandleFunc("POST /api/grants/{id}/approve", h.HandleApproveGrant)
	api.HandleFunc("POST /api/grants/{id}/deny", h.HandleDenyGrant)
	api.HandleFunc("POST /api/grants/{id}/revoke", h.HandleRevokeGrant)
	api.HandleFunc("GET /api/grants/{id}", h.HandleGetGrant)
	api.HandleFunc("GET /api/grant-types", h.HandleListGrantTypes)
	api.HandleFunc("GET /api/devices", h.HandleListDevices)
	api.HandleFunc("GET /api/users", h.HandleListUsers)
	api.HandleFunc("GET /api/grants", h.HandleListGrants)
	api.HandleFunc("GET /api/whoami", h.HandleWhoAmI)
	api.HandleFunc("POST /api/grants/{id}/extend", h.HandleExtendGrant)

	mux.Handle("/api/", WhoIsMiddleware(lc)(api))

	// Serve static UI files
	mux.Handle("/", http.FileServerFS(staticFS))

	return mux
}
