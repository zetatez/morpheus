package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	fstool "github.com/zetatez/morpheus/internal/tools/fs"
)

type globalEvent struct {
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
}

type eventBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan globalEvent]struct{}
}

func newEventBroadcaster() *eventBroadcaster {
	return &eventBroadcaster{
		subscribers: make(map[chan globalEvent]struct{}),
	}
}

func (b *eventBroadcaster) subscribe() chan globalEvent {
	ch := make(chan globalEvent, 64)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *eventBroadcaster) unsubscribe(ch chan globalEvent) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
}

func (b *eventBroadcaster) broadcast(event globalEvent) {
	b.mu.RLock()
	subs := make([]chan globalEvent, 0, len(b.subscribers))
	for ch := range b.subscribers {
		subs = append(subs, ch)
	}
	b.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *APIServer) handleGlobalEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.eventBroadcaster.subscribe()
	defer s.eventBroadcaster.unsubscribe(ch)

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	closeNotify := w.(http.CloseNotifier).CloseNotify()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-closeNotify:
			return
		case <-ticker.C:
			_, err := w.Write([]byte(": ping\n\n"))
			if err != nil {
				return
			}
			flusher.Flush()
		case event := <-ch:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, err = w.Write([]byte("data: "))
			if err != nil {
				return
			}
			_, err = w.Write(data)
			if err != nil {
				return
			}
			_, err = w.Write([]byte("\n\n"))
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *APIServer) handleGlobalSyncEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.eventBroadcaster.subscribe()
	defer s.eventBroadcaster.unsubscribe(ch)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	closeNotify := w.(http.CloseNotifier).CloseNotify()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-closeNotify:
			return
		case <-ticker.C:
			_, err := w.Write([]byte(": ping\n\n"))
			if err != nil {
				return
			}
			flusher.Flush()
		case event := <-ch:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, err = w.Write([]byte("data: "))
			if err != nil {
				return
			}
			_, err = w.Write(data)
			if err != nil {
				return
			}
			_, err = w.Write([]byte("\n\n"))
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *APIServer) broadcastEvent(eventType string, data map[string]interface{}) {
	s.eventBroadcaster.broadcast(globalEvent{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
	})
}

