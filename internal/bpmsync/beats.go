package bpmsync

import "math"

// Квант длительности кадра формата GIF — 10 мс.
const quantMs = 10

// minQuanta: паузы 0 и 10 мс браузеры крутят как ~100 мс, поэтому кадр
// не бывает короче 20 мс.
const minQuanta = 2

// maxBeats — предел числа битов на цикл (длиннее ~20 с не бывает).
const maxBeats = 64

// roundHalfEven повторяет семантику Python round(): 0.5 -> к чётному.
func roundHalfEven(x float64) float64 {
	f := math.Floor(x)
	switch d := x - f; {
	case d < 0.5:
		return f
	case d > 0.5:
		return f + 1
	default:
		if int(f)%2 == 0 {
			return f
		}
		return f + 1
	}
}

// ChooseBeatsLowDrift — целое число битов с минимальным дрейфом внутри
// коридора скорости speedMin..speedMax.
//
// Бит почти всегда не кратен кванту 10 мс, поэтому цикл из целого числа
// битов ложится на сетку формата только приблизительно, а ошибка одного
// цикла накапливается фазой гифки против музыки. Перебор ищет целое b,
// где цикл b×бит ближе всего к сетке.
func ChooseBeatsLowDrift(cycleMs, beatMs, speedMin, speedMax float64) int {
	if beatMs <= 0 {
		return 1
	}
	natural := cycleMs / beatMs
	lo := int(math.Ceil(natural/speedMax - 1e-9))
	if lo < 1 {
		lo = 1
	}
	hi := int(math.Floor(natural/speedMin + 1e-9))
	if hi > maxBeats {
		hi = maxBeats
	}
	if hi < lo {
		hi = lo
	}
	best, bestErr := lo, math.Inf(1)
	for b := lo; b <= hi; b++ {
		q := roundHalfEven(float64(b) * beatMs / quantMs)
		if q < 1 {
			q = 1
		}
		err := math.Abs(q*quantMs-float64(b)*beatMs) / (q * quantMs)
		if err < bestErr-1e-12 {
			best, bestErr = b, err
		}
	}
	return best
}

// FitBeatsNumber удваивает/делит число битов, пока скорость natural/beats
// не попадёт в коридор (аналог fit_beats из Python-версии).
func FitBeatsNumber(natural float64, beats int, speedMin, speedMax float64) int {
	for natural/float64(beats) > speedMax {
		beats *= 2
	}
	for beats > 1 && natural/float64(beats) < speedMin {
		beats /= 2
	}
	return beats
}

// Quantize — дробные длительности в кванты: округляется накопленное время,
// ошибка не копится (Брезенхем). Кадры короче minQuanta добираются за счёт
// самых длинных.
func Quantize(exact []float64) []int {
	acc := make([]int, len(exact))
	run := 0.0
	for i, t := range exact {
		run += t
		acc[i] = int(roundHalfEven(run / quantMs))
	}
	quanta := make([]int, len(acc))
	prev := 0
	for i, a := range acc {
		quanta[i] = a - prev
		prev = a
	}
	deficit := 0
	for i, q := range quanta {
		if q < minQuanta {
			deficit += minQuanta - q
			quanta[i] = minQuanta
		}
	}
	for ; deficit > 0; deficit-- {
		maxIdx := 0
		for i, q := range quanta {
			if q > quanta[maxIdx] {
				maxIdx = i
			}
		}
		if quanta[maxIdx] > minQuanta {
			quanta[maxIdx]--
		}
	}
	return quanta
}

