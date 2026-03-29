package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

type remoteWSRequest struct {
	Type        string            `json:"type"`
	SessionID   string            `json:"session_id,omitempty"`
	Input       string            `json:"input,omitempty"`
	RunID       string            `json:"run_id,omitempty"`
	AfterSeq    int64             `json:"after_seq,omitempty"`
	Limit       int               `json:"limit,omitempty"`
	Attachments []InputAttachment `json:"attachments,omitempty"`
	Format      *OutputFormat     `json:"format,omitempty"`
	Mode        string            `json:"mode,omitempty"`
}

type remoteWSMessage struct {
	Type  string         `json:"type"`
	Data  map[string]any `json:"data,omitempty"`
	Error string         `json:"error,omitempty"`
}

var remoteWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *APIServer) handleRemoteWS(w http.ResponseWriter, r *http.Request) {
	if !s.runtime.cfg.Server.Remote.Enabled {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if !s.authorizeRemote(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}
	conn, err := remoteWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	clientID := uuid.NewString()
	s.logger.Info("remote ws connected", zap.String("client_id", clientID), zap.String("remote", r.RemoteAddr))
	_ = conn.WriteJSON(remoteWSMessage{Type: "ready", Data: map[string]any{"client_id": clientID, "server_time": time.Now().UTC()}})
	for {
		var req remoteWSRequest
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		if err := s.handleRemoteWSRequest(r.Context(), conn, req); err != nil {
			_ = conn.WriteJSON(remoteWSMessage{Type: "error", Error: err.Error()})
		}
	}
}

func (s *APIServer) authorizeRemote(r *http.Request) bool {
	token := strings.TrimSpace(s.runtime.cfg.Server.Remote.BearerToken)
	if token == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:]) == token
	}
	return strings.TrimSpace(r.URL.Query().Get("token")) == token
}

func (s *APIServer) handleRemoteWSRequest(ctx context.Context, conn *websocket.Conn, req remoteWSRequest) error {
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "default"
	}
	switch strings.ToLower(strings.TrimSpace(req.Type)) {
	case "ping":
		return conn.WriteJSON(remoteWSMessage{Type: "pong", Data: map[string]any{"time": time.Now().UTC()}})
	case "session.get":
		messages := s.runtime.conversation.History(ctx, sessionID)
		return conn.WriteJSON(remoteWSMessage{Type: "session", Data: map[string]any{"session_id": sessionID, "messages": messages, "summary": s.runtime.conversation.Summary(sessionID)}})
	case "run.start":
		if strings.TrimSpace(req.Input) == "" {
			return fmt.Errorf("input is required")
		}
		mode := s.runtime.defaultAgentMode()
		if strings.TrimSpace(req.Mode) != "" {
			mode = normalizeAgentMode(AgentMode(req.Mode))
		}
		run := s.runtime.startRun(sessionID, UserInput{Text: req.Input, Attachments: req.Attachments}, req.Format, mode)
		go func() {
			_, _ = s.runtime.runAgentLoopWithRun(context.Background(), run, sessionID, UserInput{Text: req.Input, Attachments: req.Attachments}, req.Format, mode, runnerCallbacks{callChat: s.runtime.callChatWithTools})
		}()
		return conn.WriteJSON(remoteWSMessage{Type: "run.started", Data: map[string]any{"session_id": sessionID, "run_id": run.ID, "run_status": run.Status}})
	case "run.events":
		if strings.TrimSpace(req.RunID) == "" {
			return fmt.Errorf("run_id is required")
		}
		limit := req.Limit
		if limit <= 0 || limit > 1000 {
			limit = 200
		}
		count := 0
		emit := func(event string, data interface{}) error {
			if count >= limit {
				return nil
			}
			count++
			payload, _ := data.(map[string]any)
			return conn.WriteJSON(remoteWSMessage{Type: event, Data: payload})
		}
		if err := s.runtime.replayRunEvents(ctx, req.RunID, req.AfterSeq, emit); err != nil {
			return err
		}
		return conn.WriteJSON(remoteWSMessage{Type: "run.events.complete", Data: map[string]any{"run_id": req.RunID}})
	default:
		return fmt.Errorf("unsupported remote message type: %s", req.Type)
	}
}
