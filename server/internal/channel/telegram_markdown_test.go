package channel

import "testing"

func TestMarkdownToTelegramHTML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain text unchanged",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "html entities escaped",
			in:   "a < b > c & d",
			want: "a &lt; b &gt; c &amp; d",
		},
		{
			name: "bold asterisks",
			in:   "this is **bold** text",
			want: "this is <b>bold</b> text",
		},
		{
			name: "bold underscores",
			in:   "this is __bold__ text",
			want: "this is <b>bold</b> text",
		},
		{
			name: "italic asterisks",
			in:   "this is *italic* text",
			want: "this is <i>italic</i> text",
		},
		{
			name: "italic underscores",
			in:   "this is _italic_ text",
			want: "this is <i>italic</i> text",
		},
		{
			name: "inline code",
			in:   "use `fmt.Println` here",
			want: "use <code>fmt.Println</code> here",
		},
		{
			name: "inline code with html chars",
			in:   "use `a < b` here",
			want: "use <code>a &lt; b</code> here",
		},
		{
			name: "fenced code block",
			in:   "before\n```go\nfmt.Println(\"hi\")\n```\nafter",
			want: "before\n<pre>fmt.Println(\"hi\")\n</pre>\nafter",
		},
		{
			name: "link",
			in:   "click [here](https://example.com) now",
			want: `click <a href="https://example.com">here</a> now`,
		},
		{
			name: "heading",
			in:   "## Section Title",
			want: "<b>Section Title</b>",
		},
		{
			name: "bold and italic together",
			in:   "**bold** and *italic*",
			want: "<b>bold</b> and <i>italic</i>",
		},
		{
			name: "code block protects markdown",
			in:   "```\n**not bold**\n```",
			want: "<pre>**not bold**\n</pre>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markdownToTelegramHTML(tt.in)
			if got != tt.want {
				t.Errorf("markdownToTelegramHTML(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}
