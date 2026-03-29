package server

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/portertech/skills-mcp-server/internal/registry"
	pkgskill "github.com/portertech/skills-mcp-server/pkg/skill"
)

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	content := `---
name: test-skill
description: A test skill
---

Test instructions.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write skill: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := registry.NewRegistry(tmpDir, logger)
	if err := reg.Scan(); err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	srv := New(reg, logger)
	if srv == nil {
		t.Fatal("New() returned nil")
	}
	if srv.mcp == nil {
		t.Error("Server.mcp is nil")
	}
	if srv.registry != reg {
		t.Error("Server.registry not set correctly")
	}
}

func TestNewWithNilLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := registry.NewRegistry(tmpDir, logger)

	srv := New(reg, nil)
	if srv == nil {
		t.Fatal("New() returned nil")
	}
	if srv.logger == nil {
		t.Error("Server.logger should default when nil")
	}
}

func TestFormatSkillResponse(t *testing.T) {
	sk := &pkgskill.Skill{
		Name:         "test-skill",
		Description:  "A test skill",
		Instructions: "Do the thing.",
		Path:         "/path/to/skill",
	}

	response := formatSkillResponse(sk)

	if !strings.Contains(response, "# Skill: test-skill") {
		t.Error("response missing skill name header")
	}
	if !strings.Contains(response, "**Description:** A test skill") {
		t.Error("response missing description")
	}
	if !strings.Contains(response, "Do the thing.") {
		t.Error("response missing instruction content")
	}
}

func TestFormatSkillResponseWithReferences(t *testing.T) {
	sk := &pkgskill.Skill{
		Name:         "code-review",
		Description:  "Review code",
		Instructions: "Follow the guidelines.",
		Path:         "/path/to/skill",
		References:   []string{"checklist.md", "templates/pr-template.md"},
	}

	response := formatSkillResponse(sk)

	if !strings.Contains(response, "## Available References") {
		t.Error("response missing references section")
	}
	if !strings.Contains(response, "skill://code-review/checklist.md") {
		t.Error("response missing checklist.md URI")
	}
	if !strings.Contains(response, "skill://code-review/templates/pr-template.md") {
		t.Error("response missing pr-template.md URI")
	}
	if !strings.Contains(response, "Use ReadResource") {
		t.Error("response missing ReadResource instruction")
	}
}

func TestFormatSkillResponseNoReferences(t *testing.T) {
	sk := &pkgskill.Skill{
		Name:         "simple",
		Description:  "Simple skill",
		Instructions: "Do it simply.",
	}

	response := formatSkillResponse(sk)

	if strings.Contains(response, "Available References") {
		t.Error("response should not have references section when none defined")
	}
}

func newTestServer(t *testing.T, skillsDir string) *Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := registry.NewRegistry(skillsDir, logger)
	if err := reg.Scan(); err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	return New(reg, logger)
}

func connectClient(t *testing.T, ctx context.Context, srv *Server) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() {
		srv.RunWithTransport(ctx, serverTransport) //nolint:errcheck
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "greet")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	content := `---
name: greet
description: Greeting instructions
---

Say hello politely.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write skill: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srv := newTestServer(t, tmpDir)
	session := connectClient(t, ctx, srv)

	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts() error: %v", err)
	}
	if len(prompts.Prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts.Prompts))
	}
	if prompts.Prompts[0].Name != "greet" {
		t.Errorf("expected prompt name 'greet', got %q", prompts.Prompts[0].Name)
	}

	result, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "greet"})
	if err != nil {
		t.Fatalf("GetPrompt() error: %v", err)
	}
	if len(result.Messages) == 0 {
		t.Fatal("expected messages in result")
	}

	textContent, ok := result.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Messages[0].Content)
	}
	if !strings.Contains(textContent.Text, "# Skill: greet") {
		t.Error("response missing skill header")
	}
	if !strings.Contains(textContent.Text, "Say hello politely.") {
		t.Error("response missing instructions")
	}
}

func TestIntegrationMultipleSkills(t *testing.T) {
	tmpDir := t.TempDir()

	skills := []struct {
		name        string
		description string
		content     string
	}{
		{"alpha", "First skill", "Alpha instructions."},
		{"beta", "Second skill", "Beta instructions."},
		{"gamma", "Third skill", "Gamma instructions."},
	}

	for _, s := range skills {
		dir := filepath.Join(tmpDir, s.name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		md := "---\nname: " + s.name + "\ndescription: " + s.description + "\n---\n\n" + s.content + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0644); err != nil {
			t.Fatalf("failed to write skill: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srv := newTestServer(t, tmpDir)
	session := connectClient(t, ctx, srv)

	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts() error: %v", err)
	}
	if len(prompts.Prompts) != 3 {
		t.Errorf("expected 3 prompts, got %d", len(prompts.Prompts))
	}

	for _, s := range skills {
		result, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: s.name})
		if err != nil {
			t.Errorf("GetPrompt(%s) error: %v", s.name, err)
			continue
		}
		textContent, ok := result.Messages[0].Content.(*mcp.TextContent)
		if !ok {
			t.Errorf("expected TextContent for %s", s.name)
			continue
		}
		if !strings.Contains(textContent.Text, s.content) {
			t.Errorf("prompt %s response missing expected content", s.name)
		}
	}
}

func TestIntegrationWithReferences(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "code-review")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}

	// Write reference file
	checklistContent := "- [ ] Check correctness\n- [ ] Check security\n"
	if err := os.WriteFile(filepath.Join(skillDir, "checklist.md"), []byte(checklistContent), 0644); err != nil {
		t.Fatalf("failed to write checklist: %v", err)
	}

	content := `---
name: code-review
description: Expert code review
references:
  - checklist.md
---

Review code carefully.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write skill: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	srv := newTestServer(t, tmpDir)
	session := connectClient(t, ctx, srv)

	// Prompt should include reference URIs
	result, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "code_review"})
	if err != nil {
		t.Fatalf("GetPrompt() error: %v", err)
	}
	textContent := result.Messages[0].Content.(*mcp.TextContent)
	if !strings.Contains(textContent.Text, "skill://code-review/checklist.md") {
		t.Error("prompt missing reference URI")
	}

	// Resource should be listed
	resources, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources() error: %v", err)
	}
	if len(resources.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources.Resources))
	}
	if resources.Resources[0].URI != "skill://code-review/checklist.md" {
		t.Errorf("unexpected resource URI: %s", resources.Resources[0].URI)
	}

	// Resource should be readable
	readResult, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "skill://code-review/checklist.md"})
	if err != nil {
		t.Fatalf("ReadResource() error: %v", err)
	}
	if len(readResult.Contents) == 0 {
		t.Fatal("expected content in resource")
	}
	if readResult.Contents[0].Text != checklistContent {
		t.Errorf("resource content = %q, want %q", readResult.Contents[0].Text, checklistContent)
	}
}
