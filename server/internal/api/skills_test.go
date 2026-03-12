package api

import "testing"

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		input       string
		wantName    string
		wantDesc    string
	}{
		{
			input:    "---\nname: my-skill\ndescription: Does things.\n---\n\n# Body",
			wantName: "my-skill",
			wantDesc: "Does things.",
		},
		{
			input:    "---\nname: weather\n---\nContent here",
			wantName: "weather",
			wantDesc: "",
		},
		{
			input:    "no frontmatter at all",
			wantName: "",
			wantDesc: "",
		},
		{
			input:    "---\ndescription: orphan\n---\nbody",
			wantName: "",
			wantDesc: "orphan",
		},
	}

	for _, tt := range tests {
		name, desc := parseFrontmatter(tt.input)
		if name != tt.wantName {
			t.Errorf("parseFrontmatter(%q): name = %q, want %q", tt.input[:20], name, tt.wantName)
		}
		if desc != tt.wantDesc {
			t.Errorf("parseFrontmatter(%q): desc = %q, want %q", tt.input[:20], desc, tt.wantDesc)
		}
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"My Cool Skill", "my-cool-skill"},
		{"open-meteo-weather", "open-meteo-weather"},
		{"  Spaces  Everywhere  ", "spaces-everywhere"},
		{"CamelCase", "camelcase"},
		{"special!@#chars", "special-chars"},
	}

	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
