package bpmsync

import "fmt"

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

// SyncGIF подгоняет цикл GIF под bpm (beats <= 0 — авто-выбор битов).
func SyncGIF(src []byte, bpm float64, beats int) (GIFResult, error) {
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
	res.Fit = FitGIF(delays, cycle, bpm, SpeedMin, SpeedMax, beats)
	if res.Output, err = Patch(src, res.Delays); err != nil {
		return GIFResult{}, err
	}
	return res, nil
}
