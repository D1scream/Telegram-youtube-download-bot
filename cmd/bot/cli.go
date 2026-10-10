package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"telegram-bot/internal/config"
	"telegram-bot/internal/msglog"
	"telegram-bot/internal/transport/telegram"
)

const cliUsage = `Использование:
  bot messages [-chat ID] [-from USER] [-grep TEXT] [-n 20] [-deleted]
      показать сообщения из лога
  bot delete -chat ID -last N [-from USER] [-grep TEXT] [-yes]
  bot delete -chat ID -ids 101,102,110-115 [-yes]
      удалить сообщения из чата; без -yes спрашивает подтверждение`

func runCLI(args []string) int {
	var err error
	switch args[0] {
	case "messages":
		err = cliMessages(args[1:])
	case "delete":
		err = cliDelete(args[1:])
	default:
		fmt.Fprintln(os.Stderr, cliUsage)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		return 1
	}
	return 0
}

func cliMessages(args []string) error {
	fs := flag.NewFlagSet("messages", flag.ContinueOnError)
	chatID := fs.Int64("chat", 0, "id чата")
	from := fs.String("from", "", "автор: id, @username или имя")
	grep := fs.String("grep", "", "подстрока в тексте")
	n := fs.Int("n", 20, "сколько последних сообщений показать, 0 - все")
	withDeleted := fs.Bool("deleted", false, "показывать и удалённые")
	if err := fs.Parse(args); err != nil {
		return err
	}
	entries, err := msglog.Read(messageLogDir, msglog.Filter{
		ChatID: *chatID, From: *from, Contains: *grep, Last: *n, IncludeDeleted: *withDeleted,
	})
	if err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Println(formatEntry(e))
	}
	if len(entries) == 0 {
		fmt.Println("Сообщений не найдено")
	}
	return nil
}

func cliDelete(args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	chatID := fs.Int64("chat", 0, "id чата (обязательно)")
	last := fs.Int("last", 0, "удалить N последних сообщений из лога")
	idsText := fs.String("ids", "", "id сообщений через запятую, можно диапазоны 10-15")
	from := fs.String("from", "", "только этого автора (с -last)")
	grep := fs.String("grep", "", "только с этой подстрокой (с -last)")
	yes := fs.Bool("yes", false, "не спрашивать подтверждение")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *chatID == 0 {
		return errors.New("нужен -chat")
	}
	if (*last > 0) == (*idsText != "") {
		return errors.New("нужен ровно один из -last или -ids")
	}
	if *idsText != "" && (*from != "" || *grep != "") {
		return errors.New("-from и -grep работают только с -last")
	}
	ids, err := parseIDs(*idsText)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	entries, err := msglog.Read(messageLogDir, msglog.Filter{
		ChatID: *chatID, From: *from, Contains: *grep, Last: *last, IDs: ids,
	})
	if err != nil {
		return err
	}
	if *last > 0 {
		for _, e := range entries {
			ids = append(ids, e.MessageID)
		}
	}
	if len(ids) == 0 {
		fmt.Println("Нечего удалять")
		return nil
	}

	known := make(map[int]msglog.Entry, len(entries))
	for _, e := range entries {
		known[e.MessageID] = e
	}
	for _, id := range ids {
		if e, ok := known[id]; ok {
			fmt.Println(formatEntry(e))
		} else {
			fmt.Printf("id=%d  (нет в логе)\n", id)
		}
	}
	if !*yes && !confirm(fmt.Sprintf("Удалить %d сообщений из чата %d? [y/N]: ", len(ids), *chatID)) {
		fmt.Println("Отменено")
		return nil
	}

	tg, err := telegram.New(cfg.BotToken)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	deleted, errs := tg.DeleteMessages(ctx, *chatID, ids)
	if err := msglog.MarkDeleted(messageLogDir, *chatID, deleted); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
	}
	for _, err := range errs {
		fmt.Fprintln(os.Stderr, "Не удалено:", err)
	}
	fmt.Printf("Удалено: %d из %d\n", len(deleted), len(ids))
	if len(errs) > 0 {
		return fmt.Errorf("не удалось удалить %d сообщений", len(errs))
	}
	return nil
}

func parseIDs(text string) ([]int, error) {
	var ids []int
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil || a <= 0 {
			return nil, fmt.Errorf("неверный id: %q", part)
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil || b < a || b-a >= 1000 {
				return nil, fmt.Errorf("неверный диапазон: %q", part)
			}
		}
		for id := a; id <= b; id++ {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func formatEntry(e msglog.Entry) string {
	author := e.Name
	if e.From != "" {
		author = "@" + e.From + " (" + e.Name + ")"
	}
	text := strings.ReplaceAll(e.Text, "\n", " ↵ ")
	if e.Kind != "" && e.Kind != "text" {
		text = "[" + e.Kind + "] " + text
	}
	line := fmt.Sprintf("%s  chat=%d (%s)  id=%d  %s: %s",
		e.Time.Local().Format("2006-01-02 15:04:05"), e.ChatID, e.Chat, e.MessageID, author, text)
	if e.Deleted {
		line += "  [удалено]"
	}
	return line
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes", "д", "да":
		return true
	}
	return false
}