func (s *APIServer) handleDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	doc := map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]string{
			"title":       "Morpheus API",
			"description": "Local AI Agent Runtime API",
			"version":     "1.0.0",
		},
		"servers": []map[string]string{
			{"url": "http://localhost:8080", "description": "Local server"},
		},
		"paths": map[string]interface{}{
			"/global/health": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Health check",
					"operationId": "getHealth",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "OK"},
					},
				},
			},
			"/event": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "SSE event stream for all events",
					"operationId": "getEventStream",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "SSE stream",
							"content": map[string]interface{}{
								"text/event-stream": map[string]interface{}{},
							},
						},
					},
				},
			},
			"/shell": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Execute shell command",
					"operationId": "executeShell",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"command": map[string]string{"type": "string"},
										"workdir": map[string]string{"type": "string"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Command result"},
					},
				},
			},
			"/chat": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Chat with agent",
					"operationId": "chat",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"session":     map[string]string{"type": "string"},
										"input":       map[string]string{"type": "string"},
										"mode":        map[string]string{"type": "string"},
										"attachments": map[string]interface{}{"type": "array"},
										"format":      map[string]string{"type": "string"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Chat response"},
					},
				},
			},
			"/plan": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Generate plan",
					"operationId": "createPlan",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Plan result"},
					},
				},
			},
			"/execute": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Execute plan",
					"operationId": "executePlan",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Execution result"},
					},
				},
			},
			"/tasks/": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List tasks",
					"operationId": "listTasks",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Task list"},
					},
				},
				"post": map[string]interface{}{
					"summary":     "Create task",
					"operationId": "createTask",
					"responses": map[string]interface{}{
						"201": map[string]interface{}{"description": "Task created"},
					},
				},
			},
			"/tasks/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get task",
					"operationId": "getTask",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Task details"},
					},
				},
				"delete": map[string]interface{}{
					"summary":     "Cancel task",
					"operationId": "cancelTask",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Task cancelled"},
					},
				},
			},
			"/session/": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List sessions",
					"operationId": "listSessions",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Session list"},
					},
				},
			},
			"/session/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get session",
					"operationId": "getSession",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Session details"},
					},
				},
				"delete": map[string]interface{}{
					"summary":     "Delete session",
					"operationId": "deleteSession",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Session deleted"},
					},
				},
			},
			"/sessions/{id}/fork": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Fork session",
					"operationId": "forkSession",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"201": map[string]interface{}{"description": "Session forked"},
					},
				},
			},
			"/sessions/{id}/share": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Share session",
					"operationId": "shareSession",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Session shared"},
					},
				},
				"delete": map[string]interface{}{
					"summary":     "Unshare session",
					"operationId": "unshareSession",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Session unshared"},
					},
				},
			},
			"/sessions/{id}/summarize": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Summarize session",
					"operationId": "summarizeSession",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Session summary"},
					},
				},
			},
			"/skill": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List skills",
					"operationId": "listSkills",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Skills list"},
					},
				},
			},
			"/skill/{name}": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Load skill",
					"operationId": "loadSkill",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Skill loaded"},
					},
				},
			},
			"/models/": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List models",
					"operationId": "listModels",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Models list"},
					},
				},
			},
			"/models/select": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Select model",
					"operationId": "selectModel",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Model selected"},
					},
				},
			},
			"/runs/": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List runs",
					"operationId": "listRuns",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Runs list"},
					},
				},
			},
			"/runs/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get run",
					"operationId": "getRun",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Run details"},
					},
				},
			},
			"/metrics": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get server metrics",
					"operationId": "getMetrics",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Metrics data"},
					},
				},
			},
			"/vim": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Read remote file",
					"operationId": "readRemoteFile",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "File content"},
					},
				},
				"post": map[string]interface{}{
					"summary":     "Write remote file",
					"operationId": "writeRemoteFile",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"path":          map[string]string{"type": "string"},
										"content":       map[string]string{"type": "string"},
										"expected_hash": map[string]string{"type": "string"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "File written"},
					},
				},
			},
			"/ssh": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get SSH info",
					"operationId": "getSSHInfo",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "SSH info"},
					},
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

func (s *APIServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.runtime.cfg)
	case http.MethodPatch:
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		s.handleConfigUpdate(w, updates)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) handleConfigUpdate(w http.ResponseWriter, updates map[string]interface{}) {
	if updates == nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "no updates provided"})
		return
	}

	if logLevel, ok := updates["logging"]; ok {
		if logLevelMap, ok := logLevel.(map[string]interface{}); ok {
			if level, ok := logLevelMap["level"].(string); ok {
				s.runtime.logger.Info("log level update requested", zap.String("level", level))
			}
		}
	}

	if serverLimits, ok := updates["server"]; ok {
		if serverLimitsMap, ok := serverLimits.(map[string]interface{}); ok {
			if limits, ok := serverLimitsMap["limits"]; ok {
				if limitsMap, ok := limits.(map[string]interface{}); ok {
					s.runtime.cfg.Server.Limits.Enabled = true
					if maxCPU, ok := limitsMap["max_cpu_percent"].(float64); ok {
						s.runtime.cfg.Server.Limits.MaxCPUPercent = maxCPU
					}
					if maxMem, ok := limitsMap["max_memory_percent"].(float64); ok {
						s.runtime.cfg.Server.Limits.MaxMemoryPercent = maxMem
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "updated": updates})
}

func (s *APIServer) handleConfigProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	providers := []map[string]interface{}{
		{
			"id":       "openai",
			"name":     "OpenAI",
			"requires": []string{"api_key"},
		},
		{
			"id":       "deepseek",
			"name":     "DeepSeek",
			"requires": []string{"api_key"},
		},
		{
			"id":       "anthropic",
			"name":     "Anthropic",
			"requires": []string{"api_key"},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"providers": providers})
}

func (s *APIServer) handleProviderOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	providerID := strings.TrimPrefix(r.URL.Path, "/provider/")
	providerID = strings.TrimSuffix(providerID, "/oauth/authorize")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"provider_id": providerID,
		"auth_url":    fmt.Sprintf("https://example.com/oauth/authorize?provider=%s", providerID),
	})
}

