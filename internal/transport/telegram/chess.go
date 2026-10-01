package telegram

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot/models"
	nc "github.com/notnil/chess"

	"telegram-bot/internal/chess"
)

const (
	chessPrefix      = "chs:"
	chessIdleTimeout = 24 * time.Hour
)

// chessGame - партия в чате вместе с сообщением доски.
type chessGame struct {
	game     *chess.Game
	msgID    int
	rich     bool
	updated  time.Time
	resigner int64 // кто нажал "Сдаться" и ещё не подтвердил
}

type chessController struct {
	mu     sync.Mutex
	tg     *Bot
	store  *chess.Store
	games  map[int64]*chessGame
	logger *slog.Logger
}

func newChessController(tg *Bot, store *chess.Store, logger *slog.Logger) *chessController {
	c := &chessController{
		tg:     tg,
		store:  store,
		games:  make(map[int64]*chessGame),
		logger: logger.With("component", "chess"),
	}
	for chatID, snap := range store.All() {
		g, err := chess.Restore(snap)
		if err != nil || g.Phase != chess.PhasePlaying {
			c.logger.Warn("Партия не восстановлена", "chat", chatID, "err", err)
			_ = store.Delete(chatID)
			continue
		}
		c.games[chatID] = &chessGame{game: g, msgID: snap.MsgID, rich: snap.Rich, updated: snap.Updated}
	}
	return c
}

func (c *chessController) handleCommand(ctx context.Context, msg *models.Message, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if msg.Chat.Type == models.ChatTypePrivate {
		c.say(ctx, msg.Chat.ID, "Шахматы играются в групповом чате. Добавьте меня в группу и напишите там /chess")
		return
	}
	switch name {
	case "chess":
		c.open(ctx, msg)
	case "chess_stop":
		c.stop(ctx, msg)
	}
}

func (c *chessController) open(ctx context.Context, msg *models.Message) {
	if msg.From == nil {
		return
	}
	if cg, busy := c.games[msg.Chat.ID]; busy {
		if time.Since(cg.updated) < chessIdleTimeout {
			c.say(ctx, msg.Chat.ID, "В чате уже идёт партия. Остановить: /chess_stop")
			return
		}
		c.finish(ctx, msg.Chat.ID, cg, "Партия закрыта: слишком долго не было ходов")
	}
	g := chess.NewGame(chessPlayer(msg.From))
	cg := &chessGame{game: g, updated: time.Now()}
	id, err := c.tg.SendHTML(ctx, msg.Chat.ID, waitingText(g), button("Играть", chessPrefix+"0:join"))
	if err != nil {
		c.logger.ErrorContext(ctx, "Не удалось открыть партию", "err", err)
		return
	}
	cg.msgID = id
	c.games[msg.Chat.ID] = cg
}

func (c *chessController) stop(ctx context.Context, msg *models.Message) {
	cg, ok := c.games[msg.Chat.ID]
	if !ok {
		c.say(ctx, msg.Chat.ID, "Сейчас нет активной партии")
		return
	}
	g := cg.game
	if msg.From == nil || (msg.From.ID != g.Host().ID && msg.From.ID != g.White.ID && msg.From.ID != g.Black.ID &&
		!c.tg.IsChatAdmin(ctx, msg.Chat.ID, msg.From.ID)) {
		c.say(ctx, msg.Chat.ID, "Остановить партию может участник или админ чата")
		return
	}
	g.Abort()
	c.finish(ctx, msg.Chat.ID, cg, "Партия остановлена")
}

