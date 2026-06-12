package music

import (
	"math"

	"go.senan.xyz/taglib"

	"telegram-bot/internal/entities"
)

func enrichLocalTrack(track entities.Track) entities.Track {
	if track.LocalPath == "" {
		return track
	}

	props, err := taglib.ReadProperties(track.LocalPath)
	if err != nil {
		return track
	}

	if props.Length > 0 {
		track.Length = int(math.Round(props.Length.Seconds()))
	}
	if props.Bitrate > 0 {
		track.BitRate = int(props.Bitrate)
	}
	if props.SampleRate > 0 {
		track.SampleRate = int(props.SampleRate)
	}

	return track
}
