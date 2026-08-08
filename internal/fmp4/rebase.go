package fmp4

import (
	"encoding/binary"
	"errors"
)

// RebaseMoof reescreve, no lugar, o tfdt de cada traf subtraindo bases[trackID].
//
// É o que torna cada segmento um arquivo autônomo. O go2rtc entrega tfdt
// contínuo desde o início da conexão, então sem isso o segundo segmento
// começaria em t=24s, o terceiro em t=48s e assim por diante: o arquivo abriria,
// mas anunciaria uma duração que cresce para sempre e um buraco no começo.
//
// A caixa mantém tamanho e versão originais — só o valor muda —, então nenhum
// tamanho de caixa acima precisa ser recalculado.
func RebaseMoof(moof []byte, bases map[uint32]uint64) error {
	hdrLen, size, ok := boxHeaderLen(moof)
	if !ok || boxType(moof) != "moof" {
		return errors.New("fmp4: esperava uma caixa moof")
	}

	return walk(moof[hdrLen:size], func(typ string, traf []byte) error {
		if typ != "traf" {
			return nil
		}

		var (
			trackID uint32
			tfdt    []byte
		)
		// walk devolve subfatias do mesmo array, então escrever em tfdt já
		// altera o buffer original.
		if err := walk(traf, func(typ string, body []byte) error {
			switch typ {
			case "tfhd":
				if id, ok := be32(body, 4); ok {
					trackID = id
				}
			case "tfdt":
				tfdt = body
			}
			return nil
		}); err != nil {
			return err
		}

		if tfdt == nil {
			return nil
		}
		base, ok := bases[trackID]
		if !ok {
			return nil
		}
		return rebaseTfdt(tfdt, base)
	})
}

func rebaseTfdt(body []byte, base uint64) error {
	if len(body) < 4 {
		return errors.New("fmp4: tfdt truncado")
	}
	if body[0] == 1 {
		v, ok := be64(body, 4)
		if !ok {
			return errors.New("fmp4: tfdt v1 truncado")
		}
		binary.BigEndian.PutUint64(body[4:12], subClamp(v, base))
		return nil
	}
	v, ok := be32(body, 4)
	if !ok {
		return errors.New("fmp4: tfdt v0 truncado")
	}
	binary.BigEndian.PutUint32(body[4:8], uint32(subClamp(uint64(v), base)))
	return nil
}

// subClamp evita underflow: a trilha de áudio pode estar alguns milissegundos
// atrás do keyframe de vídeo que abriu o segmento, e um uint64 negativo viraria
// um timestamp astronômico.
func subClamp(v, base uint64) uint64 {
	if v < base {
		return 0
	}
	return v - base
}

// ScaleTime converte um instante de uma timescale para outra. Serve para
// derivar a base de cada trilha a partir do keyframe de vídeo que abriu o
// segmento, preservando o desalinhamento real entre áudio e vídeo em vez de
// zerar as duas trilhas independentemente.
func ScaleTime(t uint64, from, to uint32) uint64 {
	if from == 0 || from == to {
		return t
	}
	return uint64(float64(t) / float64(from) * float64(to))
}
