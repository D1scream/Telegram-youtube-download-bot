package music

import "errors"

var (
	ErrNoSession        = errors.New("нет активного результата поиска")
	ErrNoMyMusicSession = errors.New("нет активного списка mymusic")
	ErrIndexOutOfRange  = errors.New("номер трека вне диапазона")
	ErrDownloadNotFound = errors.New("ожидающее скачивание не найдено")
)
