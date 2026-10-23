package bpmsync

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os/exec"
	"strings"
)

const (
	audioRate   = 11025 // частота, в которую ffmpeg переводит аудио
	detectMinMs = 20000 // короче — BPM по звуку не вычислить
	detectLoBPM = 60
	detectHiBPM = 180
)

// DecodeAudio декодирует любой аудиофайл ffmpeg'ом в моно PCM 16 бит, 11025 Гц.
func DecodeAudio(ctx context.Context, path string) ([]int16, int, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", path, "-vn", "-ac", "1", "-ar", fmt.Sprint(audioRate), "-f", "s16le", "pipe:")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil || len(raw) == 0 {
		return nil, 0, fmt.Errorf("ffmpeg не смог декодировать аудио: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	samples := make([]int16, len(raw)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	return samples, audioRate, nil
}

// DetectBPM — BPM из звука (как detect_bpm_from_file): RMS-огибающая за
// 10 мс -> атаки -> автокорреляция, поиск 60–180 BPM шагом 0.05 -> уточнение
// ±0.75 BPM по средней силе атак на сетке битов. На 20 треках с тегом BPM:
// 17 точно, ещё 2 — с ошибкой в кратность темпа. Результат — с шагом 0.05,
// почти целое округляется до целого (студийная музыка обычно в целом BPM).
func DetectBPM(samples []int16, rate int) (float64, error) {
	frame := max(1, rate/100)
	n := len(samples) / frame
	if float64(n*frame)*1000/float64(rate) < detectMinMs {
		return 0, fmt.Errorf("аудио короче ~20 с — BPM по звуку не вычислить")
	}
	env := make([]float64, n)
	for i := range env {
		var sum int64
		for _, x := range samples[i*frame : (i+1)*frame] {
			sum += int64(x) * int64(x)
		}
		env[i] = math.Sqrt(float64(sum) / float64(frame))
	}
	on := make([]float64, n-1) // атаки: полувыпрямленная разность огибающей
	for i := range on {
		on[i] = max(0, env[i+1]-env[i])
	}
	n = len(on)
	frameMs := float64(frame) * 1000 / float64(rate)

	// автокорреляция атак по целым лагам до 4 периодов самого медленного BPM;
	// фаза не нужна, поэтому дробный и «плавающий» темп не размазывается
	maxLag := int(4*60000/detectLoBPM/frameMs) + 2
	if maxLag >= n {
		return 0, fmt.Errorf("аудио слишком короткое")
	}
	ac := make([]float64, maxLag+1)
	for lag := range ac {
		var s pySum
		for i := 0; i+lag < n; i++ {
			s.add(on[i] * on[i+lag])
		}
		ac[lag] = s.value()
	}
	if ac[0] == 0 {
		return 0, fmt.Errorf("в записи нет атак — BPM не вычислить")
	}
	// грубый поиск шагом 0.05: средняя автокорреляция на кратных периода
	// в общем окне лагов × априорный темп (лог-нормальный вокруг 120 BPM,
	// σ = октава, как в librosa) — иначе темп путается вдвое
	window := float64(maxLag - 1)
	best, bestS := float64(detectLoBPM), -1.0
	for j := 0; j <= (detectHiBPM-detectLoBPM)*20; j++ {
		bpm := detectLoBPM + float64(j)*0.05
		period := 60000 / bpm / frameMs
		var s pySum
		k := 1
		for ; float64(k)*period <= window; k++ {
			x := float64(k) * period
			i := int(x)
			f := x - float64(i)
			s.add(ac[i]*(1-f) + ac[i+1]*f)
		}
		oct := math.Log2(bpm / 120)
		if v := s.value() / float64(k-1) * math.Exp(-0.5*oct*oct); v > bestS {
			best, bestS = bpm, v
		}
	}

	// уточнение ±0.75 BPM шагом 0.05: средняя сила атак на сетке битов с лучшей фазой
	ref, refS := best, -1.0
	for b := best - 0.75; b <= best+0.75; b += 0.05 {
		p := 60000 / b / frameMs
		cnt := int(float64(n-1) / p)
		if cnt <= 10 {
			continue
		}
		for ph := 0; ph < int(p); ph++ {
			var s pySum
			for k := 0; k < cnt; k++ {
				x := float64(ph) + float64(k)*p
				i := int(x)
				if i+1 >= n {
					break
				}
				f := x - float64(i)
				s.add(on[i]*(1-f) + on[i+1]*f)
			}
			if mean := s.value() / float64(cnt); mean > refS {
				ref, refS = b, mean
			}
		}
	}
	// шаг уточнения 0.05; почти целое — целое (студийная музыка обычно в целом BPM)
	if r := math.Round(ref); math.Abs(ref-r) < 0.075 {
		return r, nil
	}
	return math.Round(ref*20) / 20, nil
}

// pySum — сумма с компенсацией Ноймайера, как sum() в Python 3.12+;
// без неё близкие кандидаты BPM расходились бы с Python-версией.
type pySum struct{ s, c float64 }

func (p *pySum) add(x float64) {
	t := p.s + x
	if math.Abs(p.s) >= math.Abs(x) {
		p.c += (p.s - t) + x
	} else {
		p.c += (x - t) + p.s
	}
	p.s = t
}

func (p *pySum) value() float64 {
	if p.c != 0 && !math.IsInf(p.c, 0) && !math.IsNaN(p.c) {
		return p.s + p.c
	}
	return p.s
}
