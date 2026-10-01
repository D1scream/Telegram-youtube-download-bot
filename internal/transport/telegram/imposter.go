package telegram

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/go-telegram/bot/models"

	"telegram-bot/internal/imposter"
)

const imposterHelp = `Импостер
/imposter - набрать игроков
/imposter_settings - настройки
/imposter_stop - остановить игру (ведущий или админ)
/topic - свои темы со списками слов (админы чата)
/word <слово> - подсказка на своём ходу (или догадка пойманного импостера)
Чтобы получать слова, каждый игрок должен один раз написать боту в личку /start`

type chatGame struct {
	awaiting bool
	game     *imposter.Game
	title    string
	lobbyMsg int
	voteMsg  int
}

type imposterController struct {
	mu       sync.Mutex
	tg       *Bot
	bank     *imposter.WordList
	defaults imposter.Settings
	store    *imposter.Store
	secret   map[int64]bool
	games    map[int64]*chatGame
	logger   *slog.Logger
}

func newImposterController(tg *Bot, bank *imposter.WordList, store *imposter.Store, logger *slog.Logger) *imposterController {
	return &imposterController{
		tg:       tg,
		bank:     bank,
		defaults: imposter.DefaultSettings(),
		store:    store,
		secret:   make(map[int64]bool),
		games:    make(map[int64]*chatGame),
		logger:   logger.With("component", "imposter"),
	}
}

func (c *imposterController) settingsFor(chatID int64) imposter.Settings {
	return c.store.Settings(chatID, c.defaults)
}

// pickWords выбирает n слов из списка, заданного в настройках игры.
func (c *imposterController) pickWords(chatID int64, list string, n int) ([]string, error) {
	if name, ok := imposter.TopicName(list); ok {
		words, found := c.store.TopicWords(chatID, name)
		if !found {
			return nil, imposter.ErrNoTopic
		}
		return imposter.PickWords(words, n)
	}
	return c.bank.Pick(n)
}

func (c *imposterController) handleCommand(ctx context.Context, msg *models.Message, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if msg.Chat.Type == models.ChatTypePrivate {
		if name == "imposter_settings" && msg.From != nil {
			c.showSecretSettings(ctx, msg.Chat.ID, msg.From.ID)
			return
		}
		c.say(ctx, msg.Chat.ID, "Игра идёт в групповом чате. Добавьте меня в группу и напишите там /imposter")
		return
	}
	switch name {
	case "imposter":
		c.openLobby(ctx, msg)
	case "imposter_settings":
		c.showSettings(ctx, msg.Chat.ID)
	case "imposter_stop":
		c.stop(ctx, msg)
	}
}

func (c *imposterController) openLobby(ctx context.Context, msg *models.Message) {
	if msg.From == nil {
		return
	}
	if _, busy := c.games[msg.Chat.ID]; busy {
		c.say(ctx, msg.Chat.ID, "Игра уже идёт. Остановить: /imposter_stop")
		return
	}
	settings := c.settingsFor(msg.Chat.ID)
	if c.secret[msg.From.ID] {
		delete(c.secret, msg.From.ID)
		settings.Secret, settings.Master = true, false
	}
	cg := &chatGame{
		game:  imposter.NewGame(playerFrom(msg.From), settings),
		title: msg.Chat.Title,
	}
	id, err := c.tg.SendHTML(ctx, msg.Chat.ID, lobbyText(cg.game), lobbyKeyboard())
	if err != nil {
		c.logger.ErrorContext(ctx, "Не удалось открыть набор игроков", "err", err)
		return
	}
	cg.lobbyMsg = id
	c.games[msg.Chat.ID] = cg
}

