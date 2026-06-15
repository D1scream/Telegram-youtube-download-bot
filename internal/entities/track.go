package entities

type Track struct {
	Filename          string
	Size              int64
	Extension         string
	Length            int
	BitRate           int // kbps
	BitDepth          int // bits
	SampleRate        int // Hz
	Username          string
	QueueLength       int
	UploadSpeed       int // bytes/s у пира
	HasFreeUploadSlot bool
	LocalPath         string // абсолютный путь на диске для кэшированных треков
}
