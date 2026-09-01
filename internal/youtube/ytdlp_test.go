package youtube

import "testing"

func TestLastNonEmptyLine(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"  \n  ", ""},
		{"/tmp/a.m4a", "/tmp/a.m4a"},
		{"ignored\n/tmp/a.m4a\n", "/tmp/a.m4a"},
		{"ignored\r\n/tmp/a.m4a\r\n", "/tmp/a.m4a"},
	}
	for _, tt := range tests {
		if got := lastNonEmptyLine(tt.in); got != tt.want {
			t.Fatalf("lastNonEmptyLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLooksLikePath(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"yt-dlp", false},
		{"/usr/bin/yt-dlp", true},
		{`C:\tools\yt-dlp.exe`, true},
		{`bin\yt-dlp`, true},
		{"./yt-dlp", true},
	}
	for _, tt := range tests {
		if got := looksLikePath(tt.in); got != tt.want {
			t.Fatalf("looksLikePath(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
