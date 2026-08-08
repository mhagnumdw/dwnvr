package fmp4

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// InitGen identifica um init segment pelo hash do conteúdo.
//
// É o que permite detectar sozinho que o codec mudou (SPS diferente depois de
// uma reconexão) sem manter contador nenhum, e o que faz um segmento órfão
// recuperado no boot voltar apontando para o init certo.
func InitGen(init []byte) string {
	sum := sha256.Sum256(init)
	return hex.EncodeToString(sum[:6])
}

// SegmentInfo é o que o índice do dwnvr precisa saber sobre um segmento já
// gravado. Tudo isso sai da estrutura de caixas, sem decodificar mídia.
type SegmentInfo struct {
	// InitSize é o tamanho de ftyp+moov no começo do arquivo. A entrega via
	// MSE/HLS pula esse prefixo e serve o init separado, uma vez só.
	InitSize int64
	// FirstFragSize é o tamanho do primeiro par moof+mdat. Init +
	// primeiro fragmento formam um MP4 de um frame só: é assim que o dwnvr
	// serve thumbnail sem decodificar nada no Pi.
	FirstFragSize int64
	// DurationMs é o tempo coberto pelo segmento.
	DurationMs int64
	// Keyframes é quantos keyframes de vídeo o segmento contém.
	Keyframes int
	// Frames é o total de fragmentos de vídeo.
	Frames int
	// Gen é o hash do init embutido, igual ao usado pelo recorder.
	Gen   string
	Movie *Movie
}

// ProbeSegment lê a estrutura de um segmento gravado. É usado para reconstruir
// o índice quando ele se perde e para conferir, no boot, que o rabo do índice
// bate com o que está no disco.
func ProbeSegment(path string) (*SegmentInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return probeSegment(f)
}

func probeSegment(r io.Reader) (*SegmentInfo, error) {
	info := &SegmentInfo{}
	rd := NewReader(r)

	var (
		videoTrack   Track
		haveVideo    bool
		offset       int64
		lastVideoDTS uint64
		fragStart    int64 = -1
		init         []byte
	)

	for {
		typ, box, err := rd.NextBox()
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		n := int64(len(box))

		switch typ {
		case "ftyp":
			info.InitSize += n
			init = append(init, box...)
		case "moov":
			info.InitSize += n
			init = append(init, box...)
			info.Gen = InitGen(init)
			mv, err := ParseMoov(box)
			if err != nil {
				return nil, err
			}
			info.Movie = mv
			videoTrack, haveVideo = mv.VideoTrack()
		case "moof":
			if !haveVideo {
				break
			}
			if fragStart < 0 {
				fragStart = offset
			}
			frag, err := ParseMoof(box, videoTrack.ID)
			if err != nil {
				return nil, err
			}
			if frag.TrackID == videoTrack.ID {
				info.Frames++
				if frag.Keyframe {
					info.Keyframes++
				}
				lastVideoDTS = frag.BaseDecodeTime
			}
		case "mdat":
			// O primeiro moof+mdat completo define o tamanho da thumbnail.
			if info.FirstFragSize == 0 && fragStart >= 0 {
				info.FirstFragSize = offset + n - fragStart
			}
		}
		offset += n
	}

	if haveVideo && videoTrack.Timescale > 0 {
		info.DurationMs = int64(float64(lastVideoDTS) / float64(videoTrack.Timescale) * 1000)
	}
	return info, nil
}
