package bpmsync

import "fmt"

// Задержка кадра GIF записана в Graphic Control Extension в сотых долях
// секунды; паузы 0 и 10 мс браузеры крутят как 100 мс.
const (
	gifUnitMs   = 10
	gifMinDelay = 2 // в единицах gifUnitMs
)

// gifDelayOffsets — смещения поля задержки GCE для каждого кадра (-1, если у
// кадра нет GCE). Данные пикселей не распаковываются: блоки только
// пропускаются.
func gifDelayOffsets(src []byte) ([]int, error) {
	if len(src) < 13 {
		return nil, fmt.Errorf("GIF слишком короткий")
	}
	if s := string(src[:6]); s != "GIF89a" && s != "GIF87a" {
		return nil, fmt.Errorf("не GIF-файл")
	}
	p := 13 // сигнатура + логический экран
	if src[10]&0x80 != 0 {
		p += 3 << (src[10]&0x07 + 1) // глобальная палитра
	}
	var frames []int
	delay := -1 // GCE, ждущий своего кадра
	for p < len(src) {
		switch src[p] {
		case 0x3B:
			return frames, nil
		case 0x21:
			if p+2 > len(src) {
				return nil, fmt.Errorf("GIF обрезан в расширении")
			}
			if src[p+1] == 0xF9 {
				// GCE: 21 F9 [размер>=4] [packed] [delay lo] [delay hi] [transparent] ...
				if p+6 >= len(src) || src[p+2] < 4 {
					return nil, fmt.Errorf("некорректный GCE у кадра %d", len(frames)+1)
				}
				delay = p + 4 // повторный GCE перекрывает предыдущий, как в Pillow
			}
			var err error
			if p, err = skipSubBlocks(src, p+2); err != nil {
				return nil, err
			}
		case 0x2C:
			if p+10 > len(src) {
				return nil, fmt.Errorf("GIF обрезан в описании кадра %d", len(frames)+1)
			}
			frames = append(frames, delay)
			delay = -1
			packed := src[p+9]
			p += 10
			if packed&0x80 != 0 {
				p += 3 << (packed&0x07 + 1) // локальная палитра
			}
			if p >= len(src) {
				return nil, fmt.Errorf("GIF обрезан в кадре %d", len(frames))
			}
			var err error
			if p, err = skipSubBlocks(src, p+1); err != nil { // +1: минимальный размер кода LZW
				return nil, err
			}
		default:
			return nil, fmt.Errorf("неизвестный блок 0x%02X на смещении %d", src[p], p)
		}
	}
	// без 0x3B браузеры и Pillow файл всё равно показывают
	if len(frames) == 0 {
		return nil, fmt.Errorf("в GIF нет кадров")
	}
	return frames, nil
}

func skipSubBlocks(src []byte, p int) (int, error) {
	for {
		if p >= len(src) {
			return p, fmt.Errorf("GIF обрезан: нет конца подблоков")
		}
		n := int(src[p])
		p++
		if n == 0 {
			return p, nil
		}
		p += n
	}
}

// Delays возвращает задержки кадров (мс) так, как их видит браузер:
// кадр без GCE и задержки 0/10 мс крутятся как 100 мс.
func Delays(src []byte) ([]int, error) {
	offs, err := gifDelayOffsets(src)
	if err != nil {
		return nil, err
	}
	out := make([]int, len(offs))
	for i, o := range offs {
		out[i] = 100
		if o >= 0 {
			if d := int(src[o]) | int(src[o+1])<<8; d >= gifMinDelay {
				out[i] = d * gifUnitMs
			}
		}
	}
	return out, nil
}
