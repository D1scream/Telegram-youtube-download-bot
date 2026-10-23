package telegram

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"telegram-bot/internal/bpmsync"
)

const (
	bpmTimeout         = 2 * time.Minute
	telegramMaxGetFile = 20 * 1024 * 1024 // предел getFile
	bpmUsage           = "Пришлите музыку (в группе — ответом /bpm), или /bpm <BPM>"
)

// handleBPM: BPM из аргумента или из аудио, в ответ — гифка в такт.
func (h *Handler) handleBPM(ctx context.Context, msg *models.Message, args string) {
	bpm, err := parseBPMArgs(args)
	if err != nil {
		h.reply(ctx, msg, err.Error()+"\n"+bpmUsage)
		return
	}
	log := h.logger.With("op", "bpm", "chat_id", msg.Chat.ID, "message_id", msg.ID)
	var audio *audioFile
	if bpm == 0 {
		if audio = findAudio(msg); audio == nil && msg.ReplyToMessage != nil {
			audio = findAudio(msg.ReplyToMessage)
		}
		if audio == nil {
			log.InfoContext(ctx, "BPM: нет ни числа, ни аудио — подсказка пользователю")
			h.reply(ctx, msg, bpmUsage)
			return
		}
		log = log.With("file_id", audio.fileID, "size", audio.size)
		if audio.size > telegramMaxGetFile {
			log.WarnContext(ctx, "BPM: файл больше 20 МБ, отказ")
			h.reply(ctx, msg, "Файл больше 20 МБ — бот не может его скачать")
			return
		}
		log.InfoContext(ctx, "BPM: аудио получено")
	} else {
		log.InfoContext(ctx, "BPM: задан вручную", "bpm", bpm)
	}
	h.startBPM(ctx, log, msg, audio, bpm, "")
}

// handleAGIF: гифка под BPM песни из сообщения или предыдущей песни чата.
func (h *Handler) handleAGIF(ctx context.Context, msg *models.Message, name string) {
	log := h.logger.With("op", "bpm", "chat_id", msg.Chat.ID, "message_id", msg.ID)
	audio := findAudio(msg)
	if audio == nil && msg.ReplyToMessage != nil {
		audio = findAudio(msg.ReplyToMessage)
	}
	if audio != nil {
		if audio.size > telegramMaxGetFile {
			h.reply(ctx, msg, "Файл больше 20 МБ — бот не может его скачать")
			return
		}
		log.InfoContext(ctx, "BPM: /agif с песней", "file_id", audio.fileID, "size", audio.size, "name", name)
		h.startBPM(ctx, log, msg, audio, 0, strings.TrimSpace(name))
		return
	}
	bpm := h.gifs.lastBPM(msg.Chat.ID)
	if bpm == 0 {
		log.InfoContext(ctx, "BPM: /agif без предыдущей песни")
		h.reply(ctx, msg, "Сначала пришлите музыку или /bpm <BPM>")
		return
	}
	log.InfoContext(ctx, "BPM: /agif — BPM предыдущей песни", "bpm", bpm, "name", name)
	h.startBPM(ctx, log, msg, nil, bpm, strings.TrimSpace(name))
}

func (h *Handler) handleAGIFList(ctx context.Context, msg *models.Message) {
	names, err := h.gifs.list()
	if err != nil {
		h.reply(ctx, msg, "Не удалось: "+err.Error())
		return
	}
	titles := make([]string, len(names))
	for i, n := range names {
		titles[i] = gifTitle(n)
	}
	h.reply(ctx, msg, "Гифки: "+strings.Join(titles, ", ")+"\nВыбрать: /agif <имя>")
}

// startBPM подгоняет и отправляет гифку в фоне.
func (h *Handler) startBPM(ctx context.Context, log *slog.Logger, msg *models.Message, audio *audioFile, bpm float64, gifName string) {
	go func() {
		workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bpmTimeout)
		defer cancel()
		start := time.Now()
		var err error
		if audio != nil {
			bpm, err = h.downloadBPM(workCtx, log, audio)
		}
		if err == nil {
			err = h.sendBPMGIF(workCtx, log, msg.Chat.ID, msg.ID, bpm, gifName)
		}
		if err != nil {
			log.ErrorContext(workCtx, "BPM: не удалось", "err", err, "took", since(start))
			h.reply(workCtx, msg, "Не удалось: "+err.Error())
			return
		}
		log.InfoContext(workCtx, "BPM: готово", "took", since(start))
	}()
}

// parseBPMArgs: 0 — определить по аудио.
func parseBPMArgs(args string) (float64, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return 0, nil
	}
	bpm, err := strconv.ParseFloat(strings.Replace(args, ",", ".", 1), 64)
	if err != nil || bpm < bpmsync.MinBPM || bpm > bpmsync.MaxBPM {
		return 0, fmt.Errorf("BPM — число от %d до %d", bpmsync.MinBPM, bpmsync.MaxBPM)
	}
	return bpm, nil
}

type audioFile struct {
	fileID string
	size   int64
}

func findAudio(msg *models.Message) *audioFile {
	switch {
	case msg.Audio != nil:
		return &audioFile{msg.Audio.FileID, msg.Audio.FileSize}
	case msg.Voice != nil:
		return &audioFile{msg.Voice.FileID, msg.Voice.FileSize}
	case msg.Document != nil && strings.HasPrefix(msg.Document.MimeType, "audio/"):
		return &audioFile{msg.Document.FileID, msg.Document.FileSize}
	}
	return nil
}

