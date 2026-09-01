package main

import "testing"

func TestIsCommand(t *testing.T) {
	tests := []struct {
		token, name string
		want        bool
	}{
		{"/ytm", "ytm", true},
		{"/ytm@mybot", "ytm", true},
		{"/ytv", "ytm", false},
		{"/ytmusic", "ytm", false},
		{"ytm", "ytm", false},
	}
	for _, tt := range tests {
		if got := isCommand(tt.token, tt.name); got != tt.want {
			t.Fatalf("isCommand(%q, %q) = %v, want %v", tt.token, tt.name, got, tt.want)
		}
	}
}