func (c *chessController) handleCallback(ctx context.Context, q *models.CallbackQuery) {
	if q.Message.Message == nil || !strings.HasPrefix(q.Data, chessPrefix) {
		return
	}
	chatID, msgID := q.Message.Message.Chat.ID, q.Message.Message.ID
	parts := strings.SplitN(strings.TrimPrefix(q.Data, chessPrefix), ":", 2)
	if len(parts) != 2 {
		return
	}
	rev, _ := strconv.Atoi(parts[0])
	action := parts[1]

	answered := false
	answer := func(text string) {
		answered = true
		if err := c.tg.AnswerCallback(ctx, q.ID, text, false); err != nil {
			c.logger.ErrorContext(ctx, "Не удалось ответить на нажатие", "err", err)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() {
		if !answered {
			answer("")
		}
	}()

	cg, ok := c.games[chatID]
	if !ok || cg.msgID != msgID {
		answer("Эта партия уже закончена")
		return
	}
	g := cg.game
	player := chessPlayer(&q.From)

	if action == "join" {
		if g.Phase != chess.PhaseWaiting {
			answer("Партия уже началась")
			return
		}
		if err := g.Start(player, rand.IntN(2) == 0); err != nil {
			answer(err.Error())
			return
		}
		c.begin(ctx, chatID, cg)
		return
	}
	if g.Phase != chess.PhasePlaying {
		answer("Партия ещё не началась")
		return
	}
	if rev != g.Revision {
		answer("Доска уже обновилась")
		return
	}
	if err := c.apply(g, cg, player.ID, action); err != nil {
		answer(err.Error())
		return
	}
	cg.updated = time.Now()
	if g.Phase == chess.PhaseDone {
		c.finish(ctx, chatID, cg, "")
		return
	}
	c.redraw(ctx, chatID, cg)
}

// apply выполняет действие кнопки. Кнопки доски - клетки (a1-h8), остальные описаны ниже.
func (c *chessController) apply(g *chess.Game, cg *chessGame, id int64, action string) error {
	resigner := cg.resigner
	cg.resigner = 0
	switch action {
	case "x":
		if err := g.CheckPlayer(id); err != nil {
			return err
		}
		cg.resigner = id
		g.Touch()
		return nil
	case "xn":
		g.Touch()
		return nil
	case "xy":
		if resigner != id {
			return chess.ErrNotYourTurn
		}
		return g.Resign(id)
	case "d":
		return g.OfferDraw(id)
	case "da":
		return g.AnswerDraw(id, true)
	case "dn":
		return g.AnswerDraw(id, false)
	case "pq":
		return g.Promote(id, nc.Queen)
	case "pr":
		return g.Promote(id, nc.Rook)
	case "pb":
		return g.Promote(id, nc.Bishop)
	case "pn":
		return g.Promote(id, nc.Knight)
	case "pc":
		return g.CancelPromotion(id)
	}
	sq, ok := parseSquare(action)
	if !ok {
		return chess.ErrBadSquare
	}
	return g.Pick(id, sq)
}

func parseSquare(s string) (nc.Square, bool) {
	if len(s) != 2 || s[0] < 'a' || s[0] > 'h' || s[1] < '1' || s[1] > '8' {
		return nc.NoSquare, false
	}
	return nc.Square(int(s[1]-'1')*8 + int(s[0]-'a')), true
}

func squareName(sq nc.Square) string {
	return string([]byte{'a' + byte(int(sq)%8), '1' + byte(int(sq)/8)})
}

// begin отправляет сообщение с доской. Сначала пробует rich (кнопки внутри сообщения), иначе обычная клавиатура.
func (c *chessController) begin(ctx context.Context, chatID int64, cg *chessGame) {
	g := cg.game
	if err := c.tg.EditHTML(ctx, chatID, cg.msgID, fmt.Sprintf("Партия началась: %s играет белыми, %s чёрными",
		html.EscapeString(g.White.Name), html.EscapeString(g.Black.Name)), nil); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось обновить приглашение", "err", err)
	}
	cg.updated = time.Now()
	text, rows := c.render(cg, true)
	id, err := c.tg.SendRich(ctx, chatID, text, nil)
	if err == nil {
		cg.rich = true
	} else {
		c.logger.WarnContext(ctx, "Rich-доска не отправилась, использую обычную клавиатуру", "err", err)
		text, rows = c.render(cg, false)
		id, err = c.tg.SendHTML(ctx, chatID, text, rows)
		if err != nil {
			c.logger.ErrorContext(ctx, "Не удалось отправить доску", "err", err)
			delete(c.games, chatID)
			return
		}
	}
	cg.msgID = id
	c.save(chatID, cg)
}

func (c *chessController) redraw(ctx context.Context, chatID int64, cg *chessGame) {
	text, rows := c.render(cg, cg.rich)
	var err error
	if cg.rich {
		err = c.tg.EditRich(ctx, chatID, cg.msgID, text, nil)
	} else {
		err = c.tg.EditHTML(ctx, chatID, cg.msgID, text, rows)
	}
	if err != nil {
		c.logger.ErrorContext(ctx, "Не удалось обновить доску", "err", err)
	}
	c.save(chatID, cg)
}

// finish убирает кнопки, показывает итог и закрывает партию. note - текст вместо результата, если игра не доиграна.
func (c *chessController) finish(ctx context.Context, chatID int64, cg *chessGame, note string) {
	delete(c.games, chatID)
	if err := c.store.Delete(chatID); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось удалить партию из хранилища", "err", err)
	}
	if cg.game.Phase == chess.PhaseWaiting {
		_ = c.tg.EditHTML(ctx, chatID, cg.msgID, note, nil)
		return
	}
	text := finalText(cg.game, note, cg.rich)
	var err error
	if cg.rich {
		err = c.tg.EditRich(ctx, chatID, cg.msgID, text, nil)
	} else {
		err = c.tg.EditHTML(ctx, chatID, cg.msgID, text, nil)
	}
	if err != nil {
		c.logger.ErrorContext(ctx, "Не удалось показать итог партии", "err", err)
		c.say(ctx, chatID, finalText(cg.game, note, false))
	}
}