func (s *APIServer) handleProviderOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	providerID := strings.TrimPrefix(r.URL.Path, "/provider/")
	providerID = strings.TrimSuffix(providerID, "/oauth/callback")

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"provider_id": providerID,
		"status":      "authenticated",
	})
}

func (s *APIServer) handleMCPAuth(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/mcp/")
	name = strings.TrimSuffix(name, "/auth")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
		return
	}

	switch r.Method {
	case http.MethodPost:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":       true,
			"name":     name,
			"auth_url": fmt.Sprintf("https://example.com/mcp/%s/auth/start", name),
		})
	case http.MethodDelete:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":     true,
			"name":   name,
			"status": "auth_removed",
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) handleMCPAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/mcp/")
	name = strings.TrimSuffix(name, "/auth/callback")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"name":   name,
		"status": "authenticated",
	})
}

func (s *APIServer) handleMCPAuthenticate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/mcp/")
	name = strings.TrimSuffix(name, "/auth/authenticate")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"name":   name,
		"status": "authenticating",
	})
}

func (s *APIServer) handlePermission(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listPermissions(w)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) listPermissions(w http.ResponseWriter) {
	permissions := []map[string]interface{}{}
	s.runtime.pendingConfirmations.Range(func(key, value interface{}) bool {
		sessionID := key.(string)
		pc := value.(*pendingConfirmation)
		permissions = append(permissions, map[string]interface{}{
			"request_id": sessionID,
			"tool":       pc.Tool,
			"inputs":     pc.Inputs,
			"kind":       pc.Kind,
		})
		return true
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"permissions": permissions})
}

func (s *APIServer) handlePermissionReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	requestID := strings.TrimPrefix(r.URL.Path, "/permission/")
	requestID = strings.TrimSuffix(requestID, "/reply")
	if requestID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request_id is required"})
		return
	}

	var body struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	allow := strings.ToLower(strings.TrimSpace(body.Reply)) == "allow"
	_, exists := s.runtime.pendingConfirmations.LoadAndDelete(requestID)
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "pending confirmation not found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "request_id": requestID, "decision": map[string]bool{"allowed": allow}})
}

func (s *APIServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleMCPStatus(w)
	case http.MethodPost:
		s.handleMCPAdd(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) handleMCPStatus(w http.ResponseWriter) {
	servers := []map[string]interface{}{}
	s.runtime.mcpSessions.Range(func(key, value interface{}) bool {
		name := key.(string)
		servers = append(servers, map[string]interface{}{
			"name":   name,
			"status": "connected",
		})
		return true
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"servers": servers})
}

func (s *APIServer) handleMCPAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		Transport string `json:"transport"`
		Command   string `json:"command"`
		URL       string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if req.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
		return
	}

	transport := strings.TrimSpace(req.Transport)
	if transport == "" {
		transport = "stdio"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":        true,
		"name":      req.Name,
		"transport": transport,
		"command":   req.Command,
		"url":       req.URL,
	})
}

func (s *APIServer) handleMCPConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/mcp/")
	name = strings.TrimSuffix(name, "/connect")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "name": name, "status": "connecting"})
}

func (s *APIServer) handleMCPDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/mcp/")
	name = strings.TrimSuffix(name, "/disconnect")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "name": name, "status": "disconnected"})
}

func (s *APIServer) handleFind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		path = s.runtime.cfg.WorkspaceRoot
	}

	if pattern == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "pattern is required"})
		return
	}

	results, err := s.doGrep(path, pattern)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
}

func (s *APIServer) doGrep(rootDir, pattern string) ([]map[string]interface{}, error) {
	var results []map[string]interface{}

	cmd := exec.Command("grep", "-rn", "--include=*.go", "--include=*.ts", "--include=*.js", "--include=*.py", "--include=*.md", pattern, rootDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	_ = cmd.Run()
	output := stdout.String()
	if output != "" {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, ":", 3)
			if len(parts) >= 3 {
				results = append(results, map[string]interface{}{
					"file": parts[0],
					"line": parts[1],
					"text": parts[2],
				})
			}
		}
	}

	_ = stderr.String()

	return results, nil
}

