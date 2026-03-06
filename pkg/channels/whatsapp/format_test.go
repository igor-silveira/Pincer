package whatsapp

import "testing"

func TestMarkdownToWhatsApp(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "bold",
			in:   "this is **bold** text",
			want: "this is *bold* text",
		},
		{
			name: "italic with stars",
			in:   "this is *italic* text",
			want: "this is _italic_ text",
		},
		{
			name: "italic with underscores preserved",
			in:   "this is _italic_ text",
			want: "this is _italic_ text",
		},
		{
			name: "bold italic",
			in:   "this is ***bold italic*** text",
			want: "this is *_bold italic_* text",
		},
		{
			name: "strikethrough",
			in:   "this is ~~deleted~~ text",
			want: "this is ~deleted~ text",
		},
		{
			name: "inline code",
			in:   "run `go test` now",
			want: "run ```go test``` now",
		},
		{
			name: "code block",
			in:   "example:\n```go\nfmt.Println(\"hi\")\n```\ndone",
			want: "example:\n```\nfmt.Println(\"hi\")\n```\ndone",
		},
		{
			name: "header",
			in:   "# Title\nsome text\n## Subtitle",
			want: "*Title*\nsome text\n*Subtitle*",
		},
		{
			name: "link",
			in:   "visit [Google](https://google.com) now",
			want: "visit Google (https://google.com) now",
		},
		{
			name: "bullet star",
			in:   "list:\n* one\n* two",
			want: "list:\n• one\n• two",
		},
		{
			name: "dash bullets unchanged",
			in:   "list:\n- one\n- two",
			want: "list:\n- one\n- two",
		},
		{
			name: "plain text unchanged",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "code block protects content",
			in:   "```\n**not bold** *not italic*\n```",
			want: "```\n**not bold** *not italic*\n```",
		},
		{
			name: "mixed formatting",
			in:   "# Hello\n\nThis is **important** and *urgent*.\n\nRun `cmd` to fix ~~old~~ things.",
			want: "*Hello*\n\nThis is *important* and _urgent_.\n\nRun ```cmd``` to fix ~old~ things.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markdownToWhatsApp(tt.in)
			if got != tt.want {
				t.Errorf("markdownToWhatsApp(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}
