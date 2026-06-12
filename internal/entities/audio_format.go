package entities

type AudioFormat string

const (
	AudioFormatMP3  AudioFormat = ".mp3"
	AudioFormatFLAC AudioFormat = ".flac"
	AudioFormatOGG  AudioFormat = ".ogg"
	AudioFormatWAV  AudioFormat = ".wav"
	AudioFormatM4A  AudioFormat = ".m4a"
	AudioFormatAAC  AudioFormat = ".aac"
)
