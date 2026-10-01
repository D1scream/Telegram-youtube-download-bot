package imposter

import (
	"errors"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MinPlayers   = 3
	MaxPlayers   = 12
	MaxRounds    = 5
	maxHintRunes = 40
)

type Phase int

const (
	PhaseLobby Phase = iota
	PhaseHints
	PhaseDiscussion
	PhaseVoting
	PhaseGuess
	PhaseDone
)

type TiePolicy string

const (
	TieImpostorWins TiePolicy = "impostor"
	TieExtraRound   TiePolicy = "round"
)

type Settings struct {
	Rounds int       `json:"rounds"`
	Tie    TiePolicy `json:"tie"`
	Secret bool      `json:"-"`
	Master bool      `json:"master"`
	List   string    `json:"list"`
}

func DefaultSettings() Settings {
	return Settings{Rounds: 1, Tie: TieImpostorWins, List: ListDownloaded}
}

type Player struct {
	ID       int64
	Name     string
	Username string

	base string
}

type Hint struct {
	Player Player
	Text   string
	Round  int
}

type Side int

const (
	SideNone Side = iota
	SideCrew
	SideImpostor
)

type Card struct {
	Word     string
	Impostor bool
}

type ResolutionKind int

const (
	ResExtraRound ResolutionKind = iota
	ResTie
	ResWrongPlayer
	ResCaught
	ResSecret
)

type Resolution struct {
	Kind    ResolutionKind
	Suspect Player
	Counts  []VoteCount
	Winner  Side
}

type VoteCount struct {
	Player Player
	Votes  int
}

var (
	ErrWrongPhase    = errors.New("неподходящая стадия игры")
	ErrNotPlayer     = errors.New("вы не участвуете в игре")
	ErrNotYourTurn   = errors.New("сейчас не ваш ход")
	ErrNotOneWord    = errors.New("нужно ровно одно слово")
	ErrSelfVote      = errors.New("за себя голосовать нельзя")
	ErrUnknownTarget = errors.New("такого игрока нет")
	ErrIsMaster      = errors.New("мастер не играет")
	ErrFull          = errors.New("лобби заполнено")
	ErrTooFewPlayers = errors.New("слишком мало игроков")
	ErrWordsCount    = errors.New("неверное количество слов")
)

type Game struct {
	Settings Settings
	Phase    Phase
	Host     Player
	Master   Player
	Players  []Player
	Hints    []Hint
	Round    int
	Rounds   int
	Impostor Player
	Suspect  Player

	extraUsed bool
	turn      int
	word      string
	words     map[int64]string
	votes     map[int64]int64
}

func NewGame(host Player, settings Settings) *Game {
	settings.Rounds = min(max(settings.Rounds, 1), MaxRounds)
	g := &Game{Settings: settings, Host: host, Players: []Player{host}}
	if settings.Master {
		g.Master, g.Players = host, nil
	}
	g.relabel()
	return g
}

// relabel делает имена игроков уникальными: к одинаковым добавляется @username.
func (g *Game) relabel() {
	seen := make(map[string]int, len(g.Players))
	for i := range g.Players {
		if g.Players[i].base == "" {
			g.Players[i].base = g.Players[i].Name
		}
		seen[strings.ToLower(g.Players[i].base)]++
	}
	for i, p := range g.Players {
		p.Name = p.base
		if seen[strings.ToLower(p.base)] > 1 {
			if p.Username != "" {
				p.Name += " (@" + p.Username + ")"
			} else {
				p.Name += " #" + strconv.FormatInt(p.ID%10000, 10)
			}
		}
		g.Players[i] = p
		if p.ID == g.Host.ID {
			g.Host = p
		}
	}
}

func (g *Game) HasPlayer(id int64) bool {
	return slices.ContainsFunc(g.Players, func(p Player) bool { return p.ID == id })
}

func (g *Game) Join(p Player) (bool, error) {
	if g.Phase != PhaseLobby {
		return false, ErrWrongPhase
	}
	if g.Settings.Master && p.ID == g.Master.ID {
		return false, ErrIsMaster
	}
	if g.HasPlayer(p.ID) {
		return false, nil
	}
	if len(g.Players) >= MaxPlayers {
		return false, ErrFull
	}
	g.Players = append(g.Players, p)
	g.relabel()
	return true, nil
}

