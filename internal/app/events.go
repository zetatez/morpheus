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
			"/health": map[string]interface{}{
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
			"/api/v1/chat": map[string]interface{}{
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
			"/api/v1/plan": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Generate plan",
					"operationId": "createPlan",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Plan result"},
					},
				},
			},
			"/api/v1/execute": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Execute plan",
					"operationId": "executePlan",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Execution result"},
					},
				},
			},
			"/api/v1/tasks": map[string]interface{}{
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
			"/api/v1/tasks/{id}": map[string]interface{}{
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
			"/api/v1/sessions": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List sessions",
					"operationId": "listSessions",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Session list"},
					},
				},
			},
			"/api/v1/sessions/{id}": map[string]interface{}{
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
			"/api/v1/sessions/{id}/fork": map[string]interface{}{
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
			"/api/v1/sessions/{id}/share": map[string]interface{}{
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
			"/api/v1/sessions/{id}/summarize": map[string]interface{}{
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
			"/api/v1/skills": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List skills",
					"operationId": "listSkills",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Skills list"},
					},
				},
			},
			"/api/v1/skills/{name}": map[string]interface{}{
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
			"/api/v1/models": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List models",
					"operationId": "listModels",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Models list"},
					},
				},
			},
			"/api/v1/models/select": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Select model",
					"operationId": "selectModel",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Model selected"},
					},
				},
			},
			"/api/v1/runs": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List runs",
					"operationId": "listRuns",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Runs list"},
					},
				},
			},
			"/api/v1/runs/{id}": map[string]interface{}{
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
			"/api/v1/metrics": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get server metrics",
					"operationId": "getMetrics",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Metrics data"},
					},
				},
			},
			"/api/v1/remote-file": map[string]interface{}{
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
			"/api/v1/ssh-info": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get SSH info",
					"operationId": "getSSHInfo",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "SSH info"},
					},
				},
			},
			"/api/v1/ws": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "WebSocket endpoint",
					"operationId": "websocket",
					"responses": map[string]interface{}{
						"101": map[string]interface{}{"description": "Switching protocols"},
					},
				},
			},
			"/repl": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "REPL endpoint",
					"operationId": "repl",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "REPL response"},
					},
				},
			},
			"/repl/stream": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Streaming REPL",
					"operationId": "replStream",
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
			"/config": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get configuration",
					"operationId": "getConfig",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Config data"},
					},
				},
				"patch": map[string]interface{}{
					"summary":     "Update configuration",
					"operationId": "updateConfig",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Config updated"},
					},
				},
			},
			"/permission": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List pending permissions",
					"operationId": "listPermissions",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Permission list"},
					},
				},
			},
			"/permission/{requestID}/reply": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Reply to permission request",
					"operationId": "replyPermission",
					"parameters": []map[string]interface{}{
						{"name": "requestID", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"reply": map[string]string{"type": "string", "enum": "allow,deny"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Reply sent"},
					},
				},
			},
			"/mcp": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get MCP servers status",
					"operationId": "getMCPServers",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "MCP servers status"},
					},
				},
				"post": map[string]interface{}{
					"summary":     "Add MCP server",
					"operationId": "addMCPServer",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"name":      map[string]string{"type": "string"},
										"transport": map[string]string{"type": "string"},
										"command":   map[string]string{"type": "string"},
										"url":       map[string]string{"type": "string"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"201": map[string]interface{}{"description": "MCP server added"},
					},
				},
			},
			"/mcp/{name}/connect": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Connect MCP server",
					"operationId": "connectMCPServer",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Connected"},
					},
				},
			},
			"/mcp/{name}/disconnect": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Disconnect MCP server",
					"operationId": "disconnectMCPServer",
					"parameters": []map[string]interface{}{
						{"name": "name", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Disconnected"},
					},
				},
			},
			"/find": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Text search",
					"operationId": "textSearch",
					"parameters": []map[string]interface{}{
						{"name": "pattern", "in": "query", "required": true, "schema": map[string]string{"type": "string"}},
						{"name": "path", "in": "query", "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Search results"},
					},
				},
			},
			"/find/file": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "File search",
					"operationId": "fileSearch",
					"parameters": []map[string]interface{}{
						{"name": "query", "in": "query", "required": true, "schema": map[string]string{"type": "string"}},
						{"name": "limit", "in": "query", "schema": map[string]string{"type": "integer"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "File results"},
					},
				},
			},
			"/find/symbol": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Symbol search",
					"operationId": "symbolSearch",
					"parameters": []map[string]interface{}{
						{"name": "query", "in": "query", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Symbol results"},
					},
				},
			},
			"/file": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List files",
					"operationId": "listFiles",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "File list"},
					},
				},
			},
			"/file/content": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Read file content",
					"operationId": "readFileContent",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "schema": map[string]string{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "File content"},
					},
				},
			},
			"/file/status": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Git status",
					"operationId": "gitStatus",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Git status"},
					},
				},
			},
			"/project": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List projects",
					"operationId": "listProjects",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Project list"},
					},
				},
			},
			"/project/current": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get current project",
					"operationId": "getCurrentProject",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Current project"},
					},
				},
			},
			"/project/git/init": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Initialize git repository",
					"operationId": "gitInit",
					"requestBody": map[string]interface{}{
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"directory": map[string]string{"type": "string"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"201": map[string]interface{}{"description": "Git initialized"},
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
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *APIServer) handleSessionFork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	sessionID = strings.TrimSuffix(sessionID, "/fork")
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
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	sessionID = strings.TrimSuffix(sessionID, "/share")

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
		"share_url":  fmt.Sprintf("/api/v1/sessions/%s", sessionID),
	})
}

func (s *APIServer) handleSessionSummarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	sessionID = strings.TrimSuffix(sessionID, "/summarize")
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

	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
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

	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
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

	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
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

	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
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

	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
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

	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
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

func (s *APIServer) handleSessionMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
	sessionID = strings.TrimSuffix(sessionID, "/message")
	if sessionID == "" {
		sessionID = strings.TrimPrefix(r.URL.Path, "/session/")
		sessionID = strings.TrimSuffix(sessionID, "/message")
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

	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
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

	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
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

	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
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
