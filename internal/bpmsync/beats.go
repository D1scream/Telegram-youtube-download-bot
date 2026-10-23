package bpmsync

import "math"

// maxBeats — предел битов на цикл.
const maxBeats = 64

// chooseBeats — число битов на цикл со скоростью ближе всего к ×1.
func chooseBeats(cycleMs, beatMs float64) int {
	natural := cycleMs / beatMs
	lo := max(1, int(math.Ceil(natural/SpeedMax-1e-9)))
	hi := max(lo, min(maxBeats, int(math.Floor(natural/SpeedMin+1e-9))))
	best := lo
	for b := lo + 1; b <= hi; b++ {
		if math.Abs(math.Log(natural/float64(b))) < math.Abs(math.Log(natural/float64(best))) {
			best = b
		}
	}
	return best
}

// fitBeats удваивает или делит число битов, пока скорость не попадёт в
// коридор. Делится только чётное, чтобы движения оставались на битах.
func fitBeats(natural float64, beats int) int {
	for natural/float64(beats) > SpeedMax {
		beats *= 2
	}
	for beats%2 == 0 && natural/float64(beats) < SpeedMin {
		beats /= 2
	}
	return beats
}

// Fit — подгонка цикла гифки под BPM.
type Fit struct {
	BeatMs   float64 // длительность бита, мс
	CycleMs  float64 // исходный цикл, мс
	Beats    int     // битов на цикл после подгонки
	Adjusted bool    // биты удвоены или поделены
	LoopMs   float64 // цикл после подгонки, мс
	Speed    float64 // множитель скорости
	Order    []int   // кадры одного прохода
}

// fitGIF подбирает число битов на цикл: из метки темпа или ближе к ×1.
func fitGIF(delays []int, cycle int, bpm float64, beats int) Fit {
	beatMs := 60000 / bpm
	cycleMs := 0.0
	for _, d := range delays[:cycle] {
		cycleMs += float64(d)
	}
	adjusted := false
	if beats > 0 {
		fitted := fitBeats(cycleMs/beatMs, beats)
		adjusted = fitted != beats
		beats = fitted
	} else {
		beats = chooseBeats(cycleMs, beatMs)
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
		Beats:    beats,
		Adjusted: adjusted,
		LoopMs:   loopMs,
		Speed:    cycleMs / loopMs,
		Order:    order,
	}
}
