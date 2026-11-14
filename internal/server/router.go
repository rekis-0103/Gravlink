package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"gravlink/internal/agy"
	"gravlink/internal/config"
	"gravlink/internal/security"
	"gravlink/internal/workspace"
)

// Dependencies groups backend services injected into the HTTP router.
type Dependencies struct {
	Config        *config.Config
	Sessions      *security.SessionManager
	RateLimiter   *security.AuthRateLimiter
	Audit         *security.AuditLogger
	Explorer      *workspace.Explorer
	Dispatcher    *agy.Dispatcher
	HubProxy      http.Handler
	StrictSubnets []*net.IPNet
	CACertPEM     []byte
	StaticFS      fs.FS
}

type wsEvent struct {
	Type  string               `json:"type"`
	Steps []agy.TranscriptStep `json:"steps,omitempty"`
	Error string               `json:"error,omitempty"`
}

// NewRouter builds the full LAN-secured HTTP/WebSocket handler.
func NewRouter(deps Dependencies) http.Handler {
	if deps.Dispatcher == nil && deps.Config != nil {
		deps.Dispatcher = agy.NewDispatcher(deps.Config.AntigravityDir)
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /ca.crt", func(w http.ResponseWriter, r *http.Request) {
		if len(deps.CACertPEM) == 0 {
			http.Error(w, "TLS CA certificate not enabled", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="gravlink-local-ca.crt"`)
		_, _ = w.Write(deps.CACertPEM)
	})

	mux.HandleFunc("GET /api/auth/status", func(w http.ResponseWriter, r *http.Request) {
		claims, ok := deps.Sessions.VerifyRequest(r)
		resp := map[string]any{
			"authenticated": ok,
			"tls":           deps.Config.UseTLS,
		}
		if ok {
			resp["csrf_token"] = claims.CSRFToken
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("POST /api/auth/login", handleLogin(deps))
	mux.HandleFunc("POST /api/auth/pair", handlePair(deps))
	mux.HandleFunc("POST /api/auth/logout", deps.Sessions.RequireAuth(deps.Audit, func(w http.ResponseWriter, r *http.Request, claims *security.SessionClaims) {
		deps.Sessions.ClearSession(w)
		deps.Audit.Log(security.AuditEvent{
			Event:    "logout",
			ClientIP: r.RemoteAddr,
			Method:   r.Method,
			Path:     r.URL.Path,
			Outcome:  "success",
		})
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))

	mux.HandleFunc("GET /api/status", deps.Sessions.RequireAuth(deps.Audit, handleStatus(deps)))
	mux.HandleFunc("GET /api/doctor", deps.Sessions.RequireAuth(deps.Audit, handleDoctor(deps)))
	mux.HandleFunc("GET /api/models", deps.Sessions.RequireAuth(deps.Audit, handleModels(deps)))
	mux.HandleFunc("GET /api/conversations", deps.Sessions.RequireAuth(deps.Audit, handleConversations(deps)))
	mux.HandleFunc("GET /api/conversations/{id}/steps", deps.Sessions.RequireAuth(deps.Audit, handleConversationSteps(deps)))
	mux.HandleFunc("POST /api/conversations/{id}/prompt", deps.Sessions.RequireAuth(deps.Audit, handleSendPrompt(deps)))
	mux.HandleFunc("GET /api/conversations/{id}/pending", deps.Sessions.RequireAuth(deps.Audit, handlePendingInteractions(deps)))
	mux.HandleFunc("POST /api/conversations/{id}/interact", deps.Sessions.RequireAuth(deps.Audit, handleInteract(deps)))
	mux.HandleFunc("GET /api/ws/conversations/{id}", handleConversationWS(deps))
	mux.HandleFunc("GET /api/workspace/tree", deps.Sessions.RequireAuth(deps.Audit, handleWorkspaceTree(deps)))
	mux.HandleFunc("GET /api/workspace/file", deps.Sessions.RequireAuth(deps.Audit, handleWorkspaceFile(deps)))
	mux.HandleFunc("/hub/", handleHubProxy(deps))
	mux.HandleFunc("GET /hub", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/hub/", http.StatusMovedPermanently)
	})

	if deps.StaticFS != nil {
		mux.Handle("/", spaHandler(deps.StaticFS))
	}

	root := securityHeadersMiddleware(mux)
	return security.IPFilterMiddleware(deps.StrictSubnets, deps.Audit)(root)
}

func handleLogin(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !security.ValidateSameOrigin(r) {
			http.Error(w, "Forbidden: origin mismatch", http.StatusForbidden)
			return
		}
		clientIP := ipKey(r.RemoteAddr)
		if allowed, retryAfter := deps.RateLimiter.Allow(clientIP); !allowed {
			deps.Audit.Log(security.AuditEvent{
				Event:    "login_rate_limited",
				ClientIP: r.RemoteAddr,
				Method:   r.Method,
				Path:     r.URL.Path,
				Outcome:  "denied",
			})
			security.WriteRateLimitExceeded(w, retryAfter)
			return
		}

		var body struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		ok, err := security.VerifyPassword(body.Password, deps.Config.PasswordHash)
		if err != nil || !ok {
			deps.RateLimiter.RecordFailure(clientIP)
			deps.Audit.Log(security.AuditEvent{
				Event:    "login_failed",
				ClientIP: r.RemoteAddr,
				Method:   r.Method,
				Path:     r.URL.Path,
				Outcome:  "denied",
			})
			http.Error(w, "Invalid password", http.StatusUnauthorized)
			return
		}

		deps.RateLimiter.Reset(clientIP)
		claims, err := deps.Sessions.IssueSession(w, clientIP)
		if err != nil {
			http.Error(w, "Failed to issue session", http.StatusInternalServerError)
			return
		}
		deps.Audit.Log(security.AuditEvent{
			Event:    "login_success",
			ClientIP: r.RemoteAddr,
			Method:   r.Method,
			Path:     r.URL.Path,
			Outcome:  "success",
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"csrf_token":    claims.CSRFToken,
		})
	}
}

func handlePair(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !security.ValidateSameOrigin(r) {
			http.Error(w, "Forbidden: origin mismatch", http.StatusForbidden)
			return
		}
		clientIP := ipKey(r.RemoteAddr)
		if allowed, retryAfter := deps.RateLimiter.Allow(clientIP); !allowed {
			security.WriteRateLimitExceeded(w, retryAfter)
			return
		}

		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 2048)).Decode(&body); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		if !deps.Sessions.ConsumePairingToken(body.Token) {
			deps.RateLimiter.RecordFailure(clientIP)
			deps.Audit.Log(security.AuditEvent{
				Event:    "pair_failed",
				ClientIP: r.RemoteAddr,
				Method:   r.Method,
				Path:     r.URL.Path,
				Outcome:  "denied",
			})
			http.Error(w, "Pairing token invalid or expired", http.StatusUnauthorized)
			return
		}

		deps.RateLimiter.Reset(clientIP)
		claims, err := deps.Sessions.IssueSession(w, clientIP)
		if err != nil {
			http.Error(w, "Failed to issue session", http.StatusInternalServerError)
			return
		}
		deps.Audit.Log(security.AuditEvent{
			Event:    "pair_success",
			ClientIP: r.RemoteAddr,
			Method:   r.Method,
			Path:     r.URL.Path,
			Outcome:  "success",
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"csrf_token":    claims.CSRFToken,
		})
	}
}

func handleStatus(deps Dependencies) func(http.ResponseWriter, *http.Request, *security.SessionClaims) {
	return func(w http.ResponseWriter, r *http.Request, _ *security.SessionClaims) {
		hubs, _ := agy.DiscoverHubs(r.Context(), deps.Config.AntigravityDir)
		fwExists, fwDetail := security.CheckPrivateFirewallStatus()
		var wsRoot string
		if deps.Explorer != nil {
			wsRoot = deps.Explorer.Root()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"workspace":        wsRoot,
			"hubs":             hubs,
			"firewall_rule_ok": fwExists,
			"firewall_detail":  fwDetail,
			"tls_enabled":      deps.Config.UseTLS,
		})
	}
}

func handleDoctor(deps Dependencies) func(http.ResponseWriter, *http.Request, *security.SessionClaims) {
	return func(w http.ResponseWriter, r *http.Request, _ *security.SessionClaims) {
		var wsRoot string
		if deps.Explorer != nil {
			wsRoot = deps.Explorer.Root()
		}
		checks := agy.RunDoctor(r.Context(), deps.Config.AntigravityDir, wsRoot)
		writeJSON(w, http.StatusOK, map[string]any{"checks": checks})
	}
}

func handleConversations(deps Dependencies) func(http.ResponseWriter, *http.Request, *security.SessionClaims) {
	return func(w http.ResponseWriter, r *http.Request, _ *security.SessionClaims) {
		convs, err := agy.ListConversations(deps.Config.AntigravityDir, 30)
		if err != nil {
			http.Error(w, "Failed to list conversations", http.StatusInternalServerError)
			return
		}
		if deps.Dispatcher != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
			summaries, hubErr := deps.Dispatcher.GetAllTrajectories(ctx)
			cancel()
			if hubErr == nil && len(summaries) > 0 {
				for i := range convs {
					if ts, ok := summaries[convs[i].ID]; ok {
						if strings.TrimSpace(ts.Summary) != "" {
							convs[i].Title = ts.Summary
						}
						if strings.TrimSpace(ts.Status) != "" {
							convs[i].LastStatus = ts.Status
						}
					}
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversations": convs})
	}
}

func handleModels(deps Dependencies) func(http.ResponseWriter, *http.Request, *security.SessionClaims) {
	return func(w http.ResponseWriter, r *http.Request, _ *security.SessionClaims) {
		if deps.Dispatcher == nil {
			writeJSON(w, http.StatusOK, map[string]any{"models": []agy.ModelOption{}})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		models := deps.Dispatcher.ListModels(ctx)
		writeJSON(w, http.StatusOK, map[string]any{"models": models})
	}
}

func handleSendPrompt(deps Dependencies) func(http.ResponseWriter, *http.Request, *security.SessionClaims) {
	return func(w http.ResponseWriter, r *http.Request, _ *security.SessionClaims) {
		if deps.Dispatcher == nil {
			http.Error(w, "Dispatcher unavailable", http.StatusServiceUnavailable)
			return
		}
		convID := r.PathValue("id")
		if convID == "new" {
			convID = ""
		} else if !agy.IsValidConversationID(convID) {
			http.Error(w, "Invalid conversation ID", http.StatusBadRequest)
			return
		}

		var body struct {
			Prompt string `json:"prompt"`
			Mode   string `json:"mode,omitempty"`
			Model  string `json:"model,omitempty"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&body); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Prompt) == "" {
			http.Error(w, "Prompt cannot be empty", http.StatusBadRequest)
			return
		}

		var wsRoot string
		if deps.Explorer != nil {
			wsRoot = deps.Explorer.Root()
		}
		req := agy.PromptRequest{
			ConversationID: convID,
			Prompt:         body.Prompt,
			Mode:           body.Mode,
			Model:          body.Model,
			Workspace:      wsRoot,
		}
		newConvID, backend, err := deps.Dispatcher.SendPrompt(r.Context(), req)
		if err != nil {
			if deps.Audit != nil {
				deps.Audit.Log(security.AuditEvent{
					Event:    "prompt_failed",
					ClientIP: r.RemoteAddr,
					Method:   r.Method,
					Path:     r.URL.Path,
					Outcome:  "error",
					Detail:   err.Error(),
				})
			}
			http.Error(w, "Failed to dispatch prompt: "+err.Error(), http.StatusBadGateway)
			return
		}
		if deps.Audit != nil {
			deps.Audit.Log(security.AuditEvent{
				Event:    "prompt_submitted",
				ClientIP: r.RemoteAddr,
				Method:   r.Method,
				Path:     r.URL.Path,
				Outcome:  "success",
				Detail:   "conversation=" + newConvID + " via=" + backend,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"conversation_id": newConvID,
			"backend":         backend,
		})
	}
}

func handlePendingInteractions(deps Dependencies) func(http.ResponseWriter, *http.Request, *security.SessionClaims) {
	return func(w http.ResponseWriter, r *http.Request, _ *security.SessionClaims) {
		convID := r.PathValue("id")
		if !agy.IsValidConversationID(convID) {
			http.Error(w, "Invalid conversation ID", http.StatusBadRequest)
			return
		}
		if deps.Dispatcher == nil {
			writeJSON(w, http.StatusOK, map[string]any{"pending": []agy.PendingInteraction{}, "status": ""})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		pending, status, err := deps.Dispatcher.GetPendingInteractions(ctx, convID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"pending": []agy.PendingInteraction{},
				"status":  status,
				"error":   err.Error(),
			})
			return
		}
		if pending == nil {
			pending = []agy.PendingInteraction{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"pending": pending,
			"status":  status,
		})
	}
}

