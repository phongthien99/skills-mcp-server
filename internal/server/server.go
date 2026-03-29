// Package server implements the MCP server for exposing skills as tools.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/portertech/skills-mcp-server/internal/registry"
	"github.com/portertech/skills-mcp-server/pkg/skill"
)

// Server wraps an MCP server that exposes skills as tools.
type Server struct {
	mcp      *mcp.Server
	registry *registry.Registry
	logger   *slog.Logger
}

// New creates a new skills MCP server.
func New(reg *registry.Registry, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	mcpServer := mcp.NewServer(
		&mcp.Implementation{
			Name:    "skills",
			Version: "1.0.0",
		},
		&mcp.ServerOptions{
			Instructions: "This server provides Claude-compatible skills as tools. " +
				"Call a skill tool to receive expert instructions for that task.",
			Logger: logger,
		},
	)

	s := &Server{
		mcp:      mcpServer,
		registry: reg,
		logger:   logger,
	}

	s.registerSkillPrompts()
	s.registerAllSkillResources()

	return s
}

// registerSkillPrompts registers each skill as an MCP prompt.
func (s *Server) registerSkillPrompts() {
	for _, sk := range s.registry.List() {
		s.registerSkillPrompt(sk)
	}
}

// formatSkillResponse formats a skill's content as a markdown response.
func formatSkillResponse(sk *skill.Skill) string {
	text := fmt.Sprintf("# Skill: %s\n\n**Description:** %s\n\n---\n\n%s", sk.Name, sk.Description, sk.Instructions)
	if len(sk.References) > 0 {
		text += "\n\n---\n\n## Available References\n"
		for _, ref := range sk.References {
			uri := fmt.Sprintf("skill://%s/%s", sk.Name, ref)
			text += fmt.Sprintf("- `%s`\n", uri)
		}
		text += "\nUse ReadResource to read any of these files when needed."
	}
	return text
}

// registerSkillPrompt registers a single skill as an MCP prompt.
func (s *Server) registerSkillPrompt(sk *skill.Skill) {
	promptName := registry.ToolNameForSkill(sk.Name)

	prompt := &mcp.Prompt{
		Name:        promptName,
		Description: sk.Description,
	}

	handler := func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		instructions, err := registry.LoadSkillInstructions(filepath.Join(sk.Path, registry.SkillFileName))
		if err != nil {
			return nil, fmt.Errorf("load skill %q: %w", sk.Name, err)
		}
		filled := *sk
		filled.Instructions = instructions
		return &mcp.GetPromptResult{
			Description: sk.Description,
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: formatSkillResponse(&filled)},
				},
			},
		}, nil
	}

	s.mcp.AddPrompt(prompt, handler)
	s.logger.Debug("registered skill prompt", "name", promptName, "skill", sk.Name)
}

// registerAllSkillResources registers MCP resources for all skill reference files.
func (s *Server) registerAllSkillResources() {
	for _, sk := range s.registry.List() {
		s.registerSkillResources(sk)
	}
}

// registerSkillResources registers MCP resources for a skill's reference files.
func (s *Server) registerSkillResources(sk *skill.Skill) {
	for _, refPath := range sk.References {
		absPath := filepath.Join(sk.Path, refPath)
		uri := fmt.Sprintf("skill://%s/%s", sk.Name, refPath)

		resource := &mcp.Resource{
			URI:         uri,
			Name:        refPath,
			Description: fmt.Sprintf("Reference file for skill: %s", sk.Name),
			MIMEType:    "text/plain",
		}

		capturedAbsPath := absPath
		capturedURI := uri
		handler := func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			data, err := os.ReadFile(capturedAbsPath)
			if err != nil {
				return nil, mcp.ResourceNotFoundError(capturedURI)
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{
					{URI: capturedURI, MIMEType: "text/plain", Text: string(data)},
				},
			}, nil
		}

		s.mcp.AddResource(resource, handler)
		s.logger.Debug("registered skill resource", "uri", uri, "skill", sk.Name)
	}
}

// Run starts the MCP server with stdio transport.
func (s *Server) Run(ctx context.Context) error {
	s.logger.Info("starting skills MCP server",
		"skills_count", s.registry.Count(),
		"skills_root", s.registry.Root(),
	)
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}

// RunHTTP starts the MCP server with Streamable HTTP transport.
func (s *Server) RunHTTP(ctx context.Context, addr string) error {
	s.logger.Info("starting skills MCP server (HTTP)",
		"skills_count", s.registry.Count(),
		"skills_root", s.registry.Root(),
		"addr", addr,
	)
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return s.mcp
	}, nil)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.logger.Debug("request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
			handler.ServeHTTP(w, r)
		}),
	}
	go func() {
		<-ctx.Done()
		s.logger.Info("shutting down HTTP server")
		if err := httpServer.Shutdown(context.Background()); err != nil {
			s.logger.Error("shutdown error", "error", err)
		}
		s.logger.Info("HTTP server stopped")
	}()
	s.logger.Info("HTTP server listening", "url", "http://"+addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// RunWithTransport starts the MCP server with a custom transport.
// This is primarily useful for testing.
func (s *Server) RunWithTransport(ctx context.Context, transport mcp.Transport) error {
	return s.mcp.Run(ctx, transport)
}