func (c *imposterController) stop(ctx context.Context, msg *models.Message) {
	cg, ok := c.games[msg.Chat.ID]
	if !ok {
		c.say(ctx, msg.Chat.ID, "Сейчас нет активной игры")
		return
	}
	if msg.From == nil || (msg.From.ID != cg.game.Host.ID && !c.tg.IsChatAdmin(ctx, msg.Chat.ID, msg.From.ID)) {
		c.say(ctx, msg.Chat.ID, "Остановить игру может ведущий или админ чата")
		return
	}
	delete(c.games, msg.Chat.ID)
	c.say(ctx, msg.Chat.ID, "Игра остановлена."+revealText(cg.game))
}

const topicUsage = `Темы (свои списки слов для игры в этом чате)
/topic - список тем
/topic show Название - слова темы
/topic add Название: слово - добавить одно слово (тема создаётся сама)
/topic remove Название: слово - убрать одно слово
/topic delete Название - удалить тему целиком
Изменять темы могут админы чата. Выбрать тему: /imposter_settings, кнопка "Слова"`

// maxTopicShowRunes ограничивает вывод /topic show: сообщение Telegram не длиннее 4096 символов.
const maxTopicShowRunes = 3500

// handleTopic управляет темами чата: /topic [show|add|remove|delete] ...
func (c *imposterController) handleTopic(ctx context.Context, msg *models.Message, args string) {
	if msg.From == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if msg.Chat.Type == models.ChatTypePrivate {
		c.reply(ctx, msg, "Темы настраиваются в групповом чате")
		return
	}
	chatID := msg.Chat.ID
	action, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	action = strings.ToLower(action)

	switch action {
	case "", "list":
		c.topicList(ctx, msg)
		return
	case "show":
		c.topicShow(ctx, msg, strings.TrimSpace(rest))
		return
	case "add", "remove", "delete":
	default:
		c.reply(ctx, msg, topicUsage)
		return
	}
	if !c.tg.IsChatAdmin(ctx, chatID, msg.From.ID) {
		c.reply(ctx, msg, "Изменять темы могут только админы чата")
		return
	}

	name, wordText, hasWord := strings.Cut(rest, ":")
	name = strings.TrimSpace(name)
	if name == "" {
		c.reply(ctx, msg, topicUsage)
		return
	}
	if action == "delete" {
		if err := c.store.DeleteTopic(chatID, name); err != nil {
			c.reply(ctx, msg, "Не получилось: "+err.Error())
			return
		}
		c.reply(ctx, msg, fmt.Sprintf("Тема \"%s\" удалена", name))
		return
	}

	word, ok := imposter.CleanWord(wordText)
	if !hasWord || !ok {
		c.reply(ctx, msg, fmt.Sprintf("Нужно ровно одно слово после двоеточия: /topic %s Название: слово", action))
		return
	}
	if action == "add" {
		added, err := c.store.AddTopicWords(chatID, name, []string{word})
		switch {
		case err != nil:
			c.reply(ctx, msg, "Не получилось: "+err.Error())
		case added == 0:
			c.reply(ctx, msg, fmt.Sprintf("Слово \"%s\" уже есть в теме \"%s\"", word, name))
		default:
			c.reply(ctx, msg, fmt.Sprintf("Слово \"%s\" добавлено в тему \"%s\"", word, name))
		}
		return
	}
	removed, err := c.store.RemoveTopicWords(chatID, name, []string{word})
	switch {
	case err != nil:
		c.reply(ctx, msg, "Не получилось: "+err.Error())
	case removed == 0:
		c.reply(ctx, msg, fmt.Sprintf("Слова \"%s\" нет в теме \"%s\"", word, name))
	default:
		c.reply(ctx, msg, fmt.Sprintf("Слово \"%s\" убрано из темы \"%s\"", word, name))
	}
}

