package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPathAllowed_EmptyAllowsAll(t *testing.T) {
	if err := CheckPathAllowed("/any/path", nil); err != nil {
		t.Errorf("empty allowedPaths should allow all: %v", err)
	}
	if err := CheckPathAllowed("/any/path", []string{}); err != nil {
		t.Errorf("empty slice should allow all: %v", err)
	}
}

func TestCheckPathAllowed_SubpathMatch(t *testing.T) {
	allowed := []string{"/tmp/sandbox"}
	if err := CheckPathAllowed("/tmp/sandbox/file.txt", allowed); err != nil {
		t.Errorf("subpath should be allowed: %v", err)
	}
}

func TestCheckPathAllowed_ExactMatch(t *testing.T) {
	allowed := []string{"/tmp/sandbox"}
	if err := CheckPathAllowed("/tmp/sandbox", allowed); err != nil {
		t.Errorf("exact match should be allowed: %v", err)
	}
}

func TestCheckPathAllowed_OutsideRejected(t *testing.T) {
	allowed := []string{"/tmp/sandbox"}
	if err := CheckPathAllowed("/etc/passwd", allowed); err == nil {
		t.Error("path outside allowed dirs should be rejected")
	}
}

func TestCheckPathAllowed_PrefixAttack(t *testing.T) {
	allowed := []string{"/tmp/foo"}
	if err := CheckPathAllowed("/tmp/foobar/secret", allowed); err == nil {
		t.Error("/tmp/foobar should NOT match /tmp/foo (prefix attack)")
	}
}

func TestCheckPathAllowed_SymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	allowed := filepath.Join(dir, "jail")
	outside := filepath.Join(dir, "outside")
	link := filepath.Join(allowed, "escape")

	os.MkdirAll(allowed, 0755)
	os.MkdirAll(outside, 0755)
	os.Symlink(outside, link)

	if err := CheckPathAllowed(link, []string{allowed}); err == nil {
		t.Error("symlink escaping allowed dir should be rejected")
	}
}

func TestCheckPathAllowed_RelativePathTraversal(t *testing.T) {
	allowed := []string{"/tmp/sandbox"}
	if err := CheckPathAllowed("/tmp/sandbox/../secret", allowed); err == nil {
		t.Error("relative path traversal should be rejected")
	}
}

func TestCheckPathAllowed_MultipleAllowedDirs(t *testing.T) {
	allowed := []string{"/tmp/a", "/tmp/b"}
	if err := CheckPathAllowed("/tmp/b/file.txt", allowed); err != nil {
		t.Errorf("path under second allowed dir should be allowed: %v", err)
	}
}

func TestCheckPathWritable_EmptyAllowsAll(t *testing.T) {
	if err := CheckPathWritable("/any/path", nil); err != nil {
		t.Errorf("empty readOnlyPaths should allow writes: %v", err)
	}
}

func TestCheckPathWritable_ReadOnlyEnforced(t *testing.T) {
	readOnly := []string{"/etc"}
	if err := CheckPathWritable("/etc/config.toml", readOnly); err == nil {
		t.Error("writing to read-only path should be rejected")
	}
}

func TestCheckPathWritable_OutsideReadOnlyAllowed(t *testing.T) {
	readOnly := []string{"/etc"}
	if err := CheckPathWritable("/tmp/file.txt", readOnly); err != nil {
		t.Errorf("writing outside read-only dirs should be allowed: %v", err)
	}
}

func TestResolvePath_NonExistent(t *testing.T) {
	result := resolvePath("/nonexistent/path/to/file")
	if result != "/nonexistent/path/to/file" {
		t.Errorf("non-existent path should be cleaned abs: got %q", result)
	}
}

func TestIsSubPath_Exact(t *testing.T) {
	if !isSubPath("/tmp/foo", "/tmp/foo") {
		t.Error("exact match should return true")
	}
}

func TestIsSubPath_Child(t *testing.T) {
	if !isSubPath("/tmp/foo/bar", "/tmp/foo") {
		t.Error("child should return true")
	}
}

func TestIsSubPath_NotChild(t *testing.T) {
	if isSubPath("/tmp/foobar", "/tmp/foo") {
		t.Error("prefix-only match should return false")
	}
}
