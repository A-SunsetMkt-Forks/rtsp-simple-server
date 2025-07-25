package rtmp

import (
	"time"

	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg1audio"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"

	"github.com/bluenviron/mediamtx/internal/protocols/rtmp/amf0"
	"github.com/bluenviron/mediamtx/internal/protocols/rtmp/h264conf"
	"github.com/bluenviron/mediamtx/internal/protocols/rtmp/message"
)

func audioRateRTMPToInt(v uint8) int {
	switch v {
	case message.Rate5512:
		return 5512
	case message.Rate11025:
		return 11025
	case message.Rate22050:
		return 22050
	default:
		return 44100
	}
}

func audioRateIntToRTMP(v int) uint8 {
	switch v {
	case 5512:
		return message.Rate5512
	case 11025:
		return message.Rate11025
	case 22050:
		return message.Rate22050
	default:
		return message.Rate44100
	}
}

func mpeg1AudioChannels(m mpeg1audio.ChannelMode) bool {
	return m != mpeg1audio.ChannelModeMono
}

// Writer provides functions to write outgoing data.
type Writer struct {
	Conn       Conn
	VideoTrack format.Format
	AudioTrack format.Format
}

// Initialize initializes Writer.
func (w *Writer) Initialize() error {
	err := w.writeTracks()
	if err != nil {
		return err
	}

	return nil
}

func (w *Writer) writeTracks() error {
	err := w.Conn.Write(&message.DataAMF0{
		ChunkStreamID:   4,
		MessageStreamID: 0x1000000,
		Payload: []interface{}{
			"@setDataFrame",
			"onMetaData",
			amf0.Object{
				{
					Key:   "videodatarate",
					Value: float64(0),
				},
				{
					Key: "videocodecid",
					Value: func() float64 {
						switch w.VideoTrack.(type) {
						case *format.H264:
							return message.CodecH264

						default:
							return 0
						}
					}(),
				},
				{
					Key:   "audiodatarate",
					Value: float64(0),
				},
				{
					Key: "audiocodecid",
					Value: func() float64 {
						switch w.AudioTrack.(type) {
						case *format.MPEG1Audio:
							return message.CodecMPEG1Audio

						case *format.MPEG4Audio, *format.MPEG4AudioLATM:
							return message.CodecMPEG4Audio

						default:
							return 0
						}
					}(),
				},
			},
		},
	})
	if err != nil {
		return err
	}

	if videoTrack, ok := w.VideoTrack.(*format.H264); ok {
		// write decoder config only if SPS and PPS are available.
		// if they're not available yet, they're sent later.
		if sps, pps := videoTrack.SafeParams(); sps != nil && pps != nil {
			buf, _ := h264conf.Conf{
				SPS: sps,
				PPS: pps,
			}.Marshal()

			err = w.Conn.Write(&message.Video{
				ChunkStreamID:   message.VideoChunkStreamID,
				MessageStreamID: 0x1000000,
				Codec:           message.CodecH264,
				IsKeyFrame:      true,
				Type:            message.VideoTypeConfig,
				Payload:         buf,
			})
			if err != nil {
				return err
			}
		}
	}

	var audioConf *mpeg4audio.AudioSpecificConfig

	if track, ok := w.AudioTrack.(*format.MPEG4Audio); ok {
		audioConf = track.Config
	} else if track, ok := w.AudioTrack.(*format.MPEG4AudioLATM); ok {
		audioConf = track.StreamMuxConfig.Programs[0].Layers[0].AudioSpecificConfig
	}

	if audioConf != nil {
		enc, err := audioConf.Marshal()
		if err != nil {
			return err
		}

		err = w.Conn.Write(&message.Audio{
			ChunkStreamID:   message.AudioChunkStreamID,
			MessageStreamID: 0x1000000,
			Codec:           message.CodecMPEG4Audio,
			Rate:            message.Rate44100,
			Depth:           message.Depth16,
			IsStereo:        true,
			AACType:         message.AudioAACTypeConfig,
			Payload:         enc,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// WriteH264 writes H264 data.
func (w *Writer) WriteH264(pts time.Duration, dts time.Duration, au [][]byte) error {
	avcc, err := h264.AVCC(au).Marshal()
	if err != nil {
		return err
	}

	return w.Conn.Write(&message.Video{
		ChunkStreamID:   message.VideoChunkStreamID,
		MessageStreamID: 0x1000000,
		Codec:           message.CodecH264,
		IsKeyFrame:      h264.IsRandomAccess(au),
		Type:            message.VideoTypeAU,
		Payload:         avcc,
		DTS:             dts,
		PTSDelta:        pts - dts,
	})
}

// WriteMPEG4Audio writes MPEG-4 Audio data.
func (w *Writer) WriteMPEG4Audio(pts time.Duration, au []byte) error {
	return w.Conn.Write(&message.Audio{
		ChunkStreamID:   message.AudioChunkStreamID,
		MessageStreamID: 0x1000000,
		Codec:           message.CodecMPEG4Audio,
		Rate:            message.Rate44100,
		Depth:           message.Depth16,
		IsStereo:        true,
		AACType:         message.AudioAACTypeAU,
		Payload:         au,
		DTS:             pts,
	})
}

// WriteMPEG1Audio writes MPEG-1 Audio data.
func (w *Writer) WriteMPEG1Audio(pts time.Duration, h *mpeg1audio.FrameHeader, frame []byte) error {
	return w.Conn.Write(&message.Audio{
		ChunkStreamID:   message.AudioChunkStreamID,
		MessageStreamID: 0x1000000,
		Codec:           message.CodecMPEG1Audio,
		Rate:            audioRateIntToRTMP(h.SampleRate),
		Depth:           message.Depth16,
		IsStereo:        mpeg1AudioChannels(h.ChannelMode),
		Payload:         frame,
		DTS:             pts,
	})
}
