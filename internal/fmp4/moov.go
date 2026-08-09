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

	// Width e Height são as dimensões da trilha de vídeo, lidas do SPS que o
	// avcC/hvcC carrega. Ficam zeradas nas trilhas de áudio e quando o SPS não
	// é legível.
	//
	// Não vêm dos campos width/height da VisualSampleEntry de propósito: o
	// go2rtc os preenche com o que a câmera ANUNCIA, e há câmera anunciando
	// 2560x1440 enquanto transmite 1920x1080. Ver sps.go.
	Width  uint16
	Height uint16

	// NALLengthSize é o tamanho do prefixo de comprimento dos NALs, declarado
	// no avcC/hvcC. Necessário para varrer os NALs de um fragmento.
	NALLengthSize int

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

// visualSampleEntryFixed é o tamanho da parte fixa de uma VisualSampleEntry,
// contando o cabeçalho da caixa: 8 do cabeçalho, 8 da SampleEntry (reservados
// e data_reference_index) e 70 dos campos visuais (pre_defined, dimensões,
// resoluções, frame_count, compressorname, depth). Depois disso começam as
// caixas filhas, entre elas o avcC/hvcC.
const visualSampleEntryFixed = 86

// videoConfig lê o SPS e o tamanho do prefixo de NAL de dentro do avcC/hvcC de
// uma sample entry de vídeo.
//
// Uma entrada sem configuração legível devolve zeros: não saber a resolução não
// invalida o init, e recusar a gravação por causa disso seria desproporcional.
func videoConfig(codec string, entry []byte) (w, h uint16, nalLen int) {
	if len(entry) <= visualSampleEntryFixed {
		return 0, 0, 0
	}
	_ = walk(entry[visualSampleEntryFixed:], func(typ string, body []byte) error {
		var sps []byte
		switch typ {
		case "avcC":
			sps, nalLen = avcCSPS(body)
		case "hvcC":
			sps, nalLen = hvcCSPS(body)
		default:
			return nil
		}
		if sps != nil {
			w, h, _ = SPSSize(codec, sps)
		}
		return nil
	})
	return w, h, nalLen
}

// avcCSPS extrai o primeiro SPS de um AVCDecoderConfigurationRecord.
//
//	version(1) profile(1) compat(1) level(1)
//	0b111111|lengthSizeMinusOne(1)  0b111|numOfSPS(1)
//	{ spsLength(2) sps }...
func avcCSPS(b []byte) ([]byte, int) {
	if len(b) < 7 {
		return nil, 0
	}
	nalLen := int(b[4]&0x03) + 1
	if b[5]&0x1f == 0 {
		return nil, nalLen
	}
	n, ok := be16(b, 6)
	if !ok || 8+int(n) > len(b) {
		return nil, nalLen
	}
	return b[8 : 8+int(n)], nalLen
}

// hvcCSPS extrai o SPS de um HEVCDecoderConfigurationRecord, cujos parameter
// sets vêm agrupados por tipo de NAL depois de 22 bytes de cabeçalho fixo.
func hvcCSPS(b []byte) ([]byte, int) {
	if len(b) < 23 {
		return nil, 0
	}
	nalLen := int(b[21]&0x03) + 1

	p := 23
	for arrays := int(b[22]); arrays > 0; arrays-- {
		if p+3 > len(b) {
			return nil, nalLen
		}
		typ := int(b[p] & 0x3f)
		count, _ := be16(b, p+1)
		p += 3
		for ; count > 0; count-- {
			n, ok := be16(b, p)
			if !ok || p+2+int(n) > len(b) {
				return nil, nalLen
			}
			nal := b[p+2 : p+2+int(n)]
			if typ == nalTypeSPS265 {
				return nal, nalLen
			}
			p += 2 + int(n)
		}
	}
	return nil, nalLen
}

func parseTrak(trak []byte) (Track, error) {
	var t Track

	// A sample entry é guardada para ser lida depois do walk: o layout dela
	// depende do handler da trilha, e não há garantia na ordem das caixas de
	// que o hdlr tenha sido visto antes do stsd.
	var sampleEntry []byte

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
								sampleEntry = body[8:]
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
	if t.Handler == HandlerVideo {
		t.Width, t.Height, t.NALLengthSize = videoConfig(t.Codec, sampleEntry)
	}
	if t.Timescale == 0 {
		return t, fmt.Errorf("fmp4: trilha %d sem timescale", t.ID)
	}
	return t, nil
}
