package mcp

import "testing"

func TestEnrichToolDescription(t *testing.T) {
	tests := []struct {
		name         string
		serverName   string
		instructions string
		description  string
		want         string
	}{
		{
			name:         "with instructions",
			serverName:   "GitHub",
			instructions: "GitHub API integration for repo management.",
			description:  "List repository issues",
			want:         "[GitHub: GitHub API integration for repo management.] List repository issues",
		},
		{
			name:        "without instructions",
			serverName:  "my-server",
			description: "Do something",
			want:        "[my-server] Do something",
		},
		{
			name:         "empty description from server",
			serverName:   "test",
			instructions: "A test server.",
			description:  "",
			want:         "[test: A test server.]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnrichToolDescription(tt.serverName, tt.instructions, tt.description)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
