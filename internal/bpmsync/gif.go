package bpmsync

import "fmt"

// GIF-файл редактируется на уровне блоков: меняются только поля задержки
// в Graphic Control Extension (2 байта, единицы 1/100 с). LZW-данные
// пикселей не распаковываются и не пересобираются — пиксели остаются
// идентичными, меняются лишь байты задержек.

// gifFrame — расположение кадра в файле.
type gifFrame struct {
	start int // начало блоков кадра: его GCE, а без GCE — image descriptor
	desc  int // image descriptor (0x2C)
	delay int // поле задержки GCE; -1, если у кадра нет GCE
}

type gifLayout struct {
	frames  []gifFrame
	trailer int // смещение 0x3B (или конец данных, если его нет)
}

func scanGIF(src []byte) (gifLayout, error) {
	if len(src) < 13 {
		return gifLayout{}, fmt.Errorf("GIF слишком короткий")
	}
	if s := string(src[:6]); s != "GIF89a" && s != "GIF87a" {
		return gifLayout{}, fmt.Errorf("не GIF-файл")
	}
	p := 13 // сигнатура + логический экран
	if src[10]&0x80 != 0 {
		p += 3 << (src[10]&0x07 + 1) // глобальная палитра
	}
	var l gifLayout
	start, delay := -1, -1 // GCE, ждущий своего кадра
	for p < len(src) {
		switch src[p] {
		case 0x3B:
			l.trailer = p
			return l, nil
		case 0x21:
			if p+2 > len(src) {
				return gifLayout{}, fmt.Errorf("GIF обрезан в расширении")
			}
			if src[p+1] == 0xF9 {
				// GCE: 21 F9 [размер>=4] [packed] [delay lo] [delay hi] [transparent] ...
				if p+6 >= len(src) || src[p+2] < 4 {
					return gifLayout{}, fmt.Errorf("некорректный GCE у кадра %d", len(l.frames)+1)
				}
				if start < 0 {
					start = p
				}
				delay = p + 4 // повторный GCE перекрывает предыдущий, как в Pillow
			}
			var err error
			if p, err = skipSubBlocks(src, p+2); err != nil {
				return gifLayout{}, err
			}
		case 0x2C:
			if p+10 > len(src) {
				return gifLayout{}, fmt.Errorf("GIF обрезан в описании кадра %d", len(l.frames)+1)
			}
			if start < 0 {
				start = p
			}
			l.frames = append(l.frames, gifFrame{start: start, desc: p, delay: delay})
			start, delay = -1, -1
			packed := src[p+9]
			p += 10
			if packed&0x80 != 0 {
				p += 3 << (packed&0x07 + 1) // локальная палитра
			}
			if p >= len(src) {
				return gifLayout{}, fmt.Errorf("GIF обрезан в кадре %d", len(l.frames))
			}
			var err error
			if p, err = skipSubBlocks(src, p+1); err != nil { // +1: минимальный размер кода LZW
				return gifLayout{}, err
			}
		default:
			return gifLayout{}, fmt.Errorf("неизвестный блок 0x%02X на смещении %d", src[p], p)
		}
	}
	// без 0x3B браузеры и Pillow файл всё равно показывают
	if len(l.frames) == 0 {
		return gifLayout{}, fmt.Errorf("в GIF нет кадров")
	}
	l.trailer = len(src)
	return l, nil
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
	l, err := scanGIF(src)
	if err != nil {
		return nil, err
	}
	out := make([]int, len(l.frames))
	for i, f := range l.frames {
		out[i] = 100
		if f.delay >= 0 {
			if ms := (int(src[f.delay]) | int(src[f.delay+1])<<8) * quantMs; ms >= minQuanta*quantMs {
				out[i] = ms
			}
		}
	}
	return out, nil
}

// Patch переписывает задержки кадров (мс), пиксели не трогает. Кадрам без
// GCE он добавляется. Если задержек меньше, чем кадров, лишние кадры в конце
// файла отбрасываются (обрезка хвоста-дубликата).
func Patch(src []byte, delays []int) ([]byte, error) {
	l, err := scanGIF(src)
	if err != nil {
		return nil, err
	}
	if len(delays) == 0 || len(delays) > len(l.frames) {
		return nil, fmt.Errorf("задержек %d, а кадров в GIF %d", len(delays), len(l.frames))
	}
	end := l.trailer
	if len(delays) < len(l.frames) {
		end = l.frames[len(delays)].start
	}
	out := make([]byte, 0, end+8*len(delays)+1)
	prev := 0
	for i, ms := range delays {
		f := l.frames[i]
		h := min(int(roundHalfEven(float64(max(ms, minQuanta*quantMs))/quantMs)), 0xFFFF)
		lo, hi := byte(h), byte(h>>8)
		if f.delay >= 0 {
			out = append(out, src[prev:f.delay]...)
			out = append(out, lo, hi)
			prev = f.delay + 2
		} else {
			out = append(out, src[prev:f.desc]...)
			out = append(out, 0x21, 0xF9, 0x04, 0x00, lo, hi, 0x00, 0x00)
			prev = f.desc
		}
	}
	out = append(out, src[prev:end]...)
	out = append(out, 0x3B)
	copy(out, "GIF89a") // GCE появился только в 89a
	return out, nil
}