func (c *imposterController) topicList(ctx context.Context, msg *models.Message) {
	names := c.store.TopicNames(msg.Chat.ID)
	if len(names) == 0 {
		c.reply(ctx, msg, "Тем пока нет.\n\n"+topicUsage)
		return
	}
	var b strings.Builder
	b.WriteString("Темы чата:\n")
	for _, name := range names {
		words, _ := c.store.TopicWords(msg.Chat.ID, name)
		fmt.Fprintf(&b, "- %s (слов: %d)\n", name, len(words))
	}
	c.reply(ctx, msg, b.String()+"\n"+topicUsage)
}

func (c *imposterController) topicShow(ctx context.Context, msg *models.Message, name string) {
	words, ok := c.store.TopicWords(msg.Chat.ID, name)
	if !ok {
		c.reply(ctx, msg, imposter.ErrNoTopic.Error())
		return
	}
	slices.Sort(words)
	text := strings.Join(words, ", ")
	if runes := []rune(text); len(runes) > maxTopicShowRunes {
		text = string(runes[:maxTopicShowRunes]) + "..."
	}
	c.reply(ctx, msg, fmt.Sprintf("Тема \"%s\", слов: %d\n%s", name, len(words), text))
}

// handleWord принимает /word: подсказку в фазе подсказок или догадку пойманного импостера.
func (c *imposterController) handleWord(ctx context.Context, msg *models.Message, text string) {
	if msg.From == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if msg.Chat.Type == models.ChatTypePrivate {
		c.masterWord(ctx, msg, text)
		return
	}

	cg, ok := c.games[msg.Chat.ID]
	if !ok {
		c.reply(ctx, msg, "Сейчас нет активной игры. Начать: /imposter")
		return
	}
	text = strings.TrimSpace(text)

	g := cg.game
	switch g.Phase {
	case imposter.PhaseHints:
		if cur, _ := g.Current(); cur.ID != msg.From.ID {
			if g.Hinted(msg.From.ID) {
				c.reply(ctx, msg, "Вы уже дали подсказку в этом раунде. Ждите, пока походят остальные")
			} else {
				c.reply(ctx, msg, "Сейчас не ваш ход")
			}
			return
		}
		if text == "" {
			c.reply(ctx, msg, "Использование: /word <слово>")
			return
		}
		leaked, err := g.Hint(msg.From.ID, text)
		switch {
		case errors.Is(err, imposter.ErrNotOneWord):
			c.reply(ctx, msg, "Нужна ровно одна подсказка из одного слова")
		case err != nil:
			c.logger.ErrorContext(ctx, "Подсказка отклонена", "err", err)
		case leaked:
			delete(c.games, msg.Chat.ID)
			c.say(ctx, msg.Chat.ID, hintsText(g)+"\n"+mention(playerFrom(msg.From))+" назвал(а) загаданное слово! Игра окончена, Импостер побеждает 😈"+revealText(g))
		case g.Phase == imposter.PhaseDiscussion:
			c.say(ctx, msg.Chat.ID, hintsText(g)+"\nПодсказки закончились. Обсуждайте, кто Импостер, и жмите кнопку, когда будете готовы.",
				button("Перейти к голосованию", "imp:talk"))
		default:
			c.announceTurn(ctx, msg.Chat.ID, g)
		}
	case imposter.PhaseGuess:
		if g.Suspect.ID != msg.From.ID {
			c.reply(ctx, msg, "Сейчас не ваш ход")
			return
		}
		if text == "" {
			c.reply(ctx, msg, "Использование: /word <слово>")
			return
		}
		winner, err := g.Guess(msg.From.ID, text)
		if err != nil {
			c.reply(ctx, msg, "Сейчас не ваш ход")
			return
		}
		delete(c.games, msg.Chat.ID)
		if winner == imposter.SideImpostor {
			c.say(ctx, msg.Chat.ID, "Угадал! Импостер побеждает 🎉"+revealText(g))
		} else {
			c.say(ctx, msg.Chat.ID, "Не угадал. Побеждают остальные игроки 🎉"+revealText(g))
		}
	default:
		c.reply(ctx, msg, "Сейчас слова не принимаются")
	}
}

