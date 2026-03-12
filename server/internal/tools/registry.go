package tools

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/cogitatorai/cogitator/server/internal/provider"
	"gopkg.in/yaml.v3"
)

// ToolDef holds a tool's definition and execution metadata.
// The embedded provider.Tool fields (Name, Description, Parameters) are decoded
// from YAML using their lowercased field names since yaml.v3 treats exported
// fields without yaml tags using their lowercased name.
type ToolDef struct {
	Name        string         `yaml:"name"        json:"name"`
	Description string         `yaml:"description" json:"description"`
	Parameters  map[string]any `yaml:"parameters"  json:"parameters"`
	Command     string         `yaml:"command,omitempty"     json:"command,omitempty"`
	WorkingDir  string         `yaml:"working_dir,omitempty" json:"working_dir,omitempty"`
	Builtin     bool           `yaml:"-"           json:"builtin"`
	MCPServer   string         `yaml:"-"           json:"mcp_server,omitempty"`
	MCPToolName string         `yaml:"-"           json:"-"`
}

// ProviderTool converts ToolDef into the provider.Tool format used for LLM calls.
func (t ToolDef) ProviderTool() provider.Tool {
	return provider.Tool{
		Name:        t.Name,
		Description: t.Description,
		Parameters:  t.Parameters,
	}
}

// Registry manages built-in and custom tools.
type Registry struct {
	mu        sync.RWMutex
	tools     map[string]ToolDef
	customDir string
	logger    *slog.Logger
}

// NewRegistry creates a Registry with built-in tools pre-registered.
// customDir is the directory to scan for custom tool definitions; it may be empty.
func NewRegistry(customDir string, logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	r := &Registry{
		tools:     make(map[string]ToolDef),
		customDir: customDir,
		logger:    logger,
	}
	registerBuiltinTools(r)
	return r
}

// LoadCustomTools scans the custom tools directory and loads tool definitions
// from tool.yaml files. Directories without a tool.yaml are silently skipped.
// If customDir is empty or does not exist, LoadCustomTools is a no-op.
func (r *Registry) LoadCustomTools() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.customDir == "" {
		return nil
	}

	entries, err := os.ReadDir(r.customDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	loaded := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		toolPath := filepath.Join(r.customDir, entry.Name(), "tool.yaml")
		data, err := os.ReadFile(toolPath)
		if err != nil {
			continue // skip directories without tool.yaml
		}

		var def ToolDef
		if err := yaml.Unmarshal(data, &def); err != nil {
			r.logger.Warn("invalid tool.yaml", "path", toolPath, "error", err)
			continue
		}
		if def.Name == "" {
			def.Name = entry.Name()
		}

		r.tools[def.Name] = def
		loaded++
	}

	r.logger.Info("loaded custom tools", "count", loaded)
	return nil
}

// Get returns a tool definition by name.
func (r *Registry) Get(name string) (ToolDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tools as a slice.
func (r *Registry) List() []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}

// ProviderTools returns all tool definitions in provider.Tool format for LLM calls.
func (r *Registry) ProviderTools() []provider.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]provider.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t.ProviderTool())
	}
	return result
}

// Register adds or replaces a tool definition.
func (r *Registry) Register(def ToolDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[def.Name] = def
}

// Delete removes a custom tool by name. Built-in tools cannot be deleted.
// Returns true if the tool was deleted, false if it does not exist or is built-in.
func (r *Registry) Delete(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tools[name]; ok && t.Builtin {
		return false
	}
	_, exists := r.tools[name]
	delete(r.tools, name)
	return exists
}