func (s *APIServer) handleFindFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	limitStr := strings.TrimSpace(r.URL.Query().Get("limit"))
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	if query == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "query is required"})
		return
	}

	results, err := s.doFileSearch(query, limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"files": results})
}

func (s *APIServer) doFileSearch(query string, limit int) ([]string, error) {
	cmd := exec.Command("find", s.runtime.cfg.WorkspaceRoot, "-type", "f", "-name", "*"+query+"*")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run()

	results := []string{}
	lines := strings.Split(stdout.String(), "\n")
	for _, line := range lines {
		if line != "" && len(results) < limit {
			results = append(results, line)
		}
	}

	return results, nil
}

func (s *APIServer) handleFindSymbol(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "query is required"})
		return
	}

	results, err := s.doSymbolSearch(query)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"symbols": results})
}

func (s *APIServer) doSymbolSearch(query string) ([]map[string]interface{}, error) {
	grepCmd := exec.Command("grep", "-rn", "-E", "^(func |type |struct |interface |const |var )\\s*"+query, s.runtime.cfg.WorkspaceRoot)
	var stdout bytes.Buffer
	grepCmd.Stdout = &stdout
	_ = grepCmd.Run()

	results := []map[string]interface{}{}
	lines := strings.Split(stdout.String(), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) >= 3 {
			results = append(results, map[string]interface{}{
				"file": parts[0],
				"line": parts[1],
				"text": parts[2],
			})
		}
	}

	return results, nil
}

func (s *APIServer) handleFileList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		path = s.runtime.cfg.WorkspaceRoot
	}

	files, err := s.listDirectory(path)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"path": path, "files": files})
}

func (s *APIServer) listDirectory(dir string) ([]map[string]interface{}, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := []map[string]interface{}{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, map[string]interface{}{
			"name":    entry.Name(),
			"size":    info.Size(),
			"isdir":   entry.IsDir(),
			"modtime": info.ModTime(),
		})
	}
	return files, nil
}

func (s *APIServer) handleFileContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
		return
	}

	absPath, err := fstool.NewReadTool(s.runtime.cfg.WorkspaceRoot).ResolveForAPI(path)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"path":    path,
		"content": string(content),
		"size":    len(content),
		"hash":    sha256Hex(content),
	})
}

func (s *APIServer) handleFileStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = s.runtime.cfg.WorkspaceRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	_ = cmd.Run()
	status := stdout.String()

	w.Header().Set("Content-Type", "application/json")

	changed := []map[string]string{}
	lines := strings.Split(status, "\n")
	for _, line := range lines {
		if len(line) >= 3 {
			changed = append(changed, map[string]string{
				"status": line[:2],
				"file":   strings.TrimSpace(line[2:]),
			})
		}
	}

	_ = stderr.String()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"changed": changed,
		"clean":   status == "",
	})
}

func (s *APIServer) handleProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"projects": []map[string]interface{}{
			{
				"id":         "default",
				"name":       filepath.Base(s.runtime.cfg.WorkspaceRoot),
				"path":       s.runtime.cfg.WorkspaceRoot,
				"is_current": true,
			},
		},
	})
}

func (s *APIServer) handleProjectCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         "default",
		"name":       filepath.Base(s.runtime.cfg.WorkspaceRoot),
		"path":       s.runtime.cfg.WorkspaceRoot,
		"is_current": true,
	})
}