func (c *imposterController) handleCallback(ctx context.Context, q *models.CallbackQuery) {
	if q.Message.Message == nil || !strings.HasPrefix(q.Data, "imp:") {
		return
	}
	chatID, msgID := q.Message.Message.Chat.ID, q.Message.Message.ID
	parts := strings.Split(q.Data, ":")

	answered := false
	answer := func(text string, alert bool) {
		answered = true
		if err := c.tg.AnswerCallback(ctx, q.ID, text, alert); err != nil {
			c.logger.ErrorContext(ctx, "Не удалось ответить на нажатие", "err", err)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() {
		if !answered {
			answer("", false)
		}
	}()

	if parts[1] == "set" && len(parts) == 3 {
		if parts[2] == "secret" {
			if q.Message.Message.Chat.Type == models.ChatTypePrivate {
				c.secret[q.From.ID] = !c.secret[q.From.ID]
				c.editSecretSettings(ctx, chatID, msgID, q.From.ID)
			}
			return
		}
		c.changeSetting(ctx, chatID, msgID, parts[2])
		return
	}

	cg, ok := c.games[chatID]
	if !ok {
		answer("Игра уже закончилась", true)
		return
	}
	g := cg.game
	user := playerFrom(&q.From)

	switch parts[1] {
	case "join":
		switch added, err := g.Join(user); {
		case err != nil:
			answer(err.Error(), true)
		case !added:
			answer("Вы уже в игре", false)
		default:
			c.editLobby(ctx, chatID, cg)
			answer("Вы в игре", false)
		}
	case "leave":
		switch {
		case q.From.ID == g.Host.ID:
			answer("Ведущий не может выйти. Остановить: /imposter_stop", true)
		case g.Leave(user.ID):
			c.editLobby(ctx, chatID, cg)
			answer("Вы вышли", false)
		default:
			answer("Вы не в списке", false)
		}
	case "start":
		if q.From.ID != g.Host.ID {
			answer("Начать может только ведущий: "+g.Host.Name, true)
			return
		}
		if text := c.startGame(ctx, chatID, cg); text != "" {
			answer(text, true)
		}
	case "talk":
		if !g.HasPlayer(user.ID) {
			answer("Вы не участвуете в игре", true)
			return
		}
		if err := g.StartVoting(); err != nil {
			answer(err.Error(), true)
			return
		}
		c.sendVoting(ctx, chatID, cg)
	case "vote":
		if len(parts) != 3 {
			return
		}
		target, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return
		}
		c.vote(ctx, chatID, cg, user.ID, target, answer)
	}
}

func (c *imposterController) startGame(ctx context.Context, chatID int64, cg *chatGame) string {
	g := cg.game
	if g.Phase != imposter.PhaseLobby {
		return "Игра уже началась"
	}
	if len(g.Players) < imposter.MinPlayers {
		return fmt.Sprintf("Нужно минимум %d игрока", imposter.MinPlayers)
	}
	if g.Settings.Master {
		prompt := "Вы Мастер в игре \"Импостер\" в чате \"" + html.EscapeString(cg.title) +
			"\". Пришлите сюда загаданное слово командой /word &lt;слово&gt;. Команда /word без слова выберет случайное."
		if _, err := c.tg.SendHTML(ctx, g.Master.ID, prompt, nil); err != nil {
			c.logger.InfoContext(ctx, "Не удалось написать мастеру", "user_id", g.Master.ID, "err", err)
			return "Не могу написать Мастеру. Откройте диалог со мной и нажмите /start"
		}
		cg.awaiting = true
		c.say(ctx, chatID, "🎩 Мастер "+mention(g.Master)+" выбирает слово. Я написал ему в личку.")
		return ""
	}
	picked, err := c.pickWords(chatID, g.Settings.List, g.WordsNeeded())
	if err != nil {
		c.logger.ErrorContext(ctx, "Не удалось получить слова", "err", err)
		return "Не удалось получить слова: " + err.Error()
	}
	return c.beginGame(ctx, chatID, cg, picked)
}

func (c *imposterController) masterWord(ctx context.Context, msg *models.Message, text string) {
	var chatID int64
	var cg *chatGame
	for id, candidate := range c.games {
		if candidate.awaiting && candidate.game.Master.ID == msg.From.ID {
			chatID, cg = id, candidate
		}
	}
	if cg == nil {
		c.reply(ctx, msg, "Сейчас от вас не ждут слово")
		return
	}

	var picked []string
	if strings.TrimSpace(text) == "" {
		var err error
		if picked, err = c.pickWords(chatID, cg.game.Settings.List, 1); err != nil {
			c.logger.ErrorContext(ctx, "Не удалось получить слово", "err", err)
			c.reply(ctx, msg, "Не удалось выбрать слово, пришлите его сами: /word <слово>")
			return
		}
	} else {
		word, ok := imposter.CleanWord(text)
		if !ok {
			c.reply(ctx, msg, "Нужно ровно одно слово: /word <слово>")
			return
		}
		picked = []string{word}
	}

	if errText := c.beginGame(ctx, chatID, cg, picked); errText != "" {
		c.reply(ctx, msg, errText)
		return
	}
	if cg.game.Phase == imposter.PhaseHints {
		cg.awaiting = false
		c.reply(ctx, msg, "Слово принято: "+picked[0]+". Игра началась.")
	}
}

func (c *imposterController) beginGame(ctx context.Context, chatID int64, cg *chatGame, picked []string) string {
	g := cg.game
	if err := g.Start(picked); err != nil {
		return err.Error()
	}

	var unreachable []string
	for _, p := range g.Players {
		if _, err := c.tg.SendHTML(ctx, p.ID, cardText(g, p, cg.title), nil); err != nil {
			c.logger.InfoContext(ctx, "Не удалось отправить карточку", "user_id", p.ID, "err", err)
			unreachable = append(unreachable, mention(p))
		}
	}
	if len(unreachable) > 0 {
		g.Abort()
		c.say(ctx, chatID, strings.Join(unreachable, ", ")+"\nНапиши боту в личку /start, чтобы начать играть")
		return ""
	}

	if err := c.tg.EditHTML(ctx, chatID, cg.lobbyMsg, lobbyText(g)+"\n\n▶️ Игра началась! Слова разосланы в личные сообщения.", nil); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось обновить лобби", "err", err)
	}
	c.announceTurn(ctx, chatID, g)
	return ""
}