func (c *chessController) save(chatID int64, cg *chessGame) {
	if cg.game.Phase != chess.PhasePlaying {
		return
	}
	if err := c.store.Save(chatID, cg.game, cg.msgID, cg.rich, cg.updated); err != nil {
		c.logger.Error("Не удалось сохранить партию", "err", err)
	}
}

func (c *chessController) say(ctx context.Context, chatID int64, text string, markup ...*models.InlineKeyboardMarkup) {
	var kb *models.InlineKeyboardMarkup
	if len(markup) > 0 {
		kb = markup[0]
	}
	if _, err := c.tg.SendHTML(ctx, chatID, text, kb); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось отправить сообщение", "err", err)
	}
}

func chessPlayer(u *models.User) chess.Player {
	p := playerFrom(u)
	return chess.Player{ID: p.ID, Name: p.Name}
}

func waitingText(g *chess.Game) string {
	return fmt.Sprintf("♟ <b>Шахматы</b>\n%s ждёт соперника. Цвета выпадут случайно.", html.EscapeString(g.Host().Name))
}

type cell struct {
	text, data, style string
}

var pieceGlyph = map[nc.PieceType][2]string{
	nc.King:   {"♔", "♚"},
	nc.Queen:  {"♕", "♛"},
	nc.Rook:   {"♖", "♜"},
	nc.Bishop: {"♗", "♝"},
	nc.Knight: {"♘", "♞"},
	nc.Pawn:   {"♙", "♟"},
}

func glyph(p nc.Piece) string {
	idx := 0
	if p.Color() == nc.Black {
		idx = 1
	}
	return pieceGlyph[p.Type()][idx]
}

func colorGlyph(white bool) string {
	if white {
		return "♔"
	}
	return "♚"
}

// board строит 8 рядов клеток, белые снизу. Для кнопок клетки подсвечиваются стилем: выбранная, куда можно пойти, куда можно побить.
func board(g *chess.Game) [][]cell {
	selected, hasSel := g.Selected()
	targets := g.Targets()
	rev := strconv.Itoa(g.Revision)
	rows := make([][]cell, 0, 8)
	for rank := 7; rank >= 0; rank-- {
		row := make([]cell, 0, 8)
		for file := 0; file < 8; file++ {
			sq := nc.Square(rank*8 + file)
			cl := cell{data: chessPrefix + rev + ":" + squareName(sq)}
			piece, occupied := g.Piece(sq)
			switch {
			case occupied:
				cl.text = glyph(piece)
			case (rank+file)%2 == 0:
				cl.text = "▪"
			default:
				cl.text = "▫"
			}
			switch {
			case hasSel && sq == selected:
				cl.style = "primary"
			case targets[sq] && occupied:
				cl.style = "danger"
			case targets[sq]:
				cl.style = "success"
				cl.text = "•"
			}
			row = append(row, cl)
		}
		rows = append(rows, row)
	}
	return rows
}

// controls - нижний ряд кнопок в зависимости от состояния партии.
func (c *chessController) controls(cg *chessGame) []cell {
	g := cg.game
	rev := strconv.Itoa(g.Revision)
	d := func(text, action, style string) cell {
		return cell{text: text, data: chessPrefix + rev + ":" + action, style: style}
	}
	if g.Promoting() {
		white := g.WhiteToMove()
		return []cell{
			d(colorGlyph2(nc.Queen, white), "pq", "primary"), d(colorGlyph2(nc.Rook, white), "pr", ""),
			d(colorGlyph2(nc.Bishop, white), "pb", ""), d(colorGlyph2(nc.Knight, white), "pn", ""),
			d("Отмена", "pc", ""),
		}
	}
	if cg.resigner != 0 {
		return []cell{d("Сдаться", "xy", "danger"), d("Отмена", "xn", "")}
	}
	if _, offered := g.DrawOfferedBy(); offered {
		return []cell{d("Принять ничью", "da", "success"), d("Отклонить", "dn", ""), d("Сдаться", "x", "")}
	}
	return []cell{d("Сдаться", "x", ""), d("Предложить ничью", "d", "")}
}

func colorGlyph2(t nc.PieceType, white bool) string {
	if white {
		return pieceGlyph[t][0]
	}
	return pieceGlyph[t][1]
}

