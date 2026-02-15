package channels

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitMessage(t *testing.T) {
	tests := []struct {
		name    string
		content string
		maxLen  int
		want    []string
	}{
		{
			name:    "under limit",
			content: "hello",
			maxLen:  10,
			want:    []string{"hello"},
		},
		{
			name:    "exactly at limit",
			content: "hello",
			maxLen:  5,
			want:    []string{"hello"},
		},
		{
			name:    "empty string",
			content: "",
			maxLen:  10,
			want:    []string{""},
		},
		{
			name:    "zero maxLen returns content as-is",
			content: "hello",
			maxLen:  0,
			want:    []string{"hello"},
		},
		{
			name:    "negative maxLen returns content as-is",
			content: "hello",
			maxLen:  -1,
			want:    []string{"hello"},
		},
		{
			name:    "split on paragraph break",
			content: "first paragraph\n\nsecond paragraph",
			maxLen:  20,
			want:    []string{"first paragraph\n\n", "second paragraph"},
		},
		{
			name:    "split on line break",
			content: "first line\nsecond line",
			maxLen:  15,
			want:    []string{"first line\n", "second line"},
		},
		{
			name:    "split on word boundary",
			content: "hello world test",
			maxLen:  11,
			want:    []string{"hello ", "world test"},
		},
		{
			name:    "hard cut when no boundaries",
			content: "abcdefghij",
			maxLen:  5,
			want:    []string{"abcde", "fghij"},
		},
		{
			name:    "multiple chunks",
			content: "aaa bbb ccc ddd eee",
			maxLen:  8,
			want:    []string{"aaa bbb ", "ccc ddd ", "eee"},
		},
		{
			name:    "code block preserved",
			content: "text before\n```go\nfunc main() {}\n```\ntext after",
			maxLen:  25,
			want:    []string{"text before\n", "```go\nfunc main() {}\n```\n", "text after"},
		},
		{
			name:    "code block exceeding limit falls back to line split",
			content: "```\nabcdefghijklmnop\n```",
			maxLen:  10,
			want:    []string{"```\n", "abcdefghij", "klmnop\n```"},
		},
		{
			name:    "paragraph preferred over line break",
			content: "aaa\nbbb\n\nccc",
			maxLen:  10,
			want:    []string{"aaa\nbbb\n\n", "ccc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitMessage(tt.content, tt.maxLen)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d chunks, want %d\ngot:  %q\nwant: %q", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("chunk[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSplitMessageReassembly(t *testing.T) {
	inputs := []string{
		"",
		"short",
		"hello world this is a longer message that should be split into multiple chunks",
		"first\n\nsecond\n\nthird",
		"no spaces here just a long string of characters",
		"```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\nsome trailing text",
	}

	for _, input := range inputs {
		for _, maxLen := range []int{5, 10, 20, 50} {
			chunks := SplitMessage(input, maxLen)
			reassembled := strings.Join(chunks, "")
			if reassembled != input {
				t.Errorf("reassembly failed for maxLen=%d\ninput:       %q\nreassembled: %q", maxLen, input, reassembled)
			}
		}
	}
}

func TestSplitMessageChunkSize(t *testing.T) {
	inputs := []string{
		"hello world this is a test of the splitting function",
		"aaa\nbbb\nccc\nddd\neee\nfff",
		"```\ncode block\n```\nsome text after the code block",
		strings.Repeat("x", 100),
	}

	for _, input := range inputs {
		for _, maxLen := range []int{5, 10, 20, 50} {
			chunks := SplitMessage(input, maxLen)
			for i, chunk := range chunks {
				if len(chunk) > maxLen {
					t.Errorf("chunk[%d] exceeds maxLen=%d: len=%d content=%q\ninput: %q",
						i, maxLen, len(chunk), chunk, input)
				}
			}
		}
	}
}

func TestSplitMessageUTF8Safety(t *testing.T) {
	content := "Hello 世界! こんにちは 🌍🌎🌏"

	chunks := SplitMessage(content, 10)
	reassembled := strings.Join(chunks, "")
	if reassembled != content {
		t.Errorf("UTF-8 reassembly failed\ninput:       %q\nreassembled: %q", content, reassembled)
	}

	for i, chunk := range chunks {
		if !utf8.ValidString(chunk) {
			t.Errorf("chunk[%d] is not valid UTF-8: %q", i, chunk)
		}
	}
}
