package music

import (
	"testing"

	"telegram-bot/internal/entities"
)

func TestParseQueryAndWildcardBypass(t *testing.T) {
	parsed := parseQuery("rihanna umbrella -remix")
	if parsed.included != "rihanna umbrella" {
		t.Fatalf("included=%q", parsed.included)
	}
	if len(parsed.excluded) != 1 || parsed.excluded[0] != "remix" {
		t.Fatalf("excluded=%v", parsed.excluded)
	}

	bypass, ok := wildcardBypassIncluded(parsed.included)
	if !ok || bypass != "*ihanna *mbrella" {
		t.Fatalf("bypass=%q ok=%v", bypass, ok)
	}
}

func TestDropExcluded(t *testing.T) {
	tracks := []entities.Track{
		{Filename: "song.mp3"},
		{Filename: "song remix.mp3"},
	}
	got := dropExcluded(tracks, []string{"remix"})
	if len(got) != 1 || got[0].Filename != "song.mp3" {
		t.Fatalf("got %#v", got)
	}
}
