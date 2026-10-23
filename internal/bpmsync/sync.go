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

// Допустимые скорость и BPM.
const (
	SpeedMin = 0.7
	SpeedMax = 1.5
	MinBPM   = 20
	MaxBPM   = 400
)

// SyncGIF подгоняет цикл гифки под bpm. При baseBPM > 0 число битов на цикл
// берётся из него и только удваивается или делится пополам.
func SyncGIF(src []byte, bpm, baseBPM float64) (Fit, error) {
	if bpm < MinBPM || bpm > MaxBPM {
		return Fit{}, fmt.Errorf("BPM %.1f неправдоподобен (допустимо %d–%d)", bpm, MinBPM, MaxBPM)
	}
	delays, err := Delays(src)
	if err != nil {
		return Fit{}, err
	}
	if len(delays) < 2 {
		return Fit{}, fmt.Errorf("в GIF один кадр — нечего ускорять")
	}
	cycle := len(delays)
	if p, tail, dups, err := DetectCycle(src); err == nil {
		cycle = p
		if dups {
			delays = delays[:len(delays)-tail]
		}
	}
	frames := len(delays)
	beats := 0
	if baseBPM > 0 {
		cycleMs := 0
		for _, d := range delays[:cycle] {
			cycleMs += d
		}
		n := float64(cycleMs) * baseBPM / 60000
		// цикл на полбита — играем два цикла подряд
		if frames == cycle && math.Abs(n-math.Round(n)) > 0.25 {
			delays = append(delays, delays...)
			cycle, n = 2*cycle, 2*n
		}
		beats = max(1, int(math.Round(n)))
	}
	res := fitGIF(delays, cycle, bpm, beats)
	for i := range res.Order {
		res.Order[i] %= frames
	}
	return res, nil
}

// Цикл повторяется до минуты: на каждой перемотке файла Telegram теряет время.
const (
	mp4TargetMs = 60000
	mp4MaxBytes = 5 * 1024 * 1024
)

// mp4ChunkMs — кусок, который кодируется один раз; остальное — его копии.
const mp4ChunkMs = 10000

// mp4Scale — до 480 px по длинной стороне, стороны чётные.
const mp4Scale = "scale=w='trunc(min(iw,480*iw/max(iw,ih))/2)*2':h='trunc(min(ih,480*ih/max(iw,ih))/2)*2':flags=lanczos"

// SyncMP4 делает из подогнанной гифки MP4 с длительностями кадров до 1 мс.
// Возвращает MP4 и число проходов.
func SyncMP4(ctx context.Context, src []byte, res Fit) ([]byte, int, error) {
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
	k := res.BeatMs * float64(res.Beats) / res.CycleMs
	frameMs := make([]float64, len(res.Order))
	passMs := 0.0
	lo, hi := orig[res.Order[0]], orig[res.Order[0]]
	for n, i := range res.Order {
		frameMs[n] = float64(orig[i]) * k
		passMs += frameMs[n]
		lo, hi = min(lo, orig[i]), max(hi, orig[i])
	}
	// 30/40 мс — округлённые форматом GIF ровные 33.3 мс
	if hi-lo <= gifUnitMs {
		for n := range frameMs {
			frameMs[n] = passMs / float64(len(frameMs))
		}
	}
	passes := max(1, int(mp4ChunkMs/passMs))
	copies := max(1, int(math.Ceil(mp4TargetMs/(float64(passes)*passMs))))
	chunk, err := encodeFrames(ctx, dir, res.Order, frameMs, passes, 18)
	if err != nil {
		return nil, 0, err
	}
	if total := float64(chunk * copies); total > mp4MaxBytes {
		// +6 CRF — примерно вдвое меньше
		crf := min(30, 18+int(math.Ceil(6*math.Log2(total*1.1/mp4MaxBytes))))
		if chunk, err = encodeFrames(ctx, dir, res.Order, frameMs, passes, crf); err != nil {
			return nil, 0, err
		}
		copies = max(1, min(copies, mp4MaxBytes/chunk))
	}
	mp4, err := repeatMP4(ctx, dir, copies)
	return mp4, passes * copies, err
}

// encodeFrames кодирует passes проходов кадров order в dir/chunk.mp4 и
// возвращает его размер. framerate 1000 — иначе concat округляет время до
// 1/25 с; длительность последнего кадра concat теряет, её ставит setts.
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
