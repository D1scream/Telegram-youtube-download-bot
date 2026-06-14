package youtube

import "testing"

func TestFormatYtdlpError(t *testing.T) {
	got := formatYtdlpError(fmtError("yt-dlp: something failed"))
	if got != "something failed" {
		t.Fatalf("got %q", got)
	}
}

type fmtError string

func (e fmtError) Error() string { return string(e) }
