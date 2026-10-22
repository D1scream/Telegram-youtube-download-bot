// Package bpmsync — подгонка скорости анимации GIF под BPM музыки.
//
// Порт Python-версии (C:\IT\Fun\gifBpmSync\gifbpmsync.py): BPM по аудио,
// выбор сетки битов, длительность цикла, скорость и прогноз дрейфа фазы
// совпадают с ней побитово (round() = roundHalfEven, sum() = pySum;
// сверка — parity_test.go).
//
// Отличие от Python-версии: GIF не пересжимается — меняются только байты
// задержек в GCE, пиксели остаются идентичными.
package bpmsync