func handleInteract(deps Dependencies) func(http.ResponseWriter, *http.Request, *security.SessionClaims) {
	return func(w http.ResponseWriter, r *http.Request, _ *security.SessionClaims) {
		if deps.Dispatcher == nil {
			http.Error(w, "Dispatcher unavailable", http.StatusServiceUnavailable)
			return
		}
		convID := r.PathValue("id")
		if !agy.IsValidConversationID(convID) {
			http.Error(w, "Invalid conversation ID", http.StatusBadRequest)
			return
		}
		var req agy.InteractionRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.TrajectoryID) == "" || req.StepIndex < 0 {
			http.Error(w, "Missing trajectory_id or valid step_index", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		err := deps.Dispatcher.RespondInteraction(ctx, convID, req)
		eventName := "interaction_rejected"
		if req.Allow {
			eventName = "interaction_approved"
		}
		if err != nil {
			if deps.Audit != nil {
				deps.Audit.Log(security.AuditEvent{
					Event:    eventName,
					ClientIP: r.RemoteAddr,
					Method:   r.Method,
					Path:     r.URL.Path,
					Outcome:  "error",
					Detail:   err.Error(),
				})
			}
			http.Error(w, "Failed to respond to interaction: "+err.Error(), http.StatusBadGateway)
			return
		}
		if deps.Audit != nil {
			deps.Audit.Log(security.AuditEvent{
				Event:    eventName,
				ClientIP: r.RemoteAddr,
				Method:   r.Method,
				Path:     r.URL.Path,
				Outcome:  "success",
				Detail:   "conversation=" + convID + " step=" + strconv.Itoa(req.StepIndex),
			})
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func handleHubProxy(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !security.ValidateSameOrigin(r) {
			if deps.Audit != nil {
				deps.Audit.Log(security.AuditEvent{
					Event:    "origin_mismatch",
					ClientIP: r.RemoteAddr,
					Method:   r.Method,
					Path:     r.URL.Path,
					Outcome:  "denied",
					Detail:   r.Header.Get("Origin"),
				})
			}
			http.Error(w, "Forbidden: origin mismatch", http.StatusForbidden)
			return
		}
		if _, ok := deps.Sessions.VerifyRequest(r); !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if deps.HubProxy == nil {
			http.Error(w, "Antigravity Hub proxy disabled", http.StatusServiceUnavailable)
			return
		}
		deps.HubProxy.ServeHTTP(w, r)
	}
}

func handleConversationSteps(deps Dependencies) func(http.ResponseWriter, *http.Request, *security.SessionClaims) {
	return func(w http.ResponseWriter, r *http.Request, _ *security.SessionClaims) {
		convID := r.PathValue("id")
		if !agy.IsValidConversationID(convID) {
			http.Error(w, "Invalid conversation ID", http.StatusBadRequest)
			return
		}
		fromIdx, _ := strconv.Atoi(r.URL.Query().Get("from"))
		tPath := agy.TranscriptPath(deps.Config.AntigravityDir, convID, false)
		steps, err := agy.ReadTranscriptSteps(tPath, fromIdx)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, "Conversation transcript not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Failed to read transcript", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"steps": steps})
	}
}

func handleConversationWS(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !security.ValidateSameOrigin(r) {
			http.Error(w, "Forbidden: origin mismatch", http.StatusForbidden)
			return
		}
		if _, ok := deps.Sessions.VerifyRequest(r); !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		convID := r.PathValue("id")
		if !agy.IsValidConversationID(convID) {
			http.Error(w, "Invalid conversation ID", http.StatusBadRequest)
			return
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "closed")

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// Pump incoming frames (handles client close cleanly).
		go func() {
			for {
				if _, _, err := conn.Read(ctx); err != nil {
					cancel()
					return
				}
			}
		}()

		tPath := agy.TranscriptPath(deps.Config.AntigravityDir, convID, false)
		var lastSize int64 = -1
		var lastMod time.Time
		nextIndex := 0

		sendDelta := func(eventType string) error {
			info, err := os.Stat(tPath)
			if err != nil {
				return nil
			}
			if info.Size() == lastSize && info.ModTime().Equal(lastMod) {
				return nil
			}
			steps, err := agy.ReadTranscriptSteps(tPath, nextIndex)
			if err != nil {
				return nil
			}
			lastSize = info.Size()
			lastMod = info.ModTime()

			if len(steps) == 0 && eventType == "delta" {
				return nil
			}
			for _, s := range steps {
				if s.Status == "DONE" && s.StepIndex >= nextIndex {
					nextIndex = s.StepIndex + 1
				} else if s.Status != "DONE" {
					break
				}
			}
			payload, err := json.Marshal(wsEvent{
				Type:  eventType,
				Steps: steps,
			})
			if err != nil {
				return err
			}
			writeCtx, wCancel := context.WithTimeout(ctx, 5*time.Second)
			defer wCancel()
			return conn.Write(writeCtx, websocket.MessageText, payload)
		}

		if err := sendDelta("snapshot"); err != nil {
			return
		}

		ticker := time.NewTicker(350 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := sendDelta("delta"); err != nil {
					return
				}
			}
		}
	}
}