// header - строки над доской. Имена игроков экранируются.
func header(cg *chessGame) []string {
	g := cg.game
	lines := []string{
		"♟ <b>Шахматы</b>",
		fmt.Sprintf("♔ %s - ♚ %s", html.EscapeString(g.White.Name), html.EscapeString(g.Black.Name)),
	}
	turn := fmt.Sprintf("Ход: %s <b>%s</b>", colorGlyph(g.WhiteToMove()), html.EscapeString(g.Turn().Name))
	if g.InCheck() {
		turn += " (шах)"
	}
	lines = append(lines, turn)
	if hist := g.History(); len(hist) > 0 {
		lines = append(lines, "Последний ход: "+moveLabel(hist, len(hist)-1))
	}
	if sq, ok := g.Selected(); ok {
		p, _ := g.Piece(sq)
		lines = append(lines, fmt.Sprintf("Выбрано: %s %s", glyph(p), squareName(sq)))
	}
	if p, ok := g.DrawOfferedBy(); ok {
		lines = append(lines, fmt.Sprintf("<b>%s</b> предлагает ничью", html.EscapeString(p.Name)))
	}
	if cg.resigner != 0 {
		lines = append(lines, "Точно сдаться?")
	}
	if g.Promoting() {
		lines = append(lines, "Выберите фигуру для превращения пешки")
	}
	return lines
}

// moveLabel - ход в записи "12. Nf3" или "12... Nf6".
func moveLabel(hist []string, i int) string {
	n := i/2 + 1
	if i%2 == 0 {
		return fmt.Sprintf("%d. %s", n, html.EscapeString(hist[i]))
	}
	return fmt.Sprintf("%d... %s", n, html.EscapeString(hist[i]))
}

// render собирает сообщение доски. rich=true: кнопки внутри html, иначе возвращает клавиатуру под сообщением.
func (c *chessController) render(cg *chessGame, rich bool) (string, *models.InlineKeyboardMarkup) {
	rows := append(board(cg.game), c.controls(cg))
	head := header(cg)
	if !rich {
		kb := &models.InlineKeyboardMarkup{}
		for _, r := range rows {
			var out []models.InlineKeyboardButton
			for _, cl := range r {
				out = append(out, models.InlineKeyboardButton{Text: cl.text, CallbackData: cl.data})
			}
			kb.InlineKeyboard = append(kb.InlineKeyboard, out)
		}
		return strings.Join(head, "\n"), kb
	}
	var b strings.Builder
	b.WriteString("<h3>" + head[0][len("♟ "):] + "</h3>")
	b.WriteString("<p>" + strings.Join(head[1:], "<br>") + "</p>")
	for _, r := range rows {
		b.WriteString(`<tg-button-row align="center">`)
		for _, cl := range r {
			style := ""
			if cl.style != "" {
				style = ` style="` + cl.style + `"`
			}
			fmt.Fprintf(&b, `<tg-button type="callback_data"%s data="%s">%s</tg-button>`, style, cl.data, html.EscapeString(cl.text))
		}
		b.WriteString("</tg-button-row>")
	}
	return b.String(), nil
}

// finalText - итог партии: результат, доска текстом и ходы.
func finalText(g *chess.Game, note string, rich bool) string {
	var lines []string
	lines = append(lines, "♟ <b>Шахматы</b>", fmt.Sprintf("♔ %s - ♚ %s", html.EscapeString(g.White.Name), html.EscapeString(g.Black.Name)))
	o := g.Outcome()
	switch {
	case note != "":
		lines = append(lines, "<b>"+html.EscapeString(note)+"</b>")
	case o.Winner != nil:
		lines = append(lines, fmt.Sprintf("<b>Победа: %s</b> (%s)", html.EscapeString(o.Winner.Name), o.Reason))
	default:
		lines = append(lines, fmt.Sprintf("<b>Ничья</b> (%s)", o.Reason))
	}
	var pre strings.Builder
	for rank := 7; rank >= 0; rank-- {
		for file := 0; file < 8; file++ {
			p, ok := g.Piece(nc.Square(rank*8 + file))
			switch {
			case ok:
				pre.WriteString(glyph(p))
			case (rank+file)%2 == 0:
				pre.WriteString("▪")
			default:
				pre.WriteString("▫")
			}
		}
		pre.WriteString("\n")
	}
	hist := g.History()
	var moves []string
	for i := 0; i < len(hist); i += 2 {
		m := fmt.Sprintf("%d. %s", i/2+1, html.EscapeString(hist[i]))
		if i+1 < len(hist) {
			m += " " + html.EscapeString(hist[i+1])
		}
		moves = append(moves, m)
	}
	sep := "\n"
	if rich {
		sep = "<br>"
	}
	out := strings.Join(lines, sep) + sep + "<pre>" + strings.TrimRight(pre.String(), "\n") + "</pre>"
	if len(moves) > 0 {
		out += sep + strings.Join(moves, " ")
	}
	return out
}
