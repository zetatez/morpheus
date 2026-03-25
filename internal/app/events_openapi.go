package app

import (
	"encoding/json"
	"net/http"
)

func (s *APIServer) handleDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

		serverURL := "http://localhost" + s.runtime.cfg.Server.Listen
		doc := map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]string{
			"title":       "Morpheus API",
			"description": "Local AI Agent Runtime API",
			"version":     "1.0.0",
		},
		"servers": []map[string]string{
			{"url": serverURL, "description": "Local server"},
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