func handleWorkspaceTree(deps Dependencies) func(http.ResponseWriter, *http.Request, *security.SessionClaims) {
	return func(w http.ResponseWriter, r *http.Request, _ *security.SessionClaims) {
		if deps.Explorer == nil {
			http.Error(w, "Workspace explorer disabled", http.StatusServiceUnavailable)
			return
		}
		rel := r.URL.Query().Get("path")
		depth, _ := strconv.Atoi(r.URL.Query().Get("depth"))
		if depth <= 0 || depth > 6 {
			depth = 4
		}
		nodes, err := deps.Explorer.ListTree(rel, depth)
		if err != nil {
			handleExplorerError(w, r, deps.Audit, rel, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"root":  deps.Explorer.Root(),
			"nodes": nodes,
		})
	}
}

func handleWorkspaceFile(deps Dependencies) func(http.ResponseWriter, *http.Request, *security.SessionClaims) {
	return func(w http.ResponseWriter, r *http.Request, _ *security.SessionClaims) {
		if deps.Explorer == nil {
			http.Error(w, "Workspace explorer disabled", http.StatusServiceUnavailable)
			return
		}
		rel := r.URL.Query().Get("path")
		if strings.TrimSpace(rel) == "" {
			http.Error(w, "Missing path parameter", http.StatusBadRequest)
			return
		}
		fc, err := deps.Explorer.ReadFile(rel)
		if err != nil {
			handleExplorerError(w, r, deps.Audit, rel, err)
			return
		}
		writeJSON(w, http.StatusOK, fc)
	}
}

func handleExplorerError(w http.ResponseWriter, r *http.Request, audit *security.AuditLogger, rel string, err error) {
	switch {
	case errors.Is(err, workspace.ErrPathTraversal), errors.Is(err, workspace.ErrSensitiveFile):
		if audit != nil {
			audit.Log(security.AuditEvent{
				Event:    "workspace_access_blocked",
				ClientIP: r.RemoteAddr,
				Method:   r.Method,
				Path:     r.URL.Path,
				Outcome:  "denied",
				Detail:   rel + ": " + err.Error(),
			})
		}
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, os.ErrNotExist):
		http.Error(w, "File not found", http.StatusNotFound)
	case errors.Is(err, workspace.ErrTooLarge), errors.Is(err, workspace.ErrBinaryFile):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func spaHandler(staticFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		cleanPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if cleanPath == "" {
			cleanPath = "index.html"
		}
		if info, err := fs.Stat(staticFS, cleanPath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func ipKey(remoteAddr string) string {
	if ip := security.ExtractClientIP(remoteAddr); ip != nil {
		return ip.String()
	}
	return remoteAddr
}
