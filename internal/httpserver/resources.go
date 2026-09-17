package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// REST bridge for MCP resources and prompts, mirroring /tools: lets curl and
// non-MCP clients read the same guides and recipes an MCP client sees.
//
//	GET  /resources               list resources and resource templates
//	GET  /resources/read?uri=…    read one resource
//	GET  /prompts                 list prompts with arguments
//	POST /prompts/{name}          render a prompt; body = JSON object of arguments

func (s *Server) handleResourceList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.Cfg.RequestTimeout)
	defer cancel()
	cs, err := s.inProcClient(ctx, sessionFrom(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer cs.Close()
	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	tpl, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"resources": res.Resources, "resource_templates": tpl.ResourceTemplates})
}

func (s *Server) handleResourceRead(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	if uri == "" {
		http.Error(w, "query parameter 'uri' is required, e.g. /resources/read?uri=bas://base/overview", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.Cfg.RequestTimeout)
	defer cancel()
	cs, err := s.inProcClient(ctx, sessionFrom(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer cs.Close()
	res, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if len(res.Contents) == 1 && res.Contents[0].Text != "" {
		mime := res.Contents[0].MIMEType
		if mime == "" {
			mime = "text/plain"
		}
		w.Header().Set("Content-Type", mime+"; charset=utf-8")
		_, _ = io.WriteString(w, res.Contents[0].Text)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handlePromptList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.Cfg.RequestTimeout)
	defer cancel()
	cs, err := s.inProcClient(ctx, sessionFrom(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer cs.Close()
	res, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"prompts": res.Prompts})
}

func (s *Server) handlePromptGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "cannot read body", 400)
		return
	}
	args := map[string]string{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &args); err != nil {
			http.Error(w, "body must be a JSON object of string arguments", 400)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.Cfg.RequestTimeout)
	defer cancel()
	cs, err := s.inProcClient(ctx, sessionFrom(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer cs.Close()
	res, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: name, Arguments: args})
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
