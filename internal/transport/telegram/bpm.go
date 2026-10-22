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
	telegramMaxGetFile = 20 * 1024 * 1024 // предел getFile в Bot API
	bpmUsage           = "Пришлите музыку (в группе — ответом /bpm), или /bpm <BPM> [битов на цикл]"
)

// handleBPM: BPM из аргумента, иначе — из аудио в сообщении или в том, на
// которое ответили. В ответ — гифка, цикл которой подогнан под бит.
func (h *Handler) handleBPM(ctx context.Context, msg *models.Message, args string) {
	bpm, beats, err := parseBPMArgs(args)
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
		log.InfoContext(ctx, "BPM: задан вручную", "bpm", bpm, "beats", beats)
	}
	h.startBPM(ctx, log, msg, audio, bpm, beats, "")
}

// handleAGIF: гифка под BPM предыдущей песни чата — следующая из очереди,
// а «/agif ellen» — выбранная по имени.
func (h *Handler) handleAGIF(ctx context.Context, msg *models.Message, name string) {
	log := h.logger.With("op", "bpm", "chat_id", msg.Chat.ID, "message_id", msg.ID)
	bpm := h.gifs.lastBPM(msg.Chat.ID)
	if bpm == 0 {
		log.InfoContext(ctx, "BPM: /agif без предыдущей песни")
		h.reply(ctx, msg, "Сначала пришлите музыку или /bpm <BPM>")
		return
	}
	log.InfoContext(ctx, "BPM: /agif — BPM предыдущей песни", "bpm", bpm, "name", name)
	h.startBPM(ctx, log, msg, nil, bpm, 0, strings.TrimSpace(name))
}

// handleAGIFList: короткие имена доступных гифок для /agif <имя>.
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

// startBPM подгоняет и отправляет гифку в фоне: скачивание и ffmpeg долгие.
func (h *Handler) startBPM(ctx context.Context, log *slog.Logger, msg *models.Message, audio *audioFile, bpm float64, beats int, gifName string) {
	go func() {
		workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bpmTimeout)
		defer cancel()
		start := time.Now()
		if err := h.sendBPMGIF(workCtx, log, msg, audio, bpm, beats, gifName); err != nil {
			log.ErrorContext(workCtx, "BPM: не удалось", "err", err, "took", since(start))
			h.reply(workCtx, msg, "Не удалось: "+err.Error())
			return
		}
		log.InfoContext(workCtx, "BPM: готово", "took", since(start))
	}()
}

// parseBPMArgs: "[BPM [битов]]", BPM можно с запятой; 0 — определить по аудио.
func parseBPMArgs(args string) (float64, int, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return 0, 0, nil
	}
	if len(fields) > 2 {
		return 0, 0, fmt.Errorf("Лишние аргументы")
	}
	bpm, err := strconv.ParseFloat(strings.Replace(fields[0], ",", ".", 1), 64)
	if err != nil || bpm < bpmsync.MinBPM || bpm > bpmsync.MaxBPM {
		return 0, 0, fmt.Errorf("BPM — число от %d до %d", bpmsync.MinBPM, bpmsync.MaxBPM)
	}
	beats := 0 // авто
	if len(fields) == 2 {
		if beats, err = strconv.Atoi(fields[1]); err != nil || beats < 1 {
			return 0, 0, fmt.Errorf("Число битов — целое >= 1")
		}
	}
	return bpm, beats, nil
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

func (h *Handler) sendBPMGIF(ctx context.Context, log *slog.Logger, msg *models.Message, audio *audioFile, bpm float64, beats int, gifName string) error {
	detected := bpm == 0
	if detected {
		var err error
		if bpm, err = h.detectBPM(ctx, log, audio); err != nil {
			return err
		}
	}
	gifPath, err := h.gifs.next(msg.Chat.ID, bpm, gifName)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(gifPath)
	if err != nil {
		return err
	}
	start := time.Now()
	baseBPM := gifBaseBPM(gifPath)
	res, err := bpmsync.SyncGIF(src, bpm, beats, baseBPM)
	if err != nil {
		return fmt.Errorf("подогнать гифку %s: %w", filepath.Base(gifPath), err)
	}
	log.InfoContext(ctx, "BPM: гифка подогнана", "gif", filepath.Base(gifPath), "base_bpm", baseBPM, "bpm", bpm, "detected", detected,
		"beats_requested", beats, "beats", res.Beats, "beats_adjusted", res.Adjusted, "copies", res.Copies, "loop_ms", res.LoopMs,
		"speed", res.Speed, "drift_ms_per_min", res.DriftPerMin, "took", since(start))

	stem := fmt.Sprintf("%s-%03dbpm", strings.TrimSuffix(filepath.Base(gifPath), filepath.Ext(gifPath)), int(math.Round(bpm)))
	data, name := res.Output, stem+".gif"
	start = time.Now()
	if mp4, loops, err := bpmsync.SyncMP4(ctx, src, res); err != nil {
		log.WarnContext(ctx, "BPM: не удалось сделать MP4, отправляю GIF — Telegram пережмёт его хуже", "err", err)
	} else {
		data, name = mp4, stem+".mp4"
		log.InfoContext(ctx, "BPM: MP4 готов", "gif_bytes", len(res.Output), "mp4_bytes", len(mp4),
			"loops", loops, "mp4_ms", res.LoopMs*float64(res.Copies*loops), "took", since(start))
	}

	start = time.Now()
	log.InfoContext(ctx, "BPM: отправляю анимацию", "file", name, "bytes", len(data))
	if _, err := h.tg.ReplyAnimation(ctx, msg.Chat.ID, msg.ID, name, bytes.NewReader(data)); err != nil {
		return err
	}
	log.InfoContext(ctx, "BPM: гифка отправлена", "took", since(start))
	return nil
}

func (h *Handler) detectBPM(ctx context.Context, log *slog.Logger, audio *audioFile) (float64, error) {
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

	start = time.Now()
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

// since — длительность для лога: JSON-логгер иначе пишет наносекунды числом.
func since(start time.Time) string {
	return time.Since(start).Round(time.Millisecond).String()
}

// baseBPMName: «skeleton-base-110.gif» — гифка нарисована под 110 BPM.
var baseBPMName = regexp.MustCompile(`(?i)-base-(\d+(?:\.\d+)?)`)

// gifBaseBPM — темп из имени файла гифки, 0 если его там нет.
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
