package bpmsync

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Коридор скорости: быстрее/медленнее гифка смотрится неестественно.
const (
	SpeedMin = 0.7
	SpeedMax = 1.5
	MinBPM   = 20
	MaxBPM   = 400
)

// GIFResult — подгонка гифки под BPM (сам MP4 делает SyncMP4).
type GIFResult struct {
	Fit
	TotalFrames int // кадров в исходном файле
	TrimmedTail int // отброшено кадров хвоста-повтора
}

// SyncGIF подгоняет цикл GIF под bpm. beats <= 0 — авто-выбор битов.
// baseBPM > 0 — темп, под который нарисована гифка: тогда битов на цикл
// столько, сколько их в исходном цикле при этом темпе (в коридор скорости
// их возвращает только удвоение/деление пополам — ритм рисунка сохраняется).
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
	frames := len(delays)
	if beats <= 0 && baseBPM > 0 {
		cycleMs := 0
		for _, d := range delays[:cycle] {
			cycleMs += d
		}
		n := float64(cycleMs) * baseBPM / 60000
		// рисунок на полбита (цикл 3.5 бита): два цикла подряд — 7 целых битов
		if frames == cycle && math.Abs(n-math.Round(n)) > 0.25 {
			delays = append(delays, delays...)
			cycle, n = 2*cycle, 2*n
		}
		beats = max(1, int(math.Round(n)))
	}
	res.Fit = FitGIF(delays, cycle, bpm, SpeedMin, SpeedMax, beats)
	for i := range res.Order {
		res.Order[i] %= frames // второй цикл — те же кадры исходника
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

// mp4ChunkMs — предел куска, который кодируется один раз: остальная минута —
// его копии без перекодирования (-c copy), это в разы быстрее.
const mp4ChunkMs = 10000

// mp4Scale — не больше 480 px по длинной стороне, стороны чётные (yuv420p).
// В чате гифка крупнее не показывается, а плеер Telegram Desktop отстаёт тем
// сильнее, чем дольше декодируется кадр: по записям экрана tilt 996×912 —
// +1.8%, tenor 498×498 — ~0.1% (время декодирования копится от кадра к кадру).
const mp4Scale = "scale=w='trunc(min(iw,480*iw/max(iw,ih))/2)*2':h='trunc(min(ih,480*ih/max(iw,ih))/2)*2':flags=lanczos"

// SyncMP4 делает из подогнанной гифки MP4 (H.264) — так, как приложение
// Telegram перед отправкой: Telegram показывает его как GIF. Цикл повторяется до mp4TargetMs в пределах mp4MaxBytes.
// В MP4 нет шага 10 мс формата GIF: длительности кадров — до миллисекунды. Кодируется кусок из нескольких
// проходов, длина которого почти точно ложится на целые мс, — его копии не
// копят ошибку. Возвращает MP4 и число проходов.
func SyncMP4(ctx context.Context, src []byte, res GIFResult) ([]byte, int, error) {
	orig, err := Delays(src)
	if err != nil {
		return nil, 0, err
	}
	dir, err := os.MkdirTemp("", "gif2mp4-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	err = composeFrames(src, func(i int, canvas *image.RGBA) error {
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%05d.png", i)))
		if err != nil {
			return err
		}
		defer f.Close()
		return enc.Encode(f, canvas)
	})
	if err != nil {
		return nil, 0, err
	}
	k := res.BeatMs * float64(res.Beats) / res.CycleMs // исходный цикл -> beats битов
	frameMs := make([]float64, len(res.Order))
	passMs := 0.0
	lo, hi := orig[res.Order[0]], orig[res.Order[0]]
	for n, i := range res.Order {
		frameMs[n] = float64(orig[i]) * k
		passMs += frameMs[n]
		lo, hi = min(lo, orig[i]), max(hi, orig[i])
	}
	// задержки 30/40 мс — это ровные 33.3 мс исходника, округлённые форматом
	// GIF до 10 мс: в MP4 ровный шаг возвращается (плавнее, и плеер Telegram
	// с неровными кадрами отстаёт — запись: urara 30/40 мс −2.4%, tenor ровный — в такт)
	if hi-lo <= gifUnitMs {
		for n := range frameMs {
			frameMs[n] = passMs / float64(len(frameMs))
		}
	}
	// проходов в куске: наименьший остаток от целых мс на минуту
	// кусок — столько проходов, сколько влезает в mp4ChunkMs: в куске H.264
	// ссылается на прошлые проходы, и повторы почти бесплатны (а копия куска
	// начинается с ключевого кадра). Округление длины куска до 1 мс — ≤ 3 мс/мин.
	passes := max(1, int(mp4ChunkMs/passMs))
	copies := max(1, int(math.Ceil(mp4TargetMs/(float64(passes)*passMs))))
	chunk, err := encodeFrames(ctx, dir, res.Order, frameMs, passes, 18)
	if err != nil {
		return nil, 0, err
	}
	if total := float64(chunk * copies); total > mp4MaxBytes {
		// размер падает примерно вдвое на каждые +6 CRF
		crf := min(30, 18+int(math.Ceil(6*math.Log2(total*1.1/mp4MaxBytes))))
		if chunk, err = encodeFrames(ctx, dir, res.Order, frameMs, passes, crf); err != nil {
			return nil, 0, err
		}
		copies = max(1, min(copies, mp4MaxBytes/chunk))
	}
	mp4, err := repeatMP4(ctx, dir, copies)
	return mp4, passes * copies, err
}

