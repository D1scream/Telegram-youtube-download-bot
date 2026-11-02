package telegram

import "testing"

func TestParseAGIFArgs(t *testing.T) {
	tests := []struct {
		args    string
		name    string
		bpm     float64
		raw     bool
		wantErr bool
	}{
		{"", "", 0, false, false},
		{"toyota", "toyota", 0, false, false},
		{"toyota 136", "toyota", 136, false, false},
		{"136 toyota", "toyota", 136, false, false},
		{"-c toyota 128,5", "toyota", 128.5, true, false},
		{"136", "", 136, false, false},
		{"big car 90", "big car", 90, false, false},
		{"toyota 1000", "", 0, false, true},
	}
	for _, tt := range tests {
		name, bpm, raw, err := parseAGIFArgs(tt.args)
		if (err != nil) != tt.wantErr {
			t.Errorf("%q: err = %v", tt.args, err)
			continue
		}
		if !tt.wantErr && (name != tt.name || bpm != tt.bpm || raw != tt.raw) {
			t.Errorf("%q: got (%q, %v, %v), want (%q, %v, %v)", tt.args, name, bpm, raw, tt.name, tt.bpm, tt.raw)
		}
	}
}