func (s *APIServer) handleProjectGitInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Directory string `json:"directory"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body.Directory = s.runtime.cfg.WorkspaceRoot
	}

	if body.Directory == "" {
		body.Directory = s.runtime.cfg.WorkspaceRoot
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = body.Directory
	err := cmd.Run()

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":        true,
		"directory": body.Directory,
		"message":   "Git repository initialized",
	})
}

func (s *APIServer) handleSessionAction(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/fork"):
		s.handleSessionFork(w, r)
	case strings.HasSuffix(path, "/share"):
		s.handleSessionShare(w, r)
	case strings.HasSuffix(path, "/summarize"):
		s.handleSessionSummarize(w, r)
	case strings.HasSuffix(path, "/children"):
		s.handleSessionChildren(w, r)
	case strings.HasSuffix(path, "/todo"):
		s.handleSessionTodo(w, r)
	case strings.HasSuffix(path, "/init"):
		s.handleSessionInit(w, r)
	case strings.HasSuffix(path, "/abort"):
		s.handleSessionAbort(w, r)
	case strings.HasSuffix(path, "/revert"):
		s.handleSessionRevert(w, r)
	case strings.HasSuffix(path, "/unrevert"):
		s.handleSessionUnrevert(w, r)
	case strings.HasSuffix(path, "/message"):
		s.handleSessionMessage(w, r)
	case strings.HasSuffix(path, "/diff"):
		s.handleSessionDiff(w, r)
	case strings.HasSuffix(path, "/command"):
		s.handleSessionCommand(w, r)
	case strings.HasSuffix(path, "/shell"):
		s.handleSessionShell(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *APIServer) handleSessionFork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := extractSessionID(r.URL.Path)
	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session_id is required"})
		return
	}

	newSessionID := "forked-" + sessionID + "-" + fmt.Sprintf("%d", time.Now().Unix())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"original_id": sessionID,
		"forked_id":   newSessionID,
		"session_id":  newSessionID,
	})
}

func (s *APIServer) handleSessionShare(w http.ResponseWriter, r *http.Request) {
	sessionID := extractSessionID(r.URL.Path)

	if r.Method == http.MethodDelete {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "session_id": sessionID, "shared": false})
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"session_id": sessionID,
		"shared":     true,
		"share_url":  fmt.Sprintf("/session/%s", sessionID),
	})
}

func (s *APIServer) handleSessionSummarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := extractSessionID(r.URL.Path)
	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session_id is required"})
		return
	}

	summary := s.runtime.conversation.Summary(sessionID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"session_id": sessionID,
		"summary":    summary,
	})
}

func (s *APIServer) handleQuestionReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	requestID := strings.TrimPrefix(r.URL.Path, "/question/")
	requestID = strings.TrimSuffix(requestID, "/reject")
	if requestID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request_id is required"})
		return
	}

	_, exists := s.runtime.pendingConfirmations.LoadAndDelete(requestID)
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "question not found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "request_id": requestID, "rejected": true})
}

func (s *APIServer) handleSessionLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/session/")
	sessionID = strings.TrimSuffix(sessionID, "/load")
	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session_id is required"})
		return
	}

	if err := s.runtime.LoadSession(r.Context(), sessionID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": sessionID})
}

func (s *APIServer) handleProjectList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"projects": []map[string]interface{}{
			{
				"id":         "default",
				"name":       filepath.Base(s.runtime.cfg.WorkspaceRoot),
				"path":       s.runtime.cfg.WorkspaceRoot,
				"is_current": true,
			},
		},
	})
}

func (s *APIServer) handleSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleSessionList(w, r)
	case http.MethodPost:
		s.handleSessionCreate(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	sessionID := "session-" + fmt.Sprintf("%d", time.Now().Unix())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         sessionID,
		"session_id": sessionID,
		"created_at": time.Now(),
	})
}

func (s *APIServer) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "active",
		"timestamp": time.Now(),
	})
}

func (s *APIServer) handleProjectByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/project/")
	parts := strings.Split(path, "/")
	projectID := parts[0]

	if projectID == "" {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"projects": []map[string]interface{}{
					{
						"id":         "default",
						"name":       filepath.Base(s.runtime.cfg.WorkspaceRoot),
						"path":       s.runtime.cfg.WorkspaceRoot,
						"is_current": true,
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "project_id is required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         projectID,
			"name":       projectID,
			"path":       s.runtime.cfg.WorkspaceRoot,
			"is_current": projectID == "default",
		})
	case http.MethodPatch:
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      true,
			"id":      projectID,
			"updated": updates,
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) handleSessionMessage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.Contains(path, "/message/") {
		s.handleSessionMessageByID(w, r)
		return
	}

	sessionID := extractSessionIDFromPath(path, "/session/", "/message")
	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session_id is required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleSessionMessageList(w, r, sessionID)
	case http.MethodPost:
		s.handleSessionMessageSend(w, r, sessionID)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) handleSessionMessageByID(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/message/")
	if len(parts) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid path"})
		return
	}

	sessionMsgPart := parts[1]
	msgParts := strings.SplitN(sessionMsgPart, "/", 2)
	messageID := msgParts[0]

	sessionID := extractSessionIDFromPath(parts[0], "/session/", "")
	if sessionID == "" {
		sessionID = "default"
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"session_id": sessionID,
			"message_id": messageID,
		})
	case http.MethodDelete:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":         true,
			"session_id": sessionID,
			"message_id": messageID,
			"deleted":    true,
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) handleSessionMessagePart(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/message/")
	if len(parts) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	msgPart := parts[1]
	msgParts := strings.SplitN(msgPart, "/part/", 2)
	if len(msgParts) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	messageID := msgParts[0]
	partID := msgParts[1]
	sessionID := extractSessionIDFromPath(parts[0], "/session/", "")
	if sessionID == "" {
		sessionID = "default"
	}

	switch r.Method {
	case http.MethodPatch:
		var body struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":         true,
			"session_id": sessionID,
			"message_id": messageID,
			"part_id":    partID,
			"type":       body.Type,
			"text":       body.Text,
		})
	case http.MethodDelete:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":         true,
			"session_id": sessionID,
			"message_id": messageID,
			"part_id":    partID,
			"deleted":    true,
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func extractSessionIDFromPath(path, prefix, suffix string) string {
	if idx := strings.Index(path, prefix); idx == 0 {
		sessionID := strings.TrimPrefix(path, prefix)
		if suffix != "" {
			sessionID = strings.TrimSuffix(sessionID, suffix)
			sessionID = strings.TrimSuffix(sessionID, "/")
		}
		parts := strings.SplitN(sessionID, "/", 2)
		return parts[0]
	}
	return ""
}

func extractSessionID(path string) string {
	paths := []string{"/session/", "/session/"}
	for _, prefix := range paths {
		if idx := strings.Index(path, prefix); idx == 0 {
			sessionID := strings.TrimPrefix(path, prefix)
			parts := strings.Split(sessionID, "/")
			return parts[0]
		}
	}
	return ""
}

func (s *APIServer) handleQuestion(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listQuestions(w)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) listQuestions(w http.ResponseWriter) {
	questions := []map[string]interface{}{}
	s.runtime.pendingConfirmations.Range(func(key, value interface{}) bool {
		sessionID := key.(string)
		pc := value.(*pendingConfirmation)
		if pc.Kind == "question" {
			questions = append(questions, map[string]interface{}{
				"request_id": sessionID,
				"tool":       pc.Tool,
				"inputs":     pc.Inputs,
				"kind":       pc.Kind,
			})
		}
		return true
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"questions": questions})
}

func (s *APIServer) handleQuestionReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	requestID := strings.TrimPrefix(r.URL.Path, "/question/")
	requestID = strings.TrimSuffix(requestID, "/reply")
	if requestID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "request_id is required"})
		return
	}

	var body struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_, exists := s.runtime.pendingConfirmations.LoadAndDelete(requestID)
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "question not found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "request_id": requestID, "answer": body.Answer})
}

func (s *APIServer) handleProvider(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listProviders(w)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) listProviders(w http.ResponseWriter) {
	providers := []map[string]interface{}{
		{
			"id":     "openai",
			"name":   "OpenAI",
			"models": []string{"gpt-4", "gpt-4-turbo", "gpt-3.5-turbo"},
		},
		{
			"id":     "deepseek",
			"name":   "DeepSeek",
			"models": []string{"deepseek-chat", "deepseek-coder"},
		},
		{
			"id":     "anthropic",
			"name":   "Anthropic",
			"models": []string{"claude-3-opus", "claude-3-sonnet", "claude-3-haiku"},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"providers": providers})
}

func (s *APIServer) handleProviderAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	authMethods := []map[string]string{
		{"method": "api_key", "description": "API Key authentication"},
		{"method": "oauth", "description": "OAuth 2.0 authentication"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"auth_methods": authMethods})
}

func (s *APIServer) handleSessionChildren(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/session/")
	sessionID = strings.TrimSuffix(sessionID, "/children")
	if sessionID == "" {
		sessionID = strings.TrimPrefix(r.URL.Path, "/session/")
		sessionID = strings.TrimSuffix(sessionID, "/children")
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": sessionID,
		"children":   []string{},
	})
}

func (s *APIServer) handleSessionTodo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/session/")
	sessionID = strings.TrimSuffix(sessionID, "/todo")
	if sessionID == "" {
		sessionID = strings.TrimPrefix(r.URL.Path, "/session/")
		sessionID = strings.TrimSuffix(sessionID, "/todo")
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": sessionID,
		"todos":      []map[string]string{},
	})
}

func (s *APIServer) handleSessionInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/session/")
	sessionID = strings.TrimSuffix(sessionID, "/init")
	if sessionID == "" {
		sessionID = strings.TrimPrefix(r.URL.Path, "/session/")
		sessionID = strings.TrimSuffix(sessionID, "/init")
	}

	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session_id is required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"session_id": sessionID,
		"message":    "session initialized",
	})
}

func (s *APIServer) handleSessionAbort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/session/")
	sessionID = strings.TrimSuffix(sessionID, "/abort")
	if sessionID == "" {
		sessionID = strings.TrimPrefix(r.URL.Path, "/session/")
		sessionID = strings.TrimSuffix(sessionID, "/abort")
	}

	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session_id is required"})
		return
	}

	run, ok := s.runtime.runs.latestBySession(sessionID)
	if ok {
		s.runtime.runs.cancel(run.ID)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"session_id": sessionID,
		"aborted":    true,
	})
}

func (s *APIServer) handleSessionRevert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/session/")
	sessionID = strings.TrimSuffix(sessionID, "/revert")
	if sessionID == "" {
		sessionID = strings.TrimPrefix(r.URL.Path, "/session/")
		sessionID = strings.TrimSuffix(sessionID, "/revert")
	}

	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session_id is required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"session_id": sessionID,
		"reverted":   true,
	})
}

func (s *APIServer) handleSessionUnrevert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/session/")
	sessionID = strings.TrimSuffix(sessionID, "/unrevert")
	if sessionID == "" {
		sessionID = strings.TrimPrefix(r.URL.Path, "/session/")
		sessionID = strings.TrimSuffix(sessionID, "/unrevert")
	}

	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session_id is required"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"session_id": sessionID,
		"unreverted": true,
	})
}

func (s *APIServer) handleSessionMessageList(w http.ResponseWriter, r *http.Request, sessionID string) {
	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session_id is required"})
		return
	}

	messages := s.runtime.conversation.History(r.Context(), sessionID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": sessionID,
		"messages":   messages,
	})
}

func (s *APIServer) handleSessionMessageSend(w http.ResponseWriter, r *http.Request, sessionID string) {
	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session_id is required"})
		return
	}

	var body struct {
		Prompt  string `json:"prompt"`
		Context string `json:"context"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"session_id": sessionID,
		"message_id": fmt.Sprintf("msg-%d", time.Now().Unix()),
	})
}

func (s *APIServer) handleSessionDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/session/")
	sessionID = strings.TrimSuffix(sessionID, "/diff")
	if sessionID == "" {
		sessionID = strings.TrimPrefix(r.URL.Path, "/session/")
		sessionID = strings.TrimSuffix(sessionID, "/diff")
	}

	messageID := r.URL.Query().Get("messageID")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": sessionID,
		"message_id": messageID,
		"diff":       "",
	})
}

func (s *APIServer) handleSessionCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/session/")
	sessionID = strings.TrimSuffix(sessionID, "/command")
	if sessionID == "" {
		sessionID = strings.TrimPrefix(r.URL.Path, "/session/")
		sessionID = strings.TrimSuffix(sessionID, "/command")
	}

	var body struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"session_id": sessionID,
		"command":    body.Command,
	})
}

func (s *APIServer) handleSessionShell(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/session/")
	sessionID = strings.TrimSuffix(sessionID, "/shell")
	if sessionID == "" {
		sessionID = strings.TrimPrefix(r.URL.Path, "/session/")
		sessionID = strings.TrimSuffix(sessionID, "/shell")
	}

	var body struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"session_id": sessionID,
		"command":    body.Command,
	})
}