func (c *imposterController) announceTurn(ctx context.Context, chatID int64, g *imposter.Game) {
	cur, ok := g.Current()
	if !ok {
		return
	}
	round := fmt.Sprintf("Раунд %d/%d", g.Round, g.Rounds)
	if g.Round > g.Settings.Rounds {
		round += " (дополнительный)"
	}
	text := fmt.Sprintf("%s\n\n%s. Ход: %s - отправь подсказку командой /word &lt;слово&gt;.", hintsText(g), round, mention(cur))
	c.say(ctx, chatID, strings.TrimSpace(text))
}

func (c *imposterController) sendVoting(ctx context.Context, chatID int64, cg *chatGame) {
	id, err := c.tg.SendHTML(ctx, chatID, votingText(cg.game), votingKeyboard(cg.game))
	if err != nil {
		c.logger.ErrorContext(ctx, "Не удалось открыть голосование", "err", err)
		return
	}
	cg.voteMsg = id
}

func (c *imposterController) vote(ctx context.Context, chatID int64, cg *chatGame, voter, target int64, answer func(string, bool)) {
	g := cg.game
	all, err := g.Vote(voter, target)
	if err != nil {
		answer(err.Error(), true)
		return
	}
	for _, p := range g.Players {
		if p.ID == target {
			answer("Голос за "+p.Name+" принят", false)
		}
	}
	if !all {
		if err := c.tg.EditHTML(ctx, chatID, cg.voteMsg, votingText(g), votingKeyboard(g)); err != nil {
			c.logger.ErrorContext(ctx, "Не удалось обновить голосование", "err", err)
		}
		return
	}

	if err := c.tg.EditHTML(ctx, chatID, cg.voteMsg, "🗳 Голосование завершено", nil); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось закрыть голосование", "err", err)
	}
	res, err := g.Resolve()
	if err != nil {
		c.logger.ErrorContext(ctx, "Не удалось подвести итоги голосования", "err", err)
		return
	}
	c.announceResolution(ctx, chatID, cg, res)
}

