package imposter

import (
	"errors"
	"testing"
)

func players(n int) []Player {
	ps := make([]Player, n)
	for i := range ps {
		ps[i] = Player{ID: int64(i + 1), Name: string(rune('A' + i))}
	}
	return ps
}

func newStarted(t *testing.T, n int, s Settings) *Game {
	t.Helper()
	ps := players(n)
	g := NewGame(ps[0], s)
	for _, p := range ps[1:] {
		if _, err := g.Join(p); err != nil {
			t.Fatal(err)
		}
	}
	words := []string{"кошка"}
	if s.Secret {
		words = nil
		for i := range ps {
			words = append(words, "слово"+string(rune('а'+i)))
		}
	}
	if err := g.Start(words); err != nil {
		t.Fatal(err)
	}
	return g
}

func playHints(t *testing.T, g *Game) {
	t.Helper()
	for g.Phase == PhaseHints {
		cur, _ := g.Current()
		if _, err := g.Hint(cur.ID, "подсказка"); err != nil {
			t.Fatal(err)
		}
	}
}

func voteAll(t *testing.T, g *Game, target func(voter Player) int64) {
	t.Helper()
	if err := g.StartVoting(); err != nil {
		t.Fatal(err)
	}
	for _, p := range g.Players {
		to := target(p)
		if to == p.ID {
			to = other(g, p.ID)
		}
		if _, err := g.Vote(p.ID, to); err != nil {
			t.Fatal(err)
		}
	}
}

func other(g *Game, not ...int64) int64 {
	for _, p := range g.Players {
		skip := false
		for _, id := range not {
			skip = skip || p.ID == id
		}
		if !skip {
			return p.ID
		}
	}
	return 0
}

func TestStartNeedsPlayers(t *testing.T) {
	g := NewGame(Player{ID: 1}, DefaultSettings())
	if err := g.Start([]string{"кошка"}); !errors.Is(err, ErrTooFewPlayers) {
		t.Fatalf("got %v", err)
	}
}

func TestRolesNormal(t *testing.T) {
	g := newStarted(t, 5, DefaultSettings())
	impostors := 0
	for _, p := range g.Players {
		c := g.Card(p.ID)
		if c.Impostor {
			impostors++
			if c.Word != "" {
				t.Fatal("импостер знает слово")
			}
		} else if c.Word != "кошка" {
			t.Fatalf("слово игрока: %q", c.Word)
		}
	}
	if impostors != 1 {
		t.Fatalf("импостеров: %d", impostors)
	}
}

func TestRolesSecret(t *testing.T) {
	g := newStarted(t, 4, Settings{Rounds: 1, Secret: true})
	seen := map[string]bool{}
	for _, p := range g.Players {
		c := g.Card(p.ID)
		if c.Impostor || c.Word == "" || seen[c.Word] {
			t.Fatalf("карточка %+v", c)
		}
		seen[c.Word] = true
	}
}

func TestHintRulesAndRounds(t *testing.T) {
	g := newStarted(t, 3, Settings{Rounds: 2, Tie: TieImpostorWins})
	cur, _ := g.Current()
	other := g.Players[1]
	if cur.ID == other.ID {
		other = g.Players[2]
	}
	if _, err := g.Hint(other.ID, "слово"); !errors.Is(err, ErrNotYourTurn) {
		t.Fatalf("got %v", err)
	}
	if _, err := g.Hint(cur.ID, "два слова"); !errors.Is(err, ErrNotOneWord) {
		t.Fatalf("got %v", err)
	}
	for g.Phase == PhaseHints {
		c, _ := g.Current()
		if _, err := g.Hint(c.ID, "мяу"); err != nil {
			t.Fatal(err)
		}
	}
	if g.Phase != PhaseDiscussion || len(g.Hints) != 6 {
		t.Fatalf("phase=%v hints=%d", g.Phase, len(g.Hints))
	}
}

