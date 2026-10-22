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

// Пределы размножения цикла против дрейфа (unrollCopies).
const (
	maxFrames = 120 // кадров в файле
	driftOK   = 10  // мс/мин: за 3 минуты трека — 30 мс, глазом не видно
)

// unrollCopies — сколько копий цикла записать, чтобы шаг 10 мс не копил
// дрейф. Бит редко кратен 10 мс: при 160 BPM цикл 375 мс ложится как 370 мс,
// и за минуту гифка уходит на 0.8 с. Копии с разным округлением (370 + 380)
// в сумме дают ровно 2 цикла. Берётся наименьшее число копий с дрейфом
// < driftOK, иначе — лучшее в пределах maxFrames кадров.
// Возвращает число копий, квантов на все копии и дрейф (мс/мин).
func unrollCopies(cycle int, exactMs float64) (copies, total int, drift float64) {
	for c := 1; c <= max(1, maxFrames/cycle); c++ {
		t := max(minQuanta*cycle*c, int(roundHalfEven(float64(c)*exactMs/quantMs)))
		d := (float64(t*quantMs) - float64(c)*exactMs) * 60000 / float64(t*quantMs)
		if c == 1 || math.Abs(d) < math.Abs(drift) {
			copies, total, drift = c, t, d
		}
		if math.Abs(d) < driftOK {
			break
		}
	}
	return copies, total, drift
}

// RecomputeDelays переписывает задержки так, чтобы цикл длился beats битов.
// Без хвоста цикл записывается копиями (unrollCopies) — дрейф почти ноль;
// с хвостом-неповтором кадры идут как в исходнике, паттерн цикла общий.
// Возвращает задержки (мс) и индексы исходных кадров для каждого кадра
// результата, среднюю длительность цикла и дрейф фазы (мс/мин).
func RecomputeDelays(delays []int, cycle int, beatMs float64, beats int) (out, order []int, loopMs, driftPerMin float64) {
	cycleMs := 0
	for _, d := range delays[:cycle] {
		cycleMs += d
	}
	exact := beatMs * float64(beats)
	scale := func(src []int, k float64, times int) []float64 {
		res := make([]float64, 0, len(src)*times)
		for range times {
			for _, d := range src {
				res = append(res, float64(d)*k)
			}
		}
		return res
	}
	var quanta []int
	copies, total := 1, 0
	if len(delays)%cycle != 0 {
		total = max(minQuanta*cycle, int(roundHalfEven(exact/quantMs)))
		driftPerMin = (float64(total*quantMs) - exact) * 60000 / float64(total*quantMs)
		k := float64(total*quantMs) / float64(cycleMs)
		reps := len(delays) / cycle
		quanta = Quantize(scale(delays[:cycle], k, 1))
		for range reps - 1 {
			quanta = append(quanta, quanta[:cycle]...)
		}
		quanta = append(quanta, Quantize(scale(delays[reps*cycle:], k, 1))...)
		for i := range delays {
			order = append(order, i)
		}
	} else {
		copies, total, driftPerMin = unrollCopies(cycle, exact)
		k := float64(total*quantMs) / float64(copies*cycleMs)
		quanta = Quantize(scale(delays[:cycle], k, copies))
		for range copies {
			for i := range cycle {
				order = append(order, i)
			}
		}
	}
	out = make([]int, len(quanta))
	for i, q := range quanta {
		out[i] = q * quantMs
	}
	return out, order, float64(total*quantMs) / float64(copies), driftPerMin
}

// Fit — полный расчёт синхронизации цикла GIF под BPM.
type Fit struct {
	BeatMs      float64 // длительность бита, мс
	CycleMs     float64 // исходный цикл, мс
	Frames      int     // кадров в цикле
	Beats       int     // битов на цикл после подгонки
	Adjusted    bool    // заданные биты перестроены, чтобы попасть в коридор
	Copies      int     // копий цикла в файле (против дрейфа)
	LoopMs      float64 // средняя длительность цикла, мс
	Speed       float64 // ×0.526 — множитель скорости
	DriftPerMin float64 // накопление ошибки фазы, мс в минуту
	Delays      []int   // задержки кадров результата, мс
	Order       []int   // индекс исходного кадра для каждого кадра результата
}

// FitGIF подбирает сетку битов (beats <= 0 — авто) и пересчитывает задержки.
// speedMin/speedMax — коридор скорости (в Python-версии 0.9 и 2.0).
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
	out, order, loopMs, drift := RecomputeDelays(delays, cycle, beatMs, beats)
	return Fit{
		BeatMs:      beatMs,
		CycleMs:     cycleMs,
		Frames:      cycle,
		Beats:       beats,
		Adjusted:    adjusted,
		Copies:      len(order) / cycle,
		LoopMs:      loopMs,
		Speed:       cycleMs / loopMs,
		DriftPerMin: drift,
		Delays:      out,
		Order:       order,
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
