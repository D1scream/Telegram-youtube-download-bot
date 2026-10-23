package bpmsync

import "math"

// maxBeats — предел числа битов на цикл (длиннее ~20 с не бывает).
const maxBeats = 64

// ChooseBeats — целое число битов на цикл, при котором скорость ближе всего
// к ×1, внутри коридора speedMin..speedMax.
func ChooseBeats(cycleMs, beatMs, speedMin, speedMax float64) int {
	if beatMs <= 0 {
		return 1
	}
	natural := cycleMs / beatMs
	lo := max(1, int(math.Ceil(natural/speedMax-1e-9)))
	hi := max(lo, min(maxBeats, int(math.Floor(natural/speedMin+1e-9))))
	best := lo
	for b := lo + 1; b <= hi; b++ {
		if math.Abs(math.Log(natural/float64(b))) < math.Abs(math.Log(natural/float64(best))) {
			best = b
		}
	}
	return best
}

// FitBeatsNumber удваивает/делит число битов, пока скорость natural/beats
// не попадёт в коридор. Делится только чётное: движения рисунка остаются на
// битах (через бит или по два на бит), нечётное пополам их бы сдвинуло.
func FitBeatsNumber(natural float64, beats int, speedMin, speedMax float64) int {
	for natural/float64(beats) > speedMax {
		beats *= 2
	}
	for beats%2 == 0 && natural/float64(beats) < speedMin {
		beats /= 2
	}
	return beats
}

// Fit — подгонка цикла гифки под BPM.
type Fit struct {
	BeatMs   float64 // длительность бита, мс
	CycleMs  float64 // исходный цикл, мс
	Frames   int     // кадров в цикле
	Beats    int     // битов на цикл после подгонки
	Adjusted bool    // заданные биты перестроены, чтобы попасть в коридор
	LoopMs   float64 // цикл после подгонки (Beats битов), мс
	Speed    float64 // множитель скорости
	Order    []int   // кадры исходника одного прохода
}

// FitGIF подбирает число битов на цикл (beats <= 0 — авто) в коридоре
// speedMin..speedMax. Проход — цикл, а при хвосте-неповторе — весь файл.
func FitGIF(delays []int, cycle int, bpm float64, speedMin, speedMax float64, beats int) Fit {
	beatMs := 60000 / bpm
	cycleMs := 0.0
	for _, d := range delays[:cycle] {
		cycleMs += float64(d)
	}
	adjusted := false
	if beats > 0 {
		fitted := FitBeatsNumber(cycleMs/beatMs, beats, speedMin, speedMax)
		adjusted = fitted != beats
		beats = fitted
	} else {
		beats = ChooseBeats(cycleMs, beatMs, speedMin, speedMax)
	}
	n := cycle
	if len(delays)%cycle != 0 {
		n = len(delays)
	}
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	loopMs := float64(beats) * beatMs
	return Fit{
		BeatMs:   beatMs,
		CycleMs:  cycleMs,
		Frames:   cycle,
		Beats:    beats,
		Adjusted: adjusted,
		LoopMs:   loopMs,
		Speed:    cycleMs / loopMs,
		Order:    order,
	}
}
