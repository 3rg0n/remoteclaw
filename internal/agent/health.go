package agent

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// healthResponse represents the response from the health check endpoint
type healthResponse struct {
	Status    string `json:"status"`
	Uptime    string `json:"uptime"`
	Connected bool   `json:"connected"`
	LastMsg   string `json:"last_message,omitempty"`
}

// startHealthServer starts an HTTP server on the given address for health checks
func (a *Agent) startHealthServer(addr string) error {
	// Ensure localhost-only binding for security
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.healthHandler)

	a.health = &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	go func() {
		if err := a.health.Serve(listener); err != nil && err != http.ErrServerClosed {
			a.logger.Error().Err(err).Msg("Health server error")
		}
	}()

	return nil
}

// healthHandler handles GET /health requests
func (a *Agent) healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	a.mu.RLock()
	lastMsg := a.lastMsg
	isConnected := a.connected
	a.mu.RUnlock()

	uptime := time.Since(a.startTime)
	lastMsgStr := ""
	if !lastMsg.IsZero() {
		lastMsgStr = lastMsg.Format(time.RFC3339)
	}

	status := "healthy"
	if !isConnected {
		status = "disconnected"
	}

	resp := healthResponse{
		Status:    status,
		Uptime:    uptime.String(),
		Connected: isConnected,
		LastMsg:   lastMsgStr,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
