package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/igorsilveira/pincer/pkg/sandbox"
	"github.com/igorsilveira/pincer/pkg/soul"
)

func TestSoulTool_Definition(t *testing.T) {
	tool := &SoulTool{Soul: soul.Default()}
	def := tool.Definition()
	if def.Name != "soul" {
		t.Errorf("Name = %q, want %q", def.Name, "soul")
	}
}

func TestSoulTool_AllSections(t *testing.T) {
	tool := &SoulTool{Soul: soul.Default()}
	input, _ := json.Marshal(soulInput{})

	output, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "Pincer") {
		t.Errorf("output should contain 'Pincer', got %q", output)
	}
}

func TestSoulTool_ExplicitAll(t *testing.T) {
	tool := &SoulTool{Soul: soul.Default()}
	input, _ := json.Marshal(soulInput{Section: "all"})

	output, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "Pincer") {
		t.Errorf("output should contain 'Pincer', got %q", output)
	}
}

func TestSoulTool_SpecificSection(t *testing.T) {
	tool := &SoulTool{Soul: soul.Default()}
	input, _ := json.Marshal(soulInput{Section: "identity"})

	output, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "Name: Pincer") {
		t.Errorf("output should contain 'Name: Pincer', got %q", output)
	}
}

func TestSoulTool_UnknownSection(t *testing.T) {
	s := soul.Default()
	tool := &SoulTool{Soul: s}
	input, _ := json.Marshal(soulInput{Section: "nosuchsection"})

	output, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	expected := s.Render()
	if output != expected {
		t.Errorf("unknown section should fall back to Render()")
	}
}
