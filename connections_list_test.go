package main

import "testing"

func TestParseRsyncListOnlyLine(t *testing.T) {
	tests := []struct {
		line     string
		wantName string
		wantDir  bool
		ok       bool
	}{
		{
			line:     "drwxr-xr-x          4096 2025/04/10 12:34:56 home",
			wantName: "home",
			wantDir:  true,
			ok:       true,
		},
		{
			line:     "-rw-r--r--       12345 Apr 10 12:34 somefile.txt",
			wantName: "somefile.txt",
			wantDir:  false,
			ok:       true,
		},
		{
			line:     "lrwxrwxrwx             1 Apr  9  2025 lib -> usr/lib",
			wantName: "lib",
			wantDir:  false,
			ok:       true,
		},
		{
			line: "total 42",
			ok:   false,
		},
	}
	for _, tt := range tests {
		fi, ok := parseRsyncListOnlyLine(tt.line)
		if ok != tt.ok {
			t.Errorf("line %q: ok=%v want %v (fi=%+v)", tt.line, ok, tt.ok, fi)
			continue
		}
		if !tt.ok {
			continue
		}
		if fi.Name != tt.wantName || fi.IsDir != tt.wantDir {
			t.Errorf("line %q: got %+v want name=%q dir=%v", tt.line, fi, tt.wantName, tt.wantDir)
		}
	}
}

func TestRsyncJoinPathNoDup(t *testing.T) {
	module := "u@h:/home"
	got := rsyncJoinPath(module, "allan")
	want := "u@h:/home/allan"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
