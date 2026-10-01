// Package chess - шахматная партия двух игроков поверх библиотеки правил notnil/chess.
// Пакет ничего не знает про Telegram: ход делается двумя выборами клетки (фигура, цель).
package chess

import (
	"errors"
	"fmt"

	nc "github.com/notnil/chess"
)

type Phase int

const (
	PhaseWaiting Phase = iota
	PhasePlaying
	PhaseDone
)

var (
	ErrNotPlaying    = errors.New("партия не идёт")
	ErrNotYourTurn   = errors.New("сейчас не ваш ход")
	ErrNotPlayer     = errors.New("вы не участвуете в партии")
	ErrOwnGame       = errors.New("нельзя играть против себя")
	ErrNothingToPick = errors.New("у этой фигуры нет ходов")
	ErrBadSquare     = errors.New("сюда нельзя пойти")
	ErrNoDrawOffer   = errors.New("ничью никто не предлагал")
	ErrDrawOffered   = errors.New("ничья уже предложена")
)

type Player struct {
	ID   int64
	Name string
}

// Outcome - итог партии.
type Outcome struct {
	Winner *Player // nil при ничьей
	Reason string
}

type Game struct {
	White, Black Player
	Phase        Phase
	// Revision растёт при каждом изменении доски и выбора, на неё привязаны кнопки.
	Revision int

	game      *nc.Game
	selected  nc.Square
	promoFrom nc.Square
	promoTo   nc.Square
	drawFrom  int64
	outcome   Outcome
	host      Player
}

// NewGame создаёт партию с ожиданием второго игрока.
func NewGame(host Player) *Game {
	return &Game{
		host:      host,
		game:      nc.NewGame(),
		selected:  nc.NoSquare,
		promoFrom: nc.NoSquare,
		promoTo:   nc.NoSquare,
	}
}

func (g *Game) Host() Player { return g.host }

// Start принимает второго игрока и раздаёт цвета: hostWhite - ведущий играет белыми.
func (g *Game) Start(guest Player, hostWhite bool) error {
	if g.Phase != PhaseWaiting {
		return ErrNotPlaying
	}
	if guest.ID == g.host.ID {
		return ErrOwnGame
	}
	if hostWhite {
		g.White, g.Black = g.host, guest
	} else {
		g.White, g.Black = guest, g.host
	}
	if g.White.Name == g.Black.Name {
		g.White.Name += " (белые)"
		g.Black.Name += " (чёрные)"
	}
	g.Phase = PhasePlaying
	g.Revision++
	return nil
}

func (g *Game) colorOf(id int64) nc.Color {
	switch id {
	case g.White.ID:
		return nc.White
	case g.Black.ID:
		return nc.Black
	}
	return nc.NoColor
}

// Turn - чей сейчас ход.
func (g *Game) Turn() Player {
	if g.game.Position().Turn() == nc.White {
		return g.White
	}
	return g.Black
}

func (g *Game) WhiteToMove() bool { return g.game.Position().Turn() == nc.White }

// Piece возвращает фигуру на клетке, для пустой клетки ok=false.
func (g *Game) Piece(sq nc.Square) (nc.Piece, bool) {
	p := g.game.Position().Board().Piece(sq)
	return p, p != nc.NoPiece
}

func (g *Game) Selected() (nc.Square, bool) { return g.selected, g.selected != nc.NoSquare }

// Targets - клетки, куда может пойти выбранная фигура.
func (g *Game) Targets() map[nc.Square]bool {
	t := map[nc.Square]bool{}
	if g.selected == nc.NoSquare {
		return t
	}
	for _, m := range g.game.ValidMoves() {
		if m.S1() == g.selected {
			t[m.S2()] = true
		}
	}
	return t
}

// LastMove - последний сделанный ход (для подсветки).
func (g *Game) LastMove() (from, to nc.Square, ok bool) {
	moves := g.game.Moves()
	if len(moves) == 0 {
		return nc.NoSquare, nc.NoSquare, false
	}
	m := moves[len(moves)-1]
	return m.S1(), m.S2(), true
}

func (g *Game) InCheck() bool {
	last := g.game.Moves()
	return len(last) > 0 && last[len(last)-1].HasTag(nc.Check)
}

// Promoting - ждём выбор фигуры для превращения пешки.
func (g *Game) Promoting() bool { return g.promoFrom != nc.NoSquare }

func (g *Game) DrawOfferedBy() (Player, bool) {
	switch g.drawFrom {
	case 0:
		return Player{}, false
	case g.White.ID:
		return g.White, true
	}
	return g.Black, true
}

func (g *Game) Outcome() Outcome { return g.outcome }

// MoveCount - число сделанных полуходов.
func (g *Game) MoveCount() int { return len(g.game.Moves()) }

// History - ходы в алгебраической записи через пробел: "1. e4 e5 2. Nf3".
func (g *Game) History() []string {
	var out []string
	notation := nc.AlgebraicNotation{}
	pos := nc.StartingPosition()
	for _, m := range g.game.Moves() {
		out = append(out, notation.Encode(pos, m))
		pos = pos.Update(m)
	}
	return out
}

func (g *Game) check(id int64) error {
	if g.Phase != PhasePlaying {
		return ErrNotPlaying
	}
	if g.colorOf(id) == nc.NoColor {
		return ErrNotPlayer
	}
	return nil
}

func (g *Game) checkTurn(id int64) error {
	if err := g.check(id); err != nil {
		return err
	}
	if g.colorOf(id) != g.game.Position().Turn() {
		return ErrNotYourTurn
	}
	return nil
}

