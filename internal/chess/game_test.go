package chess

import (
	"path/filepath"
	"testing"
	"time"

	nc "github.com/notnil/chess"
)

var (
	alice = Player{ID: 1, Name: "Alice"}
	bob   = Player{ID: 2, Name: "Bob"}
)

func newPlaying(t *testing.T) *Game {
	t.Helper()
	g := NewGame(alice)
	if err := g.Start(bob, true); err != nil {
		t.Fatal(err)
	}
	return g
}

func play(t *testing.T, g *Game, moves ...[2]nc.Square) {
	t.Helper()
	for _, m := range moves {
		id := g.Turn().ID
		if err := g.Pick(id, m[0]); err != nil {
			t.Fatalf("выбор %s: %v", m[0], err)
		}
		if err := g.Pick(id, m[1]); err != nil {
			t.Fatalf("ход %s-%s: %v", m[0], m[1], err)
		}
	}
}

func TestStartRules(t *testing.T) {
	g := NewGame(alice)
	if err := g.Start(alice, true); err != ErrOwnGame {
		t.Fatalf("против себя: %v", err)
	}
	if err := g.Start(bob, false); err != nil || g.White.ID != bob.ID || g.Black.ID != alice.ID {
		t.Fatalf("цвета: %v %+v %+v", err, g.White, g.Black)
	}
}

func TestTwoStepMove(t *testing.T) {
	g := newPlaying(t)
	if err := g.Pick(bob.ID, nc.E2); err != ErrNotYourTurn {
		t.Fatalf("чужой ход: %v", err)
	}
	if err := g.Pick(alice.ID, nc.E2); err != nil {
		t.Fatal(err)
	}
	if got := g.Targets(); !got[nc.E3] || !got[nc.E4] || len(got) != 2 {
		t.Fatalf("цели: %v", got)
	}
	if err := g.Pick(alice.ID, nc.E5); err != ErrBadSquare {
		t.Fatalf("недопустимая цель: %v", err)
	}
	if err := g.Pick(alice.ID, nc.E2); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.Selected(); ok {
		t.Fatal("выбор не снят")
	}
	play(t, g, [2]nc.Square{nc.E2, nc.E4})
	if g.Turn().ID != bob.ID || g.MoveCount() != 1 {
		t.Fatal("ход не перешёл")
	}
	if err := g.Pick(bob.ID, nc.A8); err != ErrNothingToPick {
		t.Fatalf("фигура без ходов: %v", err)
	}
}

func TestReselect(t *testing.T) {
	g := newPlaying(t)
	g.Pick(alice.ID, nc.E2)
	if err := g.Pick(alice.ID, nc.G1); err != nil {
		t.Fatal(err)
	}
	if sq, _ := g.Selected(); sq != nc.G1 {
		t.Fatalf("выбрано %s", sq)
	}
}

func TestFoolsMate(t *testing.T) {
	g := newPlaying(t)
	play(t, g,
		[2]nc.Square{nc.F2, nc.F3}, [2]nc.Square{nc.E7, nc.E5},
		[2]nc.Square{nc.G2, nc.G4}, [2]nc.Square{nc.D8, nc.H4})
	if g.Phase != PhaseDone {
		t.Fatal("партия не завершена")
	}
	if o := g.Outcome(); o.Winner == nil || o.Winner.ID != bob.ID || o.Reason != "мат" {
		t.Fatalf("итог: %+v", o)
	}
}

func TestPromotion(t *testing.T) {
	g := newPlaying(t)
	fen, _ := nc.FEN("8/P6k/8/8/8/8/8/K7 w - - 0 1")
	g.game = nc.NewGame(fen)
	g.Pick(alice.ID, nc.A7)
	if err := g.Pick(alice.ID, nc.A8); err != nil || !g.Promoting() {
		t.Fatalf("превращение не запрошено: %v", err)
	}
	if err := g.Promote(alice.ID, nc.Knight); err != nil {
		t.Fatal(err)
	}
	p, _ := g.Piece(nc.A8)
	if p.Type() != nc.Knight {
		t.Fatalf("на a8 %s", p)
	}
}

func TestResignAndDraw(t *testing.T) {
	g := newPlaying(t)
	if err := g.AnswerDraw(bob.ID, true); err != ErrNoDrawOffer {
		t.Fatalf("без предложения: %v", err)
	}
	g.OfferDraw(alice.ID)
	if err := g.OfferDraw(bob.ID); err != ErrDrawOffered {
		t.Fatalf("повторное: %v", err)
	}
	if err := g.AnswerDraw(alice.ID, true); err != ErrNoDrawOffer {
		t.Fatalf("сам себе: %v", err)
	}
	g.AnswerDraw(bob.ID, true)
	if g.Phase != PhaseDone || g.Outcome().Winner != nil {
		t.Fatalf("ничья: %+v", g.Outcome())
	}

	g = newPlaying(t)
	g.Resign(bob.ID)
	if o := g.Outcome(); o.Winner == nil || o.Winner.ID != alice.ID {
		t.Fatalf("сдача: %+v", o)
	}
	if err := g.Pick(alice.ID, nc.E2); err != ErrNotPlaying {
		t.Fatalf("ход после конца: %v", err)
	}
}

func TestMoveClearsDrawOffer(t *testing.T) {
	g := newPlaying(t)
	g.OfferDraw(bob.ID)
	play(t, g, [2]nc.Square{nc.E2, nc.E4})
	if _, ok := g.DrawOfferedBy(); ok {
		t.Fatal("предложение ничьей не сброшено")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chess.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	g := newPlaying(t)
	play(t, g, [2]nc.Square{nc.E2, nc.E4}, [2]nc.Square{nc.E7, nc.E5})
	g.OfferDraw(alice.ID)
	if err := s.Save(-100, g, 7, true, time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}

	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	snap, ok := s2.All()[-100]
	if !ok {
		t.Fatal("партия не сохранилась")
	}
	r, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if r.game.FEN() != g.game.FEN() || r.MoveCount() != 2 || r.White != g.White || r.Phase != PhasePlaying {
		t.Fatalf("восстановлено неверно: %s", r.game.FEN())
	}
	if snap.MsgID != 7 || !snap.Rich || !snap.Updated.Equal(time.Unix(100, 0)) {
		t.Fatalf("метаданные: %+v", snap)
	}
	if p, ok := r.DrawOfferedBy(); !ok || p.ID != alice.ID {
		t.Fatal("предложение ничьей потеряно")
	}
	if err := s2.Delete(-100); err != nil || len(s2.All()) != 0 {
		t.Fatal("удаление")
	}
}