func TestCaughtImpostorGuess(t *testing.T) {
	for _, tc := range []struct {
		guess string
		want  Side
	}{{"Кошка", SideImpostor}, {"собака", SideCrew}} {
		g := newStarted(t, 4, DefaultSettings())
		playHints(t, g)
		voteAll(t, g, func(Player) int64 { return g.Impostor.ID })
		res, err := g.Resolve()
		if err != nil {
			t.Fatal(err)
		}
		if res.Kind != ResCaught {
			t.Fatalf("kind=%v", res.Kind)
		}
		if _, err := g.Guess(other(g, g.Suspect.ID), tc.guess); !errors.Is(err, ErrNotYourTurn) {
			t.Fatalf("got %v", err)
		}
		if side, err := g.Guess(g.Suspect.ID, tc.guess); err != nil || side != tc.want {
			t.Fatalf("side=%v err=%v", side, err)
		}
	}
}

func TestWrongPlayerVoted(t *testing.T) {
	g := newStarted(t, 4, DefaultSettings())
	playHints(t, g)
	victim := other(g, g.Impostor.ID)
	voteAll(t, g, func(Player) int64 { return victim })
	res, _ := g.Resolve()
	if res.Kind != ResWrongPlayer || res.Winner != SideImpostor || g.Phase != PhaseDone {
		t.Fatalf("res=%+v phase=%v", res, g.Phase)
	}
}

func TestTieImpostorWins(t *testing.T) {
	g := newStarted(t, 4, Settings{Rounds: 1, Tie: TieImpostorWins})
	playHints(t, g)
	a, b := g.Players[0].ID, g.Players[1].ID
	if err := g.StartVoting(); err != nil {
		t.Fatal(err)
	}
	for i, p := range g.Players {
		target := a
		if i%2 == 1 || p.ID == a {
			target = b
		}
		if p.ID == b {
			target = a
		}
		if _, err := g.Vote(p.ID, target); err != nil {
			t.Fatal(err)
		}
	}
	res, _ := g.Resolve()
	if res.Kind != ResTie || res.Winner != SideImpostor {
		t.Fatalf("res=%+v", res)
	}
}

func TestTieExtraRound(t *testing.T) {
	g := newStarted(t, 4, Settings{Rounds: 1, Tie: TieExtraRound})
	playHints(t, g)
	tie := func() Resolution {
		if err := g.StartVoting(); err != nil {
			t.Fatal(err)
		}
		// 0->1, 1->0, 2->3, 3->2: у всех по одному голосу
		for i, p := range g.Players {
			if _, err := g.Vote(p.ID, g.Players[i^1].ID); err != nil {
				t.Fatal(err)
			}
		}
		res, _ := g.Resolve()
		return res
	}
	if res := tie(); res.Kind != ResExtraRound || g.Phase != PhaseHints || g.Rounds != 2 || g.Round != 2 {
		t.Fatalf("res=%+v phase=%v rounds=%d/%d", res, g.Phase, g.Round, g.Rounds)
	}
	playHints(t, g)
	if res := tie(); res.Kind != ResTie || res.Winner != SideImpostor {
		t.Fatalf("res=%+v", res)
	}
}

func TestSecretModeNoWinner(t *testing.T) {
	g := newStarted(t, 3, Settings{Rounds: 1, Secret: true})
	playHints(t, g)
	target := g.Players[0].ID
	voteAll(t, g, func(Player) int64 { return target })
	res, _ := g.Resolve()
	if res.Kind != ResSecret || res.Winner != SideNone || res.Suspect.ID != target || g.Phase != PhaseDone {
		t.Fatalf("res=%+v", res)
	}
}

func TestVoteRules(t *testing.T) {
	g := newStarted(t, 3, DefaultSettings())
	playHints(t, g)
	if err := g.StartVoting(); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Vote(1, 1); !errors.Is(err, ErrSelfVote) {
		t.Fatalf("got %v", err)
	}
	if _, err := g.Vote(99, 1); !errors.Is(err, ErrNotPlayer) {
		t.Fatalf("got %v", err)
	}
	if _, err := g.Vote(1, 99); !errors.Is(err, ErrUnknownTarget) {
		t.Fatalf("got %v", err)
	}
}

func TestNormalize(t *testing.T) {
	if Normalize(" Ёлка! ") != "елка" {
		t.Fatal(Normalize(" Ёлка! "))
	}
}