func (g *Game) movesFrom(sq nc.Square) []*nc.Move {
	var out []*nc.Move
	for _, m := range g.game.ValidMoves() {
		if m.S1() == sq {
			out = append(out, m)
		}
	}
	return out
}

func (g *Game) own(sq nc.Square) bool {
	p, ok := g.Piece(sq)
	return ok && p.Color() == g.game.Position().Turn()
}

// Pick обрабатывает нажатие на клетку: выбор фигуры, смена выбора, снятие выбора или ход.
// Для хода пешки на последнюю горизонталь доска ждёт Promote.
func (g *Game) Pick(id int64, sq nc.Square) error {
	if err := g.checkTurn(id); err != nil {
		return err
	}
	if g.Promoting() {
		return errors.New("сначала выберите фигуру для превращения")
	}
	if g.selected == nc.NoSquare || (g.own(sq) && sq != g.selected) {
		if !g.own(sq) {
			return ErrBadSquare
		}
		if len(g.movesFrom(sq)) == 0 {
			return ErrNothingToPick
		}
		g.selected = sq
		g.Revision++
		return nil
	}
	if sq == g.selected {
		g.selected = nc.NoSquare
		g.Revision++
		return nil
	}
	var cand []*nc.Move
	for _, m := range g.movesFrom(g.selected) {
		if m.S2() == sq {
			cand = append(cand, m)
		}
	}
	switch {
	case len(cand) == 0:
		return ErrBadSquare
	case cand[0].Promo() != nc.NoPieceType:
		g.promoFrom, g.promoTo = g.selected, sq
		g.Revision++
		return nil
	}
	return g.apply(cand[0])
}

// Promote завершает ход пешки выбранной фигурой (ферзь, ладья, слон, конь).
func (g *Game) Promote(id int64, piece nc.PieceType) error {
	if err := g.checkTurn(id); err != nil {
		return err
	}
	if !g.Promoting() {
		return ErrBadSquare
	}
	for _, m := range g.movesFrom(g.promoFrom) {
		if m.S2() == g.promoTo && m.Promo() == piece {
			return g.apply(m)
		}
	}
	return ErrBadSquare
}

// CancelPromotion отменяет превращение и возвращает выбор фигуры.
func (g *Game) CancelPromotion(id int64) error {
	if err := g.checkTurn(id); err != nil {
		return err
	}
	g.promoFrom, g.promoTo = nc.NoSquare, nc.NoSquare
	g.selected = nc.NoSquare
	g.Revision++
	return nil
}

func (g *Game) apply(m *nc.Move) error {
	if err := g.game.Move(m); err != nil {
		return fmt.Errorf("ход отклонён: %w", err)
	}
	g.selected, g.promoFrom, g.promoTo = nc.NoSquare, nc.NoSquare, nc.NoSquare
	g.drawFrom = 0
	g.Revision++
	g.finishIfOver()
	return nil
}

// finishIfOver фиксирует итог. Троекратное повторение и правило 50 ходов засчитываются автоматически.
func (g *Game) finishIfOver() {
	if g.game.Outcome() == nc.NoOutcome {
		for _, d := range g.game.EligibleDraws() {
			if d == nc.ThreefoldRepetition || d == nc.FiftyMoveRule {
				_ = g.game.Draw(d)
				break
			}
		}
	}
	switch g.game.Outcome() {
	case nc.NoOutcome:
		return
	case nc.WhiteWon:
		w := g.White
		g.outcome.Winner = &w
	case nc.BlackWon:
		w := g.Black
		g.outcome.Winner = &w
	}
	g.outcome.Reason = methodLabel(g.game.Method())
	g.Phase = PhaseDone
}

func methodLabel(m nc.Method) string {
	switch m {
	case nc.Checkmate:
		return "мат"
	case nc.Resignation:
		return "сдача"
	case nc.DrawOffer:
		return "ничья по согласию"
	case nc.Stalemate:
		return "пат"
	case nc.ThreefoldRepetition, nc.FivefoldRepetition:
		return "троекратное повторение позиции"
	case nc.FiftyMoveRule, nc.SeventyFiveMoveRule:
		return "правило 50 ходов"
	case nc.InsufficientMaterial:
		return "недостаточно материала для мата"
	}
	return ""
}

// Resign - игрок сдаётся (можно не в свой ход).
func (g *Game) Resign(id int64) error {
	if err := g.check(id); err != nil {
		return err
	}
	g.game.Resign(g.colorOf(id))
	g.Revision++
	g.finishIfOver()
	return nil
}

// OfferDraw предлагает ничью сопернику.
func (g *Game) OfferDraw(id int64) error {
	if err := g.check(id); err != nil {
		return err
	}
	if g.drawFrom != 0 {
		return ErrDrawOffered
	}
	g.drawFrom = id
	g.Revision++
	return nil
}

// AnswerDraw: соперник принимает или отклоняет предложенную ничью.
func (g *Game) AnswerDraw(id int64, accept bool) error {
	if err := g.check(id); err != nil {
		return err
	}
	if g.drawFrom == 0 || g.drawFrom == id {
		return ErrNoDrawOffer
	}
	g.drawFrom = 0
	g.Revision++
	if accept {
		_ = g.game.Draw(nc.DrawOffer)
		g.finishIfOver()
	}
	return nil
}

// Abort закрывает партию без результата (команда остановки, простой).
func (g *Game) Abort() {
	g.Phase = PhaseDone
	g.Revision++
}

// CheckPlayer проверяет, что партия идёт и id - её участник.
func (g *Game) CheckPlayer(id int64) error { return g.check(id) }

// Touch обновляет ревизию без изменения доски (для служебных кнопок).
func (g *Game) Touch() { g.Revision++ }
