package bpmsync

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"slices"
)

// DetectCycle ищет минимальный период p, с которым кадры повторяются (>= 2
// раз), длину хвоста после последнего полного цикла и то, повторяет ли хвост
// начало цикла (такой хвост безопасно обрезать). Кадры сравниваются после
// склейки с учётом disposal — как их видит зритель (аналог detect_cycle).
func DetectCycle(src []byte) (period, tail int, tailDups bool, err error) {
	var hs [][md5.Size]byte
	err = composeFrames(src, func(_ int, canvas *image.RGBA) error {
		hs = append(hs, md5.Sum(canvas.Pix))
		return nil
	})
	if err != nil {
		return 0, 0, false, err
	}
	n := len(hs)
	for p := 1; p <= n/2; p++ {
		if repeats(hs, p) {
			t := n % p
			return p, t, t > 0 && slices.Equal(hs[n-t:], hs[:t]), nil
		}
	}
	return n, 0, false, nil
}

// composeFrames склеивает кадры GIF с учётом disposal — как их видит
// зритель — и отдаёт каждый в fn (холст переиспользуется между вызовами).
func composeFrames(src []byte, fn func(i int, canvas *image.RGBA) error) error {
	g, err := gif.DecodeAll(bytes.NewReader(src))
	if err != nil {
		return fmt.Errorf("декодировать GIF: %w", err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	for i, fr := range g.Image {
		var saved []byte
		if g.Disposal[i] == gif.DisposalPrevious {
			saved = slices.Clone(canvas.Pix)
		}
		draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		if err := fn(i, canvas); err != nil {
			return err
		}
		switch g.Disposal[i] {
		case gif.DisposalBackground:
			draw.Draw(canvas, fr.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, saved)
		}
	}
	return nil
}

func repeats(hs [][md5.Size]byte, p int) bool {
	for i := p; i < len(hs)/p*p; i++ {
		if hs[i] != hs[i%p] {
			return false
		}
	}
	return true
}