func (c *imposterController) announceResolution(ctx context.Context, chatID int64, cg *chatGame, res imposter.Resolution) {
	g := cg.game
	counts := countsText(res)
	switch res.Kind {
	case imposter.ResExtraRound:
		c.say(ctx, chatID, counts+"\nНичья! Играем ещё один раунд подсказок.")
		c.announceTurn(ctx, chatID, g)
	case imposter.ResCaught:
		c.say(ctx, chatID, fmt.Sprintf("%s\nБольше всего голосов у %s - и это Импостер! 🎯\n%s, последний шанс: назови загаданное слово командой /word &lt;слово&gt;.",
			counts, mention(res.Suspect), mention(res.Suspect)))
	case imposter.ResWrongPlayer:
		delete(c.games, chatID)
		c.say(ctx, chatID, fmt.Sprintf("%s\nБольше всего голосов у %s, но он не Импостер. Импостер побеждает 😈%s",
			counts, mention(res.Suspect), revealText(g)))
	case imposter.ResTie:
		delete(c.games, chatID)
		c.say(ctx, chatID, counts+"\nНичья в голосовании - Импостер побеждает 😈"+revealText(g))
	case imposter.ResSecret:
		delete(c.games, chatID)
		verdict := "Голоса разделились поровну."
		if res.Suspect.ID != 0 {
			verdict = "Больше всего голосов у " + mention(res.Suspect) + "."
		}
		c.say(ctx, chatID, counts+"\n"+verdict+" Победителей нет: все были Импостерами 🤫"+revealText(g))
	}
}

func (c *imposterController) showSettings(ctx context.Context, chatID int64) {
	if _, err := c.tg.SendHTML(ctx, chatID, c.settingsText(chatID), c.settingsKeyboard(chatID)); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось показать настройки", "err", err)
	}
}

// showSecretSettings: секретный режим включается только в личке с ботом, чтобы в группе о нём не было видно.
func (c *imposterController) showSecretSettings(ctx context.Context, chatID, userID int64) {
	if _, err := c.tg.SendHTML(ctx, chatID, secretText(c.secret[userID]), secretKeyboard(c.secret[userID])); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось показать секретные настройки", "err", err)
	}
}

func (c *imposterController) editSecretSettings(ctx context.Context, chatID int64, msgID int, userID int64) {
	if err := c.tg.EditHTML(ctx, chatID, msgID, secretText(c.secret[userID]), secretKeyboard(c.secret[userID])); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось обновить секретные настройки", "err", err)
	}
}

func secretText(on bool) string {
	return "🤫 <b>Секретный режим</b>\n" +
		"Все игроки получают разные слова и не знают, что все они Импостеры. " +
		"Включается на одну следующую игру, в которой вы ведущий (сбрасывается после её запуска). " +
		"Сейчас: " + modeLabel(on)
}

func secretKeyboard(on bool) *models.InlineKeyboardMarkup {
	return button("Секретный режим: "+modeLabel(on), "imp:set:secret")
}