func (g *Game) Leave(id int64) bool {
	if g.Phase != PhaseLobby || id == g.Host.ID || !g.HasPlayer(id) {
		return false
	}
	g.Players = slices.DeleteFunc(g.Players, func(p Player) bool { return p.ID == id })
	g.relabel()
	return true
}

func (g *Game) WordsNeeded() int {
	if g.Settings.Secret {
		return len(g.Players)
	}
	return 1
}

// Start распределяет роли и слова. words должно содержать WordsNeeded() слов.
func (g *Game) Start(words []string) error {
	if g.Phase != PhaseLobby {
		return ErrWrongPhase
	}
	if len(g.Players) < MinPlayers {
		return ErrTooFewPlayers
	}
	if len(words) != g.WordsNeeded() {
		return ErrWordsCount
	}

	rand.Shuffle(len(g.Players), func(i, j int) { g.Players[i], g.Players[j] = g.Players[j], g.Players[i] })
	g.words = make(map[int64]string, len(g.Players))
	if g.Settings.Secret {
		for i, p := range g.Players {
			g.words[p.ID] = words[i]
		}
	} else {
		g.Impostor = g.Players[rand.IntN(len(g.Players))]
		g.word = words[0]
		for _, p := range g.Players {
			if p.ID != g.Impostor.ID {
				g.words[p.ID] = words[0]
			}
		}
	}
	g.Phase = PhaseHints
	g.Round, g.Rounds, g.turn = 1, g.Settings.Rounds, 0
	g.Hints, g.votes, g.extraUsed = nil, nil, false
	return nil
}

// Abort возвращает игру в лобби (например, если не удалось разослать роли).
func (g *Game) Abort() {
	g.Phase = PhaseLobby
	g.words, g.Hints, g.votes = nil, nil, nil
	g.word = ""
	g.Impostor = Player{}
}

func (g *Game) Card(id int64) Card {
	word, ok := g.words[id]
	return Card{Word: word, Impostor: !ok}
}

// Word возвращает общее загаданное слово (пусто в секретном режиме).
func (g *Game) Word() string { return g.word }

func (g *Game) Current() (Player, bool) {
	if g.Phase != PhaseHints {
		return Player{}, false
	}
	return g.Players[g.turn], true
}

// Hint принимает подсказку от игрока, чей сейчас ход. Если игрок, знающий слово,
// назвал его, игра сразу заканчивается победой импостера (leaked == true).
func (g *Game) Hint(id int64, text string) (leaked bool, err error) {
	cur, ok := g.Current()
	if !ok {
		return false, ErrWrongPhase
	}
	if cur.ID != id {
		return false, ErrNotYourTurn
	}
	text = strings.TrimSpace(text)
	if !isOneWord(text) {
		return false, ErrNotOneWord
	}

	g.Hints = append(g.Hints, Hint{Player: cur, Text: text, Round: g.Round})
	if !g.Settings.Secret && id != g.Impostor.ID && strings.Contains(Normalize(text), Normalize(g.word)) {
		g.Phase = PhaseDone
		return true, nil
	}
	g.turn++
	if g.turn == len(g.Players) {
		g.turn = 0
		if g.Round == g.Rounds {
			g.Phase = PhaseDiscussion
		} else {
			g.Round++
		}
	}
	return false, nil
}

// Hinted сообщает, давал ли игрок подсказку в текущем раунде.
func (g *Game) Hinted(id int64) bool {
	return slices.ContainsFunc(g.Hints, func(h Hint) bool { return h.Player.ID == id && h.Round == g.Round })
}

func (g *Game) StartVoting() error {
	if g.Phase != PhaseDiscussion {
		return ErrWrongPhase
	}
	g.Phase = PhaseVoting
	g.votes = make(map[int64]int64, len(g.Players))
	return nil
}

