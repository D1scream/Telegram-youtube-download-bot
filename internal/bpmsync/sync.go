package bpmsync

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Коридор скорости по умолчанию, как в Python-версии.
const (
	SpeedMin = 0.9
	SpeedMax = 2.0
	MinBPM   = 20
	MaxBPM   = 400
)

// GIFResult — пересчитанный GIF и сведения о подгонке.
type GIFResult struct {
	Fit
	Output      []byte
	TotalFrames int // кадров в исходном файле
	TrimmedTail int // отброшено кадров хвоста-повтора
}

// SyncGIF подгоняет цикл GIF под bpm. beats <= 0 — авто-выбор битов.
// baseBPM > 0 — темп, под который нарисована гифка: тогда битов на цикл
// столько, сколько их в исходном цикле при этом темпе, и коридор скорости их
// не перестраивает (как --base-bpm в Python-версии).
func SyncGIF(src []byte, bpm float64, beats int, baseBPM float64) (GIFResult, error) {
	if bpm < MinBPM || bpm > MaxBPM {
		return GIFResult{}, fmt.Errorf("BPM %.1f неправдоподобен (допустимо %d–%d)", bpm, MinBPM, MaxBPM)
	}
	delays, err := Delays(src)
	if err != nil {
		return GIFResult{}, err
	}
	if len(delays) < 2 {
		return GIFResult{}, fmt.Errorf("в GIF один кадр — нечего ускорять")
	}
	res := GIFResult{TotalFrames: len(delays)}
	cycle := len(delays)
	// не декодируется стандартной библиотекой — цикл = весь файл
	if p, tail, dups, err := DetectCycle(src); err == nil {
		cycle = p
		if dups {
			res.TrimmedTail = tail
			delays = delays[:len(delays)-tail]
		}
	}
	speedMin, speedMax := SpeedMin, SpeedMax
	if beats <= 0 && baseBPM > 0 {
		cycleMs := 0
		for _, d := range delays[:cycle] {
			cycleMs += d
		}
		beats = max(1, int(roundHalfEven(float64(cycleMs)*baseBPM/60000)))
		speedMin, speedMax = 0, math.Inf(1) // биты заданы рисунком — коридор не трогает
	}
	res.Fit = FitGIF(delays, cycle, bpm, speedMin, speedMax, beats)
	if res.Output, err = Patch(src, res.Order, res.Delays); err != nil {
		return GIFResult{}, err
	}
	return res, nil
}

// Длина MP4 из повторов цикла: плеер Telegram на каждом повторе файла
// перематывает его в начало и теряет на этом миллисекунды — при цикле 500 мс
// это дрейф в секунды за минуту. Длинный файл перематывается редко.
const (
	mp4TargetMs = 60000           // столько повторов цикла, чтобы набралась минута
	mp4MaxBytes = 5 * 1024 * 1024 // но не тяжелее (гифки в чате качаются автоматически)
)

// SyncMP4 делает из подогнанной гифки MP4 (H.264) — так, как приложение
// Telegram перед отправкой: GIF, загруженный ботом, сервер Telegram пережимает
// сам и заметно хуже. Цикл повторяется до mp4TargetMs в пределах mp4MaxBytes;
// повторы кладутся в GIF кадрами (как копии против дрейфа), а не через
// -stream_loop: тот ошибается в длине GIF и рвёт тайминг на стыках.
// Возвращает MP4 и число повторов.
func SyncMP4(ctx context.Context, src []byte, res GIFResult) ([]byte, int, error) {
	one, err := gifToMP4(ctx, res.Output)
	if err != nil {
		return nil, 0, err
	}
	gifMs := 0
	for _, d := range res.Delays {
		gifMs += d
	}
	// повтор весит не больше одного прохода — оценка размера сверху
	loops := min((mp4TargetMs+gifMs-1)/max(gifMs, 1), mp4MaxBytes/max(len(one), 1))
	if loops <= 1 {
		return one, 1, nil
	}
	order := make([]int, 0, len(res.Order)*loops)
	delays := make([]int, 0, len(res.Delays)*loops)
	for range loops {
		order = append(order, res.Order...)
		delays = append(delays, res.Delays...)
	}
	// длинный файл — только улучшение: при ошибке хватит одного прохода
	long, err := Patch(src, order, delays)
	if err != nil {
		return one, 1, nil
	}
	if long, err = gifToMP4(ctx, long); err != nil {
		return one, 1, nil
	}
	return long, loops, nil
}

// gifToMP4 перекодирует GIF в MP4 с таймингом кадров до миллисекунды: метки
// времени не трогаются (passthrough), шкала 1 мс, без B-кадров — иначе
// последний кадр теряет длительность и цикл укорачивается.
func gifToMP4(ctx context.Context, gif []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "gif2mp4-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in.gif"), filepath.Join(dir, "out.mp4")
	if err := os.WriteFile(in, gif, 0o644); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", in,
		"-fps_mode", "passthrough", "-enc_time_base", "1:1000", "-video_track_timescale", "1000",
		"-c:v", "libx264", "-crf", "18", "-bf", "0", "-pix_fmt", "yuv420p",
		// повторы цикла кодируются ссылками на те же кадры раньше — почти бесплатно
		"-refs", "16", "-g", "100000", "-sc_threshold", "0",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", // yuv420p требует чётных сторон
		"-an", "-movflags", "+faststart", out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg gif->mp4: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	return os.ReadFile(out)
}