// encodeFrames кодирует passes проходов кадров order (PNG в dir) с
// длительностями frameMs в dir/chunk.mp4 и возвращает его размер.
// Конец каждого кадра — накопленное точное время, округлённое до 1 мс. Шкала
// 1 мс (framerate 1000 у картинок — иначе concat округляет время до 1/25 с),
// без B-кадров. Длительность последнего кадра concat не передаёт (вышла бы
// 1 мс) — её ставит фильтр пакетов setts.
func encodeFrames(ctx context.Context, dir string, order []int, frameMs []float64, passes, crf int) (int, error) {
	var list strings.Builder
	entry := func(i, ms int) {
		fmt.Fprintf(&list, "file '%05d.png'\noption framerate 1000\nduration %.3f\n", i, float64(ms)/1000)
	}
	var ends []int
	t := 0.0
	for range passes {
		for n := range order {
			t += frameMs[n]
			ends = append(ends, int(math.Round(t)))
		}
	}
	prev := 0
	for n, end := range ends {
		entry(order[n%len(order)], end-prev)
		prev = end
	}
	listPath, out := filepath.Join(dir, "list.txt"), filepath.Join(dir, "chunk.mp4")
	if err := os.WriteFile(listPath, []byte(list.String()), 0o644); err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-bsf:v", fmt.Sprintf("setts=duration=if(eq(N\\,%d)\\,%d\\,DURATION)", len(ends)-1, ends[len(ends)-1]-ends[max(0, len(ends)-2)]),
		"-fps_mode", "passthrough", "-enc_time_base", "1:1000", "-video_track_timescale", "1000",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", strconv.Itoa(crf), "-bf", "0", "-pix_fmt", "yuv420p",
		"-refs", "16", "-g", "100000", "-sc_threshold", "0",
		"-vf", mp4Scale,
		"-an", out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("ffmpeg кадры->mp4: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	st, err := os.Stat(out)
	if err != nil {
		return 0, err
	}
	return int(st.Size()), nil
}

// repeatMP4 склеивает copies копий dir/chunk.mp4 без перекодирования.
func repeatMP4(ctx context.Context, dir string, copies int) ([]byte, error) {
	listPath, out := filepath.Join(dir, "copies.txt"), filepath.Join(dir, "out.mp4")
	if err := os.WriteFile(listPath, []byte(strings.Repeat("file 'chunk.mp4'\n", copies)), 0o644); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", "-movflags", "+faststart", out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg копии mp4: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	return os.ReadFile(out)
}
