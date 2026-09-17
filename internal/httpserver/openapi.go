package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/session"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/tools"
)

// inProcClient opens an in-memory MCP client session against the server bound
// to sess. It is used by the REST bridge and the OpenAPI generator so that the
// tool list has a single source of truth.
func (s *Server) inProcClient(ctx context.Context, sess *session.Session) (*mcp.ClientSession, error) {
	ct, st := mcp.NewInMemoryTransports()
	srv := s.mcpServerFor(sess)
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "vchasno-edo-mcp-rest", Version: tools.Version}, nil)
	return client.Connect(ctx, ct, nil)
}

// listToolsFor returns the tool definitions of a session's server (or of a
// template server when no session is available yet).
func (s *Server) listToolsFor(ctx context.Context, sess *session.Session) ([]*mcp.Tool, error) {
	cs, err := s.inProcClient(ctx, sess)
	if err != nil {
		return nil, err
	}
	defer cs.Close()
	var out []*mcp.Tool
	for t, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *Server) handleToolList(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	list, err := s.listToolsFor(r.Context(), sess)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tools": list})
}

// handleToolCall is the REST bridge: POST /tools/{name} with a JSON body of arguments.
func (s *Server) handleToolCall(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	name := r.PathValue("name")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "cannot read body", 400)
		return
	}
	args := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &args); err != nil {
			http.Error(w, "body must be a JSON object of tool arguments", 400)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.Cfg.RequestTimeout*3)
	defer cancel()
	cs, err := s.inProcClient(ctx, sess)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	text := ""
	for _, c := range res.Content {
		if tc, okc := c.(*mcp.TextContent); okc {
			text += tc.Text
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if res.IsError {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		_ = json.NewEncoder(w).Encode(parsed)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"result": text})
}

// handleOpenAPI serves an OpenAPI 3.1 document describing every tool as
// POST /tools/{name}. It is public (no data inside) and derives the tool list
// from the pre-auth session when present, otherwise from a template session
// built from the metadata of any live session; when nothing is live yet it
// lists the static tool names only.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	sess := s.preAuth
	if sess == nil {
		s.mu.Lock()
		for live := range s.servers {
			sess = live
			break
		}
		s.mu.Unlock()
	}
	paths := map[string]any{}
	if sess != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		list, err := s.listToolsFor(ctx, sess)
		if err == nil {
			for _, t := range list {
				paths["/tools/"+t.Name] = map[string]any{"post": map[string]any{
					"operationId": t.Name,
					"summary":     t.Description,
					"requestBody": map[string]any{"required": false, "content": map[string]any{"application/json": map[string]any{"schema": t.InputSchema}}},
					"responses":   map[string]any{"200": map[string]any{"description": "Tool result (JSON)"}, "422": map[string]any{"description": "Tool error"}},
					"security":    []map[string]any{{"bearerAuth": []string{}}},
				}}
			}
		}
	}
	spec := map[string]any{
		"openapi":    "3.1.0",
		"info":       map[string]any{"title": "Вчасно.ЕДО MCP tools", "version": tools.Version, "description": "Bridge to the Vchasno.EDO document exchange API. Authenticate with the OAuth Bearer token or the X-Vchasno-Token header."},
		"servers":    []map[string]any{{"url": s.Cfg.IssuerURL}},
		"paths":      paths,
		"components": map[string]any{"securitySchemes": map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"}}},
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(spec)
}
