package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/autocarto/carto_agent/internal/toolsession"
)

type SessionLayer struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

type SessionContext struct {
	SessionID     string         `json:"session_id,omitempty"`
	ProjectID     string         `json:"project_id,omitempty"`
	ProjectPath   string         `json:"project_path,omitempty"`
	InstanceID    string         `json:"instance_id,omitempty"`
	PythonPath    string         `json:"python_path,omitempty"`
	AutocartoHome string         `json:"autocarto_home,omitempty"`
	Layers        []SessionLayer `json:"layers,omitempty"`
}

type sessionContextKey struct{}

func WithSessionContext(ctx context.Context, session SessionContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	session.normalize()
	return context.WithValue(ctx, sessionContextKey{}, session.Clone())
}

func SessionContextFromContext(ctx context.Context) SessionContext {
	if ctx == nil {
		return SessionContext{}
	}

	var session SessionContext
	if stored, ok := ctx.Value(sessionContextKey{}).(SessionContext); ok {
		session = stored.Clone()
	}
	if session.SessionID == "" {
		session.SessionID = toolsession.SessionIDFromContext(ctx)
	}
	if session.ProjectPath == "" {
		session.ProjectPath = toolsession.ProjectPathFromContext(ctx)
	}
	if session.InstanceID == "" {
		session.InstanceID = toolsession.InstanceIDFromContext(ctx)
	}
	session.normalize()
	return session
}

func (s *SessionContext) normalize() {
	if s == nil {
		return
	}
	s.SessionID = strings.TrimSpace(s.SessionID)
	s.ProjectID = strings.TrimSpace(s.ProjectID)
	s.ProjectPath = strings.TrimSpace(s.ProjectPath)
	s.InstanceID = strings.TrimSpace(s.InstanceID)
	s.PythonPath = strings.TrimSpace(s.PythonPath)
	s.AutocartoHome = strings.TrimSpace(s.AutocartoHome)
	if len(s.Layers) == 0 {
		return
	}
	layers := make([]SessionLayer, 0, len(s.Layers))
	for _, layer := range s.Layers {
		layer.Name = strings.TrimSpace(layer.Name)
		layer.Path = strings.TrimSpace(layer.Path)
		if layer.Name == "" && layer.Path == "" {
			continue
		}
		layers = append(layers, layer)
	}
	s.Layers = layers
}

func (s SessionContext) Clone() SessionContext {
	cloned := s
	if len(s.Layers) > 0 {
		cloned.Layers = append([]SessionLayer(nil), s.Layers...)
	}
	return cloned
}

func (s SessionContext) LayerExists(reference string) bool {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return false
	}
	if len(s.Layers) == 0 {
		return true
	}
	_, ok := s.findLayer(reference)
	return ok
}

func (s SessionContext) ResolveLayer(reference string) string {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return ""
	}
	if layer, ok := s.findLayer(reference); ok {
		if layer.Path != "" {
			return layer.Path
		}
		return layer.Name
	}
	return reference
}

func (s SessionContext) AutoOutputPath(name string) string {
	baseName := sanitizeFilePart(name)
	if baseName == "" {
		baseName = "output"
	}
	fileName := baseName + ".gpkg"
	if sessionID := sanitizeFilePart(s.SessionID); sessionID != "" {
		fileName = fmt.Sprintf("%s_%s.gpkg", sessionID, baseName)
	}
	if sessionDir := s.SessionDir(); sessionDir != "" {
		return filepath.Join(sessionDir, "output", fileName)
	}
	return filepath.Join(s.outputBaseDir(), "autocarto_outputs", fileName)
}

func (s SessionContext) outputBaseDir() string {
	projectPath := strings.TrimSpace(s.ProjectPath)
	if projectPath == "" {
		return os.TempDir()
	}
	if filepath.Ext(projectPath) != "" {
		return filepath.Dir(projectPath)
	}
	return projectPath
}

func (s SessionContext) SessionDir() string {
	if strings.TrimSpace(s.SessionID) == "" && strings.TrimSpace(s.ProjectPath) == "" {
		return ""
	}
	baseDir := s.outputBaseDir()
	projectDirName := sanitizeFilePart(strings.TrimSuffix(filepath.Base(strings.TrimSpace(s.ProjectPath)), filepath.Ext(strings.TrimSpace(s.ProjectPath))))
	if projectDirName == "" {
		projectDirName = "project"
	}
	sessionID := sanitizeFilePart(s.SessionID)
	if sessionID == "" {
		sessionID = "runtime"
	}
	return filepath.Join(baseDir, "autoc", projectDirName, "sessions", sessionID)
}

func (s SessionContext) SessionSubdirs() map[string]string {
	root := s.SessionDir()
	if root == "" {
		return nil
	}
	names := []string{"logs", "intermediate", "output", "temp", "snapshots", "exports"}
	sort.Strings(names)
	out := make(map[string]string, len(names)+1)
	out["root"] = root
	for _, name := range names {
		out[name] = filepath.Join(root, name)
	}
	return out
}

func (s SessionContext) findLayer(reference string) (SessionLayer, bool) {
	reference = strings.TrimSpace(reference)
	for _, layer := range s.Layers {
		if strings.EqualFold(layer.Name, reference) || strings.EqualFold(layer.Path, reference) {
			return layer, true
		}
	}
	return SessionLayer{}, false
}

func sanitizeFilePart(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range raw {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(unicode.ToLower(r))
		case r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), "_")
}
