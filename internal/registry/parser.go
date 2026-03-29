// Package registry handles skill discovery and management.
package registry

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/portertech/skills-mcp-server/pkg/skill"
	"gopkg.in/yaml.v3"
)

// SkillFileName is the conventional filename for skill definitions.
const SkillFileName = "SKILL.md"

var (
	// ErrNoFrontmatter is returned when a SKILL.md file lacks YAML frontmatter.
	ErrNoFrontmatter = errors.New("no YAML frontmatter found")
	// ErrMissingName is returned when a skill's frontmatter lacks the required name field.
	ErrMissingName = errors.New("skill name is required")
	// ErrMissingDesc is returned when a skill's frontmatter lacks the required description field.
	ErrMissingDesc = errors.New("skill description is required")
	// ErrFileTooLarge is returned when a SKILL.md file exceeds MaxSkillFileSize.
	ErrFileTooLarge = errors.New("skill file exceeds maximum size")
	// ErrRefAbsPath is returned when a reference path is absolute instead of relative.
	ErrRefAbsPath = errors.New("reference path must be relative")
	// ErrRefPathTraversal is returned when a reference path escapes the skill directory.
	ErrRefPathTraversal = errors.New("reference path must not escape skill directory")
)

// MaxSkillFileSize is the maximum allowed size for a SKILL.md file (64KB).
// This limit ensures skills remain token-efficient for LLM context windows.
const MaxSkillFileSize = 64 << 10

// ParseSkillMD parses a SKILL.md file and returns a Skill.
// The file must contain YAML frontmatter between --- markers.
// Returns ErrFileTooLarge if the file exceeds MaxSkillFileSize.
func ParseSkillMD(path string) (*skill.Skill, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat skill file: %w", err)
	}
	if info.Size() > MaxSkillFileSize {
		return nil, fmt.Errorf("%w: %d bytes (max %d)", ErrFileTooLarge, info.Size(), MaxSkillFileSize)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open skill file: %w", err)
	}
	defer f.Close()

	var (
		scanner       = bufio.NewScanner(f)
		inFrontmatter bool
		frontmatter   strings.Builder
		content       strings.Builder
		lineNum       int
		fmStart       = -1
		fmEnd         = -1
	)

	for scanner.Scan() {
		line := scanner.Text()
		lineNum++

		if lineNum == 1 && line == "---" {
			inFrontmatter = true
			fmStart = lineNum
			continue
		}

		if inFrontmatter && line == "---" {
			inFrontmatter = false
			fmEnd = lineNum
			continue
		}

		if inFrontmatter {
			frontmatter.WriteString(line)
			frontmatter.WriteString("\n")
		} else if fmEnd > 0 {
			content.WriteString(line)
			content.WriteString("\n")
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read skill file: %w", err)
	}

	if fmStart < 0 || fmEnd < 0 {
		return nil, ErrNoFrontmatter
	}

	var s skill.Skill
	if err := yaml.Unmarshal([]byte(frontmatter.String()), &s); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	if s.Name == "" {
		return nil, ErrMissingName
	}
	if s.Description == "" {
		return nil, ErrMissingDesc
	}

	s.Instructions = strings.TrimSpace(content.String())

	skillDir := filepath.Dir(path)
	for _, ref := range s.References {
		if filepath.IsAbs(ref) {
			return nil, fmt.Errorf("%w: %s", ErrRefAbsPath, ref)
		}
		absRef := filepath.Join(skillDir, ref)
		rel, err := filepath.Rel(skillDir, absRef)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("%w: %s", ErrRefPathTraversal, ref)
		}
	}

	return &s, nil
}

// LoadSkillInstructions reads the markdown instructions from a SKILL.md file.
// It is used for lazy loading: the registry stores only metadata at scan time,
// and the full content is read on demand when a skill is actually requested.
func LoadSkillInstructions(path string) (string, error) {
	s, err := ParseSkillMD(path)
	if err != nil {
		return "", err
	}
	return s.Instructions, nil
}