func (c *imposterController) nextList(chatID int64, current string) string {
	lists := []string{imposter.ListDownloaded}
	for _, name := range c.store.TopicNames(chatID) {
		lists = append(lists, imposter.TopicList(name))
	}
	i := slices.Index(lists, current)
	return lists[(i+1)%len(lists)]
}

func (c *imposterController) changeSetting(ctx context.Context, chatID int64, msgID int, key string) {
	s := c.settingsFor(chatID)
	switch key {
	case "rounds":
		s.Rounds = s.Rounds%imposter.MaxRounds + 1
	case "tie":
		if s.Tie == imposter.TieImpostorWins {
			s.Tie = imposter.TieExtraRound
		} else {
			s.Tie = imposter.TieImpostorWins
		}
	case "master":
		s.Master = !s.Master
	case "list":
		s.List = c.nextList(chatID, s.List)
	}
	if err := c.store.SetSettings(chatID, s); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось сохранить настройки", "err", err)
	}
	if err := c.tg.EditHTML(ctx, chatID, msgID, c.settingsText(chatID), c.settingsKeyboard(chatID)); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось обновить настройки", "err", err)
	}
}

func (c *imposterController) settingsText(chatID int64) string {
	return "⚙️ <b>Настройки \"Импостер\"</b>\nДействуют на следующие игры в этом чате.\n\n" + settingsSummary(c.settingsFor(chatID))
}

func (c *imposterController) settingsKeyboard(chatID int64) *models.InlineKeyboardMarkup {
	s := c.settingsFor(chatID)
	rows := [][]models.InlineKeyboardButton{
		{{Text: fmt.Sprintf("Раундов подсказок: %d", s.Rounds), CallbackData: "imp:set:rounds"}},
		{{Text: "Ничья: " + tieLabel(s.Tie), CallbackData: "imp:set:tie"}},
		{{Text: "Мастер: " + masterLabel(s.Master), CallbackData: "imp:set:master"}},
		{{Text: "Слова: " + listLabel(s.List), CallbackData: "imp:set:list"}},
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (c *imposterController) editLobby(ctx context.Context, chatID int64, cg *chatGame) {
	if err := c.tg.EditHTML(ctx, chatID, cg.lobbyMsg, lobbyText(cg.game), lobbyKeyboard()); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось обновить лобби", "err", err)
	}
}

func (c *imposterController) say(ctx context.Context, chatID int64, text string, markup ...*models.InlineKeyboardMarkup) {
	var kb *models.InlineKeyboardMarkup
	if len(markup) > 0 {
		kb = markup[0]
	}
	if _, err := c.tg.SendHTML(ctx, chatID, text, kb); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось отправить сообщение игры", "err", err)
	}
}

func (c *imposterController) reply(ctx context.Context, msg *models.Message, text string) {
	if _, err := c.tg.ReplyToChat(ctx, msg.Chat.ID, msg.ID, text); err != nil {
		c.logger.ErrorContext(ctx, "Не удалось отправить ответ игры", "err", err)
	}
}

func playerFrom(u *models.User) imposter.Player {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	if name == "" {
		name = strconv.FormatInt(u.ID, 10)
	}
	return imposter.Player{ID: u.ID, Name: name, Username: u.Username}
}

func mention(p imposter.Player) string {
	return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, p.ID, html.EscapeString(p.Name))
}

func button(text, data string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: text, CallbackData: data}}}}
}

func lobbyKeyboard() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{{Text: "Присоединиться", CallbackData: "imp:join"}, {Text: "Выйти", CallbackData: "imp:leave"}},
		{{Text: "Начать игру", CallbackData: "imp:start"}},
	}}
}

