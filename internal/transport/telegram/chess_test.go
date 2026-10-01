package telegram

import (
	"strings"
	"testing"
	"time"

	nc "github.com/notnil/chess"

	"telegram-bot/internal/chess"
)

func TestParseSquare(t *testing.T) {
	for in, want := range map[string]nc.Square{"a1": nc.A1, "e4": nc.E4, "h8": nc.H8} {
		if got, ok := parseSquare(in); !ok || got != want {
			t.Errorf("%s: %v %v", in, got, ok)
		}
		if squareName(want) != in {
			t.Errorf("squareName(%v) = %s", want, squareName(want))
		}
	}
	for _, bad := range []string{"", "i1", "a9", "e", "e44"} {
		if _, ok := parseSquare(bad); ok {
			t.Errorf("%q принят", bad)
		}
	}
}

func TestChessRender(t *testing.T) {
	g := chess.NewGame(chess.Player{ID: 1, Name: "A<b>"})
	if err := g.Start(chess.Player{ID: 2, Name: "B"}, true); err != nil {
		t.Fatal(err)
	}
	cg := &chessGame{game: g, updated: time.Now()}
	c := &chessController{}

	if err := g.Pick(1, nc.G1); err != nil {
		t.Fatal(err)
	}
	text, kb := c.render(cg, true)
	if kb != nil || strings.Count(text, "<tg-button-row") != 9 || strings.Count(text, "<tg-button ") != 64+2 {
		t.Fatalf("rich: ряды %d кнопки %d", strings.Count(text, "<tg-button-row"), strings.Count(text, "<tg-button "))
	}
	if !strings.Contains(text, "A&lt;b&gt;") || strings.Contains(text, "A<b>") {
		t.Fatal("имя не экранировано")
	}
	if !strings.Contains(text, `style="success" data="chs:2:f3"`) || !strings.Contains(text, `style="primary" data="chs:2:g1"`) {
		t.Fatalf("подсветка: %s", text)
	}

	plain, kb := c.render(cg, false)
	if kb == nil || len(kb.InlineKeyboard) != 9 || len(kb.InlineKeyboard[0]) != 8 || strings.Contains(plain, "<tg-button") {
		t.Fatal("обычная клавиатура")
	}
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			if len(b.CallbackData) > 64 {
				t.Fatalf("callback_data длиннее 64 байт: %s", b.CallbackData)
			}
		}
	}
}

func TestChessApply(t *testing.T) {
	g := chess.NewGame(chess.Player{ID: 1, Name: "A"})
	g.Start(chess.Player{ID: 2, Name: "B"}, true)
	cg := &chessGame{game: g}
	c := &chessController{}

	if err := c.apply(g, cg, 1, "xy"); err == nil {
		t.Fatal("сдача без подтверждения")
	}
	if err := c.apply(g, cg, 1, "x"); err != nil || cg.resigner != 1 {
		t.Fatalf("запрос сдачи: %v", err)
	}
	if err := c.apply(g, cg, 1, "e2"); err != nil || cg.resigner != 0 {
		t.Fatalf("ход сбрасывает подтверждение: %v", err)
	}
	c.apply(g, cg, 1, "x")
	if err := c.apply(g, cg, 1, "xy"); err != nil || g.Phase != chess.PhaseDone {
		t.Fatalf("сдача: %v", err)
	}
	if !strings.Contains(finalText(g, "", true), "Победа: B") {
		t.Fatal("итог")
	}
}
