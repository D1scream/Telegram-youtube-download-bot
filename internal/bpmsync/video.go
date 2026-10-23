package bpmsync

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// VideoResult — подогнанный ролик и сведения о подгонке.
type VideoResult struct {
	Output   []byte
	Speed    float64
	LengthMs float64 // длительность ролика после подгонки, мс
}

// SyncVideo подгоняет ролик (MP4, звук отбрасывается) под bpm и отдаёт его
// целиком, один раз — для клипов, которые в GIF не влезают. С baseBPM
// скорость — bpm/baseBPM (удвоение/деление пополам — в коридор скорости),
// без него ролик считается циклом на целое число битов, как гифка. Кадры не
// трогаются: метки времени ffmpeg считает от номера кадра.
func SyncVideo(ctx context.Context, path string, bpm, baseBPM float64) (VideoResult, error) {
	if bpm < MinBPM || bpm > MaxBPM {
		return VideoResult{}, fmt.Errorf("BPM %.1f неправдоподобен (допустимо %d–%d)", bpm, MinBPM, MaxBPM)
	}
	num, den, frames, err := probeVideo(ctx, path)
	if err != nil {
		return VideoResult{}, err
	}
	cycleMs := float64(frames) * 1000 * float64(den) / float64(num)
	var res VideoResult
	if baseBPM > 0 {
		res.Speed = bpm / baseBPM
		for res.Speed > SpeedMax {
			res.Speed /= 2
		}
		for res.Speed < SpeedMin {
			res.Speed *= 2
		}
	} else {
		beatMs := 60000 / bpm
		res.Speed = cycleMs / (float64(ChooseBeats(cycleMs, beatMs, SpeedMin, SpeedMax)) * beatMs)
	}
	res.LengthMs = cycleMs / res.Speed
	res.Output, err = retimeVideo(ctx, path, num, den, res.Speed)
	return res, err
}

// probeVideo — частота кадров (num/den) и число кадров ролика.
func probeVideo(ctx context.Context, path string) (num, den, frames int, err error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-count_frames",
		"-show_entries", "stream=r_frame_rate,nb_read_frames", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	rate, count, _ := strings.Cut(strings.TrimSpace(string(out)), ",")
	n, d, _ := strings.Cut(rate, "/")
	num, err1 := strconv.Atoi(n)
	den, err2 := strconv.Atoi(d)
	frames, err3 := strconv.Atoi(strings.TrimSpace(count))
	if err1 != nil || err2 != nil || err3 != nil || num <= 0 || den <= 0 || frames < 2 {
		return 0, 0, 0, fmt.Errorf("ffprobe %s: непонятный ответ %q", filepath.Base(path), out)
	}
	return num, den, frames, nil
}

// retimeVideo перекодирует ролик со скоростью speed: метка кадра N —
// N/(fps·speed), шкала 1 мс, без B-кадров (как framesToMP4).
func retimeVideo(ctx context.Context, path string, num, den int, speed float64) ([]byte, error) {
	dir, err := os.MkdirTemp("", "video-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "out.mp4")
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-i", path, "-an",
		"-vf", fmt.Sprintf("setpts=N*%d/(%d*%.9f)/TB,scale=trunc(iw/2)*2:trunc(ih/2)*2", den, num, speed),
		"-fps_mode", "passthrough", "-enc_time_base", "1:1000", "-video_track_timescale", "1000",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-bf", "0", "-pix_fmt", "yuv420p",
		"-movflags", "+faststart", out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg retime: %w: %s", err, strings.TrimSpace(string(msg)))
	}
	return os.ReadFile(out)
}