func votingKeyboard(g *imposter.Game) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	for i, p := range g.Players {
		btn := models.InlineKeyboardButton{Text: p.Name, CallbackData: "imp:vote:" + strconv.FormatInt(p.ID, 10)}
		if i%2 == 0 {
			rows = append(rows, []models.InlineKeyboardButton{btn})
		} else {
			rows[len(rows)-1] = append(rows[len(rows)-1], btn)
		}
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func lobbyText(g *imposter.Game) string {
	var b strings.Builder
	b.WriteString("🕵️ <b>Импостер - набор игроков</b>\n")
	b.WriteString(settingsSummary(g.Settings))
	fmt.Fprintf(&b, "\n\nИгроки (%d/%d):\n", len(g.Players), imposter.MaxPlayers)
	for i, p := range g.Players {
		fmt.Fprintf(&b, "%d. %s\n", i+1, html.EscapeString(p.Name))
	}
	if g.Settings.Master {
		fmt.Fprintf(&b, "\nМастер: %s (не играет, выбирает слово).", html.EscapeString(g.Master.Name))
	}
	fmt.Fprintf(&b, "\nМинимум %d игрока. Начать игру может ведущий - %s.", imposter.MinPlayers, html.EscapeString(g.Host.Name))
	return b.String()
}

func votingText(g *imposter.Game) string {
	return fmt.Sprintf("🗳 <b>Голосование: кто Импостер?</b>\nПроголосовали: %d/%d\n\n%s", g.Voted(), len(g.Players), hintsText(g))
}

func hintsText(g *imposter.Game) string {
	if len(g.Hints) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<b>Подсказки:</b>\n")
	for _, h := range g.Hints {
		fmt.Fprintf(&b, "%s - %s\n", html.EscapeString(h.Player.Name), html.EscapeString(h.Text))
	}
	return b.String()
}

func countsText(res imposter.Resolution) string {
	var b strings.Builder
	b.WriteString("<b>Итоги голосования:</b>\n")
	for _, vc := range res.Counts {
		fmt.Fprintf(&b, "%s - %d\n", html.EscapeString(vc.Player.Name), vc.Votes)
	}
	return b.String()
}

func revealText(g *imposter.Game) string {
	if g.Phase == imposter.PhaseLobby {
		return ""
	}
	if g.Settings.Secret {
		var b strings.Builder
		b.WriteString("\n\nСлова игроков:\n")
		for _, p := range g.Players {
			fmt.Fprintf(&b, "%s - %s\n", html.EscapeString(p.Name), html.EscapeString(g.Card(p.ID).Word))
		}
		return b.String()
	}
	return fmt.Sprintf("\n\nСлово: <b>%s</b>\nИмпостер: %s", html.EscapeString(g.Word()), mention(g.Impostor))
}

func cardText(g *imposter.Game, p imposter.Player, chatTitle string) string {
	card := g.Card(p.ID)
	head := "🕵️ Игра \"Импостер\" в чате \"" + html.EscapeString(chatTitle) + "\"\n\n"
	if card.Impostor {
		return head + "Ты - <b>ИМПОСТЕР</b> 😈\nСлова ты не знаешь. Слушай подсказки, подстраивайся и не выдай себя."
	}
	return head + "Секретное слово: <b>" + html.EscapeString(card.Word) + "</b>\nНазывай подсказки, связанные со словом, но само слово произносить нельзя."
}

func settingsSummary(s imposter.Settings) string {
	return fmt.Sprintf("Мастер: %s\nРаундов подсказок: %d\nНичья: %s\nСлова: %s",
		masterLabel(s.Master), s.Rounds, tieLabel(s.Tie), html.EscapeString(listLabel(s.List)))
}

func listLabel(list string) string {
	if name, ok := imposter.TopicName(list); ok {
		return "тема \"" + name + "\""
	}
	return "скачанный список"
}

func masterLabel(master bool) string {
	if master {
		return "есть (сам выбирает слово)"
	}
	return "нет"
}

func modeLabel(secret bool) string {
	if secret {
		return "включён"
	}
	return "выключен"
}

func tieLabel(t imposter.TiePolicy) string {
	if t == imposter.TieExtraRound {
		return "ещё один раунд"
	}
	return "победа Импостера"
}