// RecomputeDelays переписывает задержки так, чтобы цикл длился ровно beats
// битов; паттерн цикла повторяется, хвост масштабируется так же.
// Возвращает задержки всех кадров (мс), итоговую длительность цикла
// и подписанный прогноз дрейфа фазы (мс/мин).
func RecomputeDelays(delays []int, cycle int, beatMs float64, beats int) (out []int, loopMs int, driftPerMin float64) {
	target := minQuanta * cycle
	if t := int(roundHalfEven(beatMs*float64(beats)/quantMs)) * quantMs; t > target {
		target = t
	}
	sum := 0
	for _, d := range delays[:cycle] {
		sum += d
	}
	k := float64(target) / float64(sum)
	reps := len(delays) / cycle
	scaled := make([]float64, 0, cycle)
	for _, d := range delays[:cycle] {
		scaled = append(scaled, float64(d)*k)
	}
	pattern := Quantize(scaled)
	out = make([]int, 0, len(delays))
	for r := 0; r < reps; r++ {
		out = append(out, pattern...)
	}
	if rest := len(delays) - reps*cycle; rest > 0 {
		tail := make([]float64, 0, rest)
		for _, d := range delays[reps*cycle:] {
			tail = append(tail, float64(d)*k)
		}
		out = append(out, Quantize(tail)...)
	}
	for i := range out {
		out[i] *= quantMs // кванты -> мс
	}
	drift := float64(target) - beatMs*float64(beats)
	driftPerMin = drift * 60000 / float64(target)
	return out, target, driftPerMin
}

// Fit — полный расчёт синхронизации цикла GIF под BPM.
type Fit struct {
	BeatMs      float64 // длительность бита, мс
	CycleMs     float64 // исходный цикл, мс
	Frames      int     // кадров в цикле
	Beats       int     // битов на цикл после подгонки
	Adjusted    bool    // заданные биты перестроены, чтобы попасть в коридор
	LoopMs      int     // итоговая длительность цикла, мс
	Speed       float64 // ×0.526 — множитель скорости
	DriftPerMin float64 // накопление ошибки фазы, мс в минуту
	Delays      []int   // новые задержки всех кадров, мс
}

// FitGIF подбирает сетку битов (beats <= 0 — авто) и пересчитывает задержки.
// speedMin/speedMax — коридор скорости (в Python-версии 0.5 и 2.0).
func FitGIF(delays []int, cycle int, bpm float64, speedMin, speedMax float64, beats int) Fit {
	beatMs := 60000 / bpm
	cycleMs := 0.0
	for _, d := range delays[:cycle] {
		cycleMs += float64(d)
	}
	natural := cycleMs / beatMs
	adjusted := false
	if beats > 0 {
		fitted := FitBeatsNumber(natural, beats, speedMin, speedMax)
		adjusted = fitted != beats
		beats = fitted
	} else {
		beats = ChooseBeatsLowDrift(cycleMs, beatMs, speedMin, speedMax)
	}
	out, loopMs, drift := RecomputeDelays(delays, cycle, beatMs, beats)
	return Fit{
		BeatMs:      beatMs,
		CycleMs:     cycleMs,
		Frames:      cycle,
		Beats:       beats,
		Adjusted:    adjusted,
		LoopMs:      loopMs,
		Speed:       cycleMs / float64(loopMs),
		DriftPerMin: drift,
		Delays:      out,
	}
}

// TableRow — строка таблицы «подходящих BPM» (аналог --table в CLI).
type TableRow struct {
	BPM         int
	BeatMs      float64
	Beats       int
	DriftPerMin float64
}

// Table для целых BPM диапазона lo..hi возвращает строки, отсортированные
// от точных кратностей (дрейф ~0) к расходящимся.
func Table(cycleMs, speedMin, speedMax float64, lo, hi int) []TableRow {
	rows := make([]TableRow, 0, hi-lo+1)
	for bpm := lo; bpm <= hi; bpm++ {
		beat := 60000 / float64(bpm)
		b := ChooseBeatsLowDrift(cycleMs, beat, speedMin, speedMax)
		q := roundHalfEven(float64(b) * beat / quantMs)
		if q < 1 {
			q = 1
		}
		perMin := (q*quantMs - float64(b)*beat) * 60000 / (q * quantMs)
		rows = append(rows, TableRow{BPM: bpm, BeatMs: beat, Beats: b, DriftPerMin: perMin})
	}
	sortSliceStable(rows)
	return rows
}

func sortSliceStable(rows []TableRow) {
	// простая вставка: диапазон мал, устойчивость важна
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && math.Abs(rows[j].DriftPerMin) < math.Abs(rows[j-1].DriftPerMin); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}