func TestHintLeakEndsGame(t *testing.T) {
	g := newStarted(t, 4, DefaultSettings())
	for {
		cur, _ := g.Current()
		if !g.Card(cur.ID).Impostor {
			leaked, err := g.Hint(cur.ID, "Кошка!")
			if err != nil || !leaked || g.Phase != PhaseDone {
				t.Fatalf("leaked=%v err=%v phase=%v", leaked, err, g.Phase)
			}
			return
		}
		if leaked, err := g.Hint(cur.ID, "кошка"); err != nil || leaked {
			t.Fatalf("импостер: leaked=%v err=%v", leaked, err)
		}
	}
}

func TestDuplicateNamesGetUsername(t *testing.T) {
	g := NewGame(Player{ID: 1, Name: "Саша", Username: "sasha1"}, DefaultSettings())
	if g.Players[0].Name != "Саша" {
		t.Fatal(g.Players[0].Name)
	}
	g.Join(Player{ID: 2, Name: "саша", Username: "sasha2"})
	g.Join(Player{ID: 3, Name: "Маша"})
	if g.Players[0].Name != "Саша (@sasha1)" || g.Players[1].Name != "саша (@sasha2)" || g.Players[2].Name != "Маша" {
		t.Fatalf("%+v", g.Players)
	}
	if g.Host.Name != "Саша (@sasha1)" {
		t.Fatal(g.Host.Name)
	}
	g.Leave(2)
	if g.Players[0].Name != "Саша" || g.Host.Name != "Саша" {
		t.Fatalf("%+v", g.Players)
	}
}

func TestWithinOneTypo(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"кошка", "кошка", true},
		{"кошка", "кошко", true},
		{"кошка", "кошк", true},
		{"кошка", "кошкка", true},
		{"кошка", "кошак", true},
		{"кошка", "ккошш", false},
		{"кошка", "кишка", true},
		{"кошка", "кошечка", false},
		{"кошка", "собака", false},
		{"кошка", "кош", false},
	}
	for _, tt := range tests {
		if got := withinOneTypo(tt.a, tt.b); got != tt.want {
			t.Errorf("withinOneTypo(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
		if got := withinOneTypo(tt.b, tt.a); got != tt.want {
			t.Errorf("withinOneTypo(%q, %q) = %v, want %v", tt.b, tt.a, got, tt.want)
		}
	}
}

func TestMasterDoesNotPlay(t *testing.T) {
	master := Player{ID: 1, Name: "М"}
	g := NewGame(master, Settings{Rounds: 1, Master: true})
	if len(g.Players) != 0 || g.Master.ID != 1 {
		t.Fatalf("%+v", g)
	}
	if _, err := g.Join(master); !errors.Is(err, ErrIsMaster) {
		t.Fatalf("got %v", err)
	}
	for _, p := range players(4)[1:] {
		if _, err := g.Join(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.Start([]string{"кошка"}); err != nil {
		t.Fatal(err)
	}
	if g.HasPlayer(master.ID) || g.Impostor.ID == master.ID {
		t.Fatal("мастер не должен быть игроком или импостером")
	}
}

func TestCleanWord(t *testing.T) {
	if w, ok := CleanWord(" Кошка "); !ok || w != "кошка" {
		t.Fatal(w, ok)
	}
	if _, ok := CleanWord("две слова"); ok {
		t.Fatal("ожидался отказ")
	}
}

func TestHinted(t *testing.T) {
	g := newStarted(t, 3, Settings{Rounds: 2, Tie: TieImpostorWins})
	first, _ := g.Current()
	if g.Hinted(first.ID) {
		t.Fatal("подсказки ещё не было")
	}
	if _, err := g.Hint(first.ID, "мяу"); err != nil {
		t.Fatal(err)
	}
	if !g.Hinted(first.ID) {
		t.Fatal("подсказка должна учитываться")
	}
	for i := 0; i < 2; i++ {
		c, _ := g.Current()
		if _, err := g.Hint(c.ID, "мяу"); err != nil {
			t.Fatal(err)
		}
	}
	if g.Hinted(first.ID) {
		t.Fatal("в новом раунде подсказка должна считаться несделанной")
	}
}
