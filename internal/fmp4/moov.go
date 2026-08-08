package fmp4

import (
	"errors"
	"fmt"
)

// Handlers de mídia que interessam ao dwnvr.
const (
	HandlerVideo = "vide"
	HandlerAudio = "soun"
)

// Track descreve uma trilha do init segment.
type Track struct {
	ID        uint32
	Handler   string // "vide" ou "soun"
	Timescale uint32 // unidades por segundo dos timestamps desta trilha
	Codec     string // 4CC da sample entry: hev1, hvc1, avc1, mp4a, fLaC, Opus…

	// DefaultSampleFlags vem do trex e é o último recurso para classificar um
	// sample quando o fragmento não traz flags próprios.
	DefaultSampleFlags uint32
}

// Movie é o resultado da leitura de um init segment (ftyp + moov).
type Movie struct {
	Tracks []Track
}

// VideoTrack devolve a primeira trilha de vídeo. Só ela decide onde cortar um
// segmento: samples de áudio também vêm marcados como "não dependem de outros"
// (go2rtc usa SampleAudio = sampleDependsOn2), então classificar keyframe sem
// olhar a trilha faria todo pacote de áudio parecer um ponto de corte válido.
func (m *Movie) VideoTrack() (Track, bool) {
	for _, t := range m.Tracks {
		if t.Handler == HandlerVideo {
			return t, true
		}
	}
	return Track{}, false
}

func (m *Movie) HasAudio() bool {
	for _, t := range m.Tracks {
		if t.Handler == HandlerAudio {
			return true
		}
	}
	return false
}

// ParseMoov lê a caixa moov completa (com cabeçalho) e extrai as trilhas.
func ParseMoov(moov []byte) (*Movie, error) {
	hdrLen, size, ok := boxHeaderLen(moov)
	if !ok || boxType(moov) != "moov" {
		return nil, errors.New("fmp4: esperava uma caixa moov")
	}

	mv := &Movie{}
	trexFlags := map[uint32]uint32{}

	err := walk(moov[hdrLen:size], func(typ string, body []byte) error {
		switch typ {
		case "trak":
			t, err := parseTrak(body)
			if err != nil {
				return err
			}
			mv.Tracks = append(mv.Tracks, t)
		case "mvex":
			return walk(body, func(typ string, body []byte) error {
				if typ != "trex" {
					return nil
				}
				// trex: version+flags(4), track_ID(4), default_sample_description_index(4),
				//       default_sample_duration(4), default_sample_size(4), default_sample_flags(4)
				id, ok1 := be32(body, 4)
				fl, ok2 := be32(body, 20)
				if ok1 && ok2 {
					trexFlags[id] = fl
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for i := range mv.Tracks {
		mv.Tracks[i].DefaultSampleFlags = trexFlags[mv.Tracks[i].ID]
	}
	if len(mv.Tracks) == 0 {
		return nil, errors.New("fmp4: moov sem nenhuma trilha")
	}
	return mv, nil
}

func parseTrak(trak []byte) (Track, error) {
	var t Track
	err := walk(trak, func(typ string, body []byte) error {
		switch typ {
		case "tkhd":
			// version 0: creation(4) modification(4) track_ID(4)
			// version 1: creation(8) modification(8) track_ID(4)
			off := 12
			if len(body) > 0 && body[0] == 1 {
				off = 20
			}
			if id, ok := be32(body, off); ok {
				t.ID = id
			}
		case "mdia":
			return walk(body, func(typ string, body []byte) error {
				switch typ {
				case "mdhd":
					// mesma variação de layout por versão que o tkhd
					off := 12
					if len(body) > 0 && body[0] == 1 {
						off = 20
					}
					if ts, ok := be32(body, off); ok {
						t.Timescale = ts
					}
				case "hdlr":
					// version+flags(4), pre_defined(4), handler_type(4)
					if len(body) >= 12 {
						t.Handler = string(body[8:12])
					}
				case "minf":
					return walk(body, func(typ string, body []byte) error {
						if typ != "stbl" {
							return nil
						}
						return walk(body, func(typ string, body []byte) error {
							if typ != "stsd" {
								return nil
							}
							// stsd: version+flags(4), entry_count(4), depois as sample entries
							if len(body) >= 16 {
								t.Codec = boxType(body[8:])
							}
							return nil
						})
					})
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return t, err
	}
	if t.Timescale == 0 {
		return t, fmt.Errorf("fmp4: trilha %d sem timescale", t.ID)
	}
	return t, nil
}
