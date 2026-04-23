package main

import (
	"strings"
	"testing"
	"time"
)

func TestFileAgentJoinPath(t *testing.T) {
	tests := []struct {
		dir, elem, want string
	}{
		{"/home", "allan", "/home/allan"},
		{"/home/allan", "docs", "/home/allan/docs"},
		{"/", "var", "/var"},
		{"/home", ".", "/home"},
		{"/home", "", "/home"},
	}
	for _, tt := range tests {
		got := fileAgentJoinPath(tt.dir, tt.elem)
		if got != tt.want {
			t.Errorf("fileAgentJoinPath(%q,%q)=%q want %q", tt.dir, tt.elem, got, tt.want)
		}
	}
}

func TestBuildFileAgentReceiverCommand(t *testing.T) {
	d := 15 * time.Minute
	s := BuildFileAgentReceiverCommand("/app/filemover", ":9742", "/data/share", "secret", true, true, d)
	if !strings.Contains(s, "-file-agent ") || !strings.Contains(s, "9742") || !strings.Contains(s, "15m") || !strings.Contains(s, "secret") {
		t.Fatalf("unexpected command: %s", s)
	}
	s2 := BuildFileAgentReceiverCommand("fm", ":1", ".", "x", false, false, 0)
	if strings.Contains(s2, "-file-agent-psk") || !strings.Contains(s2, "lan-only=false") {
		t.Fatalf("unexpected: %s", s2)
	}
}

func TestFileAgentParentDir(t *testing.T) {
	if p := fileAgentParentDir("/a/b/c"); p != "/a/b" {
		t.Fatal(p)
	}
	if p := fileAgentParentDir("/"); p != "/" {
		t.Fatal(p)
	}
}

func TestFileAgentNormalizeVirtualPath(t *testing.T) {
	if g := fileAgentNormalizeVirtualPath(`C:\`); g != "/" {
		t.Fatalf("C:\\: got %q", g)
	}
	if g := fileAgentNormalizeVirtualPath(`c:/`); g != "/" {
		t.Fatalf("c:/: got %q", g)
	}
	if g := fileAgentNormalizeVirtualPath(`\C:\`); g != "/" {
		t.Fatalf(`\C:\: got %q`, g)
	}
	if g := fileAgentNormalizeVirtualPath("docs"); g != "/docs" {
		t.Fatal(g)
	}
	if g := fileAgentNormalizeVirtualPath("/docs"); g != "/docs" {
		t.Fatal(g)
	}
}
