package fmp4

import "errors"

// Bits de sample_flags definidos em ISO/IEC 14496-12 §8.8.3.1, tratados como um
// uint32 big-endian:
//
//	bits 6-7  sample_depends_on        (2 = não depende de outros = keyframe)
//	bit  15   sample_is_non_sync_sample (1 = NÃO é ponto de sincronismo)
//
// go2rtc escreve exatamente esses valores em pkg/iso/atoms.go:
//
//	SampleVideoIFrame    = 0x2000000
//	SampleVideoNonIFrame = 0x1000000 | 0x10000
const (
	sampleIsNonSync    = 0x00010000
	sampleDependsOnMsk = 0x03000000
	sampleDependsOnNo  = 0x02000000 // não depende de outros samples
)

// isSyncSample decide se um sample é ponto de entrada válido para decodificação.
//
// O teste primário é o bit sample_is_non_sync_sample, que é o que o padrão
// define. Alguns muxers deixam esse bit zerado em tudo e só preenchem
// sample_depends_on; por isso, quando o bit de não-sincronismo está limpo,
// ainda exigimos que depends_on não diga explicitamente "depende de outros".
func isSyncSample(flags uint32) bool {
	if flags&sampleIsNonSync != 0 {
		return false
	}
	if dep := flags & sampleDependsOnMsk; dep != 0 && dep != sampleDependsOnNo {
		return false
	}
	return true
}

// Fragment resume um par moof+mdat: o suficiente para decidir corte de segmento
// e alimentar o índice, sem olhar um único byte de mídia.
type Fragment struct {
	TrackID uint32
	// BaseDecodeTime é o tfdt, na timescale da trilha.
	BaseDecodeTime uint64
	HasBaseTime    bool
	// Keyframe só é verdadeiro para a trilha de vídeo indicada a ParseMoof.
	Keyframe bool
	// SampleCount é quantos samples o fragmento carrega (go2rtc emite 1).
	SampleCount uint32

	// Duration é a soma das durações dos samples, na timescale da trilha.
	//
	// Sem ela só se sabe QUANDO o último frame começa, não quando ele termina.
	// A diferença é um frame - irrelevante para exibir, decisiva para emendar
	// segmentos: sem somá-la, o segmento seguinte começa em cima do último
	// frame do anterior em vez de depois dele, e o DTS regride na emenda.
	Duration uint64
}

// EndTime é o instante em que o fragmento termina, na timescale da trilha.
func (f Fragment) EndTime() uint64 { return f.BaseDecodeTime + f.Duration }

var errNoTraf = errors.New("fmp4: moof sem traf")

// ParseMoof lê uma caixa moof completa (com cabeçalho) e resume o traf da
// trilha videoTrackID. Se o moof for de outra trilha (áudio), devolve o
// TrackID encontrado com Keyframe=false.
func ParseMoof(moof []byte, videoTrackID uint32) (Fragment, error) {
	var f Fragment

	hdrLen, size, ok := boxHeaderLen(moof)
	if !ok || boxType(moof) != "moof" {
		return f, errors.New("fmp4: esperava uma caixa moof")
	}

	found := false
	err := walk(moof[hdrLen:size], func(typ string, body []byte) error {
		if typ != "traf" {
			return nil
		}
		found = true
		return parseTraf(body, videoTrackID, &f)
	})
	if err != nil {
		return f, err
	}
	if !found {
		return f, errNoTraf
	}
	return f, nil
}

func parseTraf(traf []byte, videoTrackID uint32, f *Fragment) error {
	var (
		defaultSampleFlags uint32
		haveDefaultFlags   bool
		trunFlags          uint32
		haveTrunFlags      bool
		defaultSampleDur   uint32
	)

	err := walk(traf, func(typ string, body []byte) error {
		switch typ {
		case "tfhd":
			// version(1) + flags(3), track_ID(4), depois campos opcionais na
			// ordem dos bits de flags.
			id, ok := be32(body, 4)
			if !ok {
				return errors.New("fmp4: tfhd truncado")
			}
			f.TrackID = id

			flags, _ := be32(body, 0)
			flags &= 0x00FFFFFF
			off := 8
			if flags&0x000001 != 0 { // base_data_offset
				off += 8
			}
			if flags&0x000002 != 0 { // sample_description_index
				off += 4
			}
			if flags&0x000008 != 0 { // default_sample_duration
				if v, ok := be32(body, off); ok {
					defaultSampleDur = v
				}
				off += 4
			}
			if flags&0x000010 != 0 { // default_sample_size
				off += 4
			}
			if flags&0x000020 != 0 { // default_sample_flags
				if v, ok := be32(body, off); ok {
					defaultSampleFlags, haveDefaultFlags = v, true
				}
			}

		case "tfdt":
			if len(body) == 0 {
				return errors.New("fmp4: tfdt vazio")
			}
			if body[0] == 1 {
				if v, ok := be64(body, 4); ok {
					f.BaseDecodeTime, f.HasBaseTime = v, true
				}
			} else {
				if v, ok := be32(body, 4); ok {
					f.BaseDecodeTime, f.HasBaseTime = uint64(v), true
				}
			}

		case "trun":
			flags, ok := be32(body, 0)
			if !ok {
				return errors.New("fmp4: trun truncado")
			}
			flags &= 0x00FFFFFF
			count, _ := be32(body, 4)
			f.SampleCount = count

			off := 8
			if flags&0x000001 != 0 { // data_offset
				off += 4
			}
			if flags&0x000004 != 0 { // first_sample_flags
				if v, ok := be32(body, off); ok {
					trunFlags, haveTrunFlags = v, true
				}
				off += 4
			}
			// Percorre a tabela de samples uma vez, somando durações e - se
			// ainda não vieram em first_sample_flags - pegando os flags do
			// primeiro sample, que é quem classifica o fragmento.
			entrySize := 0
			for _, f := range []uint32{0x000100, 0x000200, 0x000400, 0x000800} {
				if flags&f != 0 {
					entrySize += 4
				}
			}
			for i := uint32(0); i < count; i++ {
				p := off + int(i)*entrySize
				if flags&0x000100 != 0 { // sample_duration
					if v, ok := be32(body, p); ok {
						f.Duration += uint64(v)
					}
					p += 4
				} else {
					f.Duration += uint64(defaultSampleDur)
				}
				if flags&0x000200 != 0 { // sample_size
					p += 4
				}
				if i == 0 && !haveTrunFlags && flags&0x000400 != 0 {
					if v, ok := be32(body, p); ok {
						trunFlags, haveTrunFlags = v, true
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if f.TrackID != videoTrackID {
		return nil
	}

	switch {
	case haveTrunFlags:
		f.Keyframe = isSyncSample(trunFlags)
	case haveDefaultFlags:
		f.Keyframe = isSyncSample(defaultSampleFlags)
	default:
		// Sem nenhum flag no fragmento a decisão caberia ao trex; quem chama
		// resolve isso passando o default da trilha via DefaultSampleFlags.
		f.Keyframe = false
	}
	return nil
}