// Vote записывает голос и сообщает, проголосовали ли уже все.
func (g *Game) Vote(voter, target int64) (all bool, err error) {
	if g.Phase != PhaseVoting {
		return false, ErrWrongPhase
	}
	if !g.HasPlayer(voter) {
		return false, ErrNotPlayer
	}
	if !g.HasPlayer(target) {
		return false, ErrUnknownTarget
	}
	if voter == target {
		return false, ErrSelfVote
	}
	g.votes[voter] = target
	return len(g.votes) == len(g.Players), nil
}

func (g *Game) Voted() int { return len(g.votes) }

func (g *Game) Resolve() (Resolution, error) {
	if g.Phase != PhaseVoting {
		return Resolution{}, ErrWrongPhase
	}
	tally := make(map[int64]int, len(g.Players))
	for _, target := range g.votes {
		tally[target]++
	}
	counts := make([]VoteCount, 0, len(g.Players))
	for _, p := range g.Players {
		counts = append(counts, VoteCount{Player: p, Votes: tally[p.ID]})
	}
	slices.SortStableFunc(counts, func(a, b VoteCount) int { return b.Votes - a.Votes })
	res := Resolution{Counts: counts}
	tie := counts[0].Votes == counts[1].Votes

	if g.Settings.Secret {
		g.Phase = PhaseDone
		res.Kind = ResSecret
		if !tie {
			res.Suspect = counts[0].Player
		}
		return res, nil
	}

	if tie {
		if g.Settings.Tie == TieExtraRound && !g.extraUsed {
			g.extraUsed = true
			g.Rounds++
			g.Round++
			g.turn, g.votes = 0, nil
			g.Phase = PhaseHints
			res.Kind = ResExtraRound
			return res, nil
		}
		g.Phase = PhaseDone
		res.Kind, res.Winner = ResTie, SideImpostor
		return res, nil
	}

	res.Suspect = counts[0].Player
	g.Suspect = res.Suspect
	if res.Suspect.ID == g.Impostor.ID {
		g.Phase = PhaseGuess
		res.Kind = ResCaught
		return res, nil
	}
	g.Phase = PhaseDone
	res.Kind, res.Winner = ResWrongPlayer, SideImpostor
	return res, nil
}

// Guess - последний шанс пойманного импостера. Возвращает победившую сторону.
func (g *Game) Guess(id int64, text string) (Side, error) {
	if g.Phase != PhaseGuess {
		return SideNone, ErrWrongPhase
	}
	if id != g.Suspect.ID {
		return SideNone, ErrNotYourTurn
	}
	g.Phase = PhaseDone
	if withinOneTypo(Normalize(text), Normalize(g.Word())) {
		return SideImpostor, nil
	}
	return SideCrew, nil
}

// Normalize приводит слово к виду для сравнения: нижний регистр, ё=е, без знаков по краям.
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}))
	return strings.ReplaceAll(s, "ё", "е")
}

// withinOneTypo: слова совпадают с точностью до одной опечатки
// (лишняя, пропущенная или заменённая буква, либо две соседние буквы местами).
func withinOneTypo(a, b string) bool {
	if a == b {
		return true
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) < len(rb) {
		ra, rb = rb, ra
	}
	if len(ra)-len(rb) > 1 {
		return false
	}
	i := 0
	for i < len(rb) && ra[i] == rb[i] {
		i++
	}
	switch {
	case len(ra) == len(rb):
		if slices.Equal(ra[i+1:], rb[i+1:]) {
			return true
		}
		return i+1 < len(ra) && ra[i] == rb[i+1] && ra[i+1] == rb[i] && slices.Equal(ra[i+2:], rb[i+2:])
	default:
		return slices.Equal(ra[i+1:], rb[i:])
	}
}

// CleanWord проверяет слово, загаданное мастером, и приводит его к нижнему регистру.
func CleanWord(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !isOneWord(s) {
		return "", false
	}
	return strings.ToLower(s), true
}

func isOneWord(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > maxHintRunes || strings.ContainsFunc(s, unicode.IsSpace) {
		return false
	}
	return strings.ContainsFunc(s, unicode.IsLetter)
}