// ytmGIF — гифка в такт к аудио из /ytm.
func (h *Handler) ytmGIF(ctx context.Context, chatID int64, messageID int, path string) {
	ctx, cancel := context.WithTimeout(ctx, bpmTimeout)
	defer cancel()
	log := h.logger.With("op", "bpm", "chat_id", chatID, "message_id", messageID, "source", "ytm")
	start := time.Now()
	bpm, err := detectBPM(ctx, log, path)
	if err == nil {
		err = h.sendBPMGIF(ctx, log, chatID, messageID, bpm, "")
	}
	if err != nil {
		log.ErrorContext(ctx, "BPM: не удалось", "err", err, "took", since(start))
		return
	}
	log.InfoContext(ctx, "BPM: готово", "took", since(start))
}

// sendBPMGIF подгоняет гифку под bpm и отправляет ответом на messageID.
func (h *Handler) sendBPMGIF(ctx context.Context, log *slog.Logger, chatID int64, messageID int, bpm float64, gifName string) error {
	gifPath, err := h.gifs.next(chatID, bpm, gifName)
	if err != nil {
		return err
	}
	file := filepath.Base(gifPath)
	baseBPM := gifBaseBPM(gifPath)
	stem := fmt.Sprintf("%s-%03dbpm", strings.TrimSuffix(file, filepath.Ext(file)), int(math.Round(bpm)))
	var data []byte
	name := stem + ".mp4"
	send := h.tg.ReplyAnimation
	start := time.Now()
	if strings.EqualFold(filepath.Ext(file), ".mp4") {
		send = h.tg.ReplyVideo
		res, err := bpmsync.SyncVideo(ctx, gifPath, bpm, baseBPM)
		if err != nil {
			return fmt.Errorf("подогнать ролик %s: %w", file, err)
		}
		log.InfoContext(ctx, "BPM: ролик подогнан", "gif", file, "base_bpm", baseBPM, "bpm", bpm,
			"speed", res.Speed, "length_ms", res.LengthMs, "mp4_bytes", len(res.Output), "took", since(start))
		data = res.Output
	} else if data, name, err = syncGIF(ctx, log, gifPath, bpm, baseBPM, stem); err != nil {
		return err
	}

	start = time.Now()
	log.InfoContext(ctx, "BPM: отправляю", "file", name, "bytes", len(data))
	if _, err := send(ctx, chatID, messageID, name, bytes.NewReader(data)); err != nil {
		return err
	}
	log.InfoContext(ctx, "BPM: гифка отправлена", "took", since(start))
	return nil
}

func syncGIF(ctx context.Context, log *slog.Logger, gifPath string, bpm, baseBPM float64, stem string) ([]byte, string, error) {
	src, err := os.ReadFile(gifPath)
	if err != nil {
		return nil, "", err
	}
	start := time.Now()
	res, err := bpmsync.SyncGIF(src, bpm, baseBPM)
	if err != nil {
		return nil, "", fmt.Errorf("подогнать гифку %s: %w", filepath.Base(gifPath), err)
	}
	log.InfoContext(ctx, "BPM: гифка подогнана", "gif", filepath.Base(gifPath), "base_bpm", baseBPM, "bpm", bpm,
		"beats", res.Beats, "beats_adjusted", res.Adjusted, "loop_ms", res.LoopMs, "speed", res.Speed, "took", since(start))

	start = time.Now()
	mp4, passes, err := bpmsync.SyncMP4(ctx, src, res)
	if err != nil {
		return nil, "", fmt.Errorf("сделать MP4 из %s: %w", filepath.Base(gifPath), err)
	}
	log.InfoContext(ctx, "BPM: MP4 готов", "mp4_bytes", len(mp4), "passes", passes, "took", since(start))
	return mp4, stem + ".mp4", nil
}

func (h *Handler) downloadBPM(ctx context.Context, log *slog.Logger, audio *audioFile) (float64, error) {
	dir, err := os.MkdirTemp("", "bpmsync-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)

	start := time.Now()
	log.InfoContext(ctx, "BPM: начинаю скачивание")
	path, err := h.tg.DownloadFile(ctx, audio.fileID, dir, "audio")
	if err != nil {
		return 0, err
	}
	log.InfoContext(ctx, "BPM: файл скачан", "path", path, "took", since(start))
	return detectBPM(ctx, log, path)
}

func detectBPM(ctx context.Context, log *slog.Logger, path string) (float64, error) {
	start := time.Now()
	log.InfoContext(ctx, "BPM: декодирую аудио (ffmpeg)")
	samples, rate, err := bpmsync.DecodeAudio(ctx, path)
	if err != nil {
		return 0, err
	}
	log.InfoContext(ctx, "BPM: аудио декодировано", "seconds", len(samples)/rate, "took", since(start))

	start = time.Now()
	log.InfoContext(ctx, "BPM: определяю темп")
	bpm, err := bpmsync.DetectBPM(samples, rate)
	if err != nil {
		return 0, err
	}
	log.InfoContext(ctx, "BPM: темп определён", "bpm", bpm, "took", since(start))
	return bpm, nil
}

// since — длительность для лога (иначе JSON пишет наносекунды).
func since(start time.Time) string {
	return time.Since(start).Round(time.Millisecond).String()
}

// baseBPMName: «skeleton-base-110.gif» — гифка под 110 BPM.
var baseBPMName = regexp.MustCompile(`(?i)-base-(\d+(?:\.\d+)?)`)

func gifBaseBPM(path string) float64 {
	m := baseBPMName.FindStringSubmatch(filepath.Base(path))
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || v < bpmsync.MinBPM || v > bpmsync.MaxBPM {
		return 0
	}
	return v
}
