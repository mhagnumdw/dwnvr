// Package fmp4 lê a estrutura de caixas de um MP4 fragmentado sem decodificar
// mídia. O dwnvr nunca toca nos bytes de vídeo: ele só precisa saber onde cada
// fragmento começa, se carrega um keyframe e qual o tempo de decodificação -
// o suficiente para cortar segmentos e montar um índice.
package fmp4

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// MaxBoxSize limita o tamanho de uma caixa aceita. Sem esse teto, um stream
// corrompido (ou um servidor hostil) faria o recorder alocar sem limite num
// dispositivo que tem 1,5 GB de RAM no total.
const MaxBoxSize = 32 << 20

// Reader lê caixas de topo de um fMP4 vindo de um stream contínuo.
type Reader struct {
	br  *bufio.Reader
	buf []byte
}

func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 64<<10)}
}

// NextBox lê a próxima caixa de topo e devolve seu tipo e os bytes completos,
// cabeçalho incluído.
//
// O slice devolvido é reaproveitado entre chamadas: quem chama deve gravá-lo ou
// copiá-lo antes de chamar NextBox de novo. Isso evita uma alocação por frame -
// com 9 câmeras a 15 fps seriam 135 alocações por segundo sem necessidade.
func (r *Reader) NextBox() (string, []byte, error) {
	var hdr [16]byte
	if _, err := io.ReadFull(r.br, hdr[:8]); err != nil {
		return "", nil, err
	}

	size := int64(binary.BigEndian.Uint32(hdr[0:4]))
	typ := string(hdr[4:8])
	hdrLen := int64(8)

	switch size {
	case 1:
		// Tamanho estendido de 64 bits nos 8 bytes seguintes.
		if _, err := io.ReadFull(r.br, hdr[8:16]); err != nil {
			return "", nil, err
		}
		size = int64(binary.BigEndian.Uint64(hdr[8:16]))
		hdrLen = 16
	case 0:
		// "vai até o fim do arquivo": impossível de delimitar num stream.
		return "", nil, fmt.Errorf("fmp4: caixa %q com tamanho 0 não é suportada em stream", typ)
	}

	if size < hdrLen || size > MaxBoxSize {
		return "", nil, fmt.Errorf("fmp4: tamanho inválido %d para a caixa %q", size, typ)
	}

	if int64(cap(r.buf)) < size {
		r.buf = make([]byte, size)
	}
	b := r.buf[:size]
	copy(b, hdr[:hdrLen])

	if _, err := io.ReadFull(r.br, b[hdrLen:]); err != nil {
		return "", nil, err
	}
	return typ, b, nil
}

// boxHeaderLen devolve o tamanho do cabeçalho e o tamanho total de uma caixa
// que já está inteira em memória.
func boxHeaderLen(b []byte) (hdrLen, size int, ok bool) {
	if len(b) < 8 {
		return 0, 0, false
	}
	size = int(binary.BigEndian.Uint32(b[0:4]))
	hdrLen = 8
	if size == 1 {
		if len(b) < 16 {
			return 0, 0, false
		}
		size = int(binary.BigEndian.Uint64(b[8:16]))
		hdrLen = 16
	}
	if size < hdrLen || size > len(b) {
		return 0, 0, false
	}
	return hdrLen, size, true
}

// boxType devolve o tipo de uma caixa que já está em memória.
func boxType(b []byte) string {
	if len(b) < 8 {
		return ""
	}
	return string(b[4:8])
}

// BoxPayload devolve o conteúdo de uma caixa sem o cabeçalho, respeitando o
// cabeçalho estendido de 16 bytes. Um mdat grande usa essa forma, e assumir 8
// bytes cegamente faria a leitura começar no meio de um campo.
func BoxPayload(b []byte) []byte {
	hdrLen, size, ok := boxHeaderLen(b)
	if !ok {
		return nil
	}
	return b[hdrLen:size]
}

// walk percorre as caixas filhas contidas em payload, chamando fn para cada uma
// com o corpo (sem o cabeçalho). Parar cedo é possível devolvendo errStopWalk.
func walk(payload []byte, fn func(typ string, body []byte) error) error {
	for off := 0; off+8 <= len(payload); {
		hdrLen, size, ok := boxHeaderLen(payload[off:])
		if !ok {
			return fmt.Errorf("fmp4: caixa malformada no offset %d", off)
		}
		typ := boxType(payload[off:])
		if err := fn(typ, payload[off+hdrLen:off+size]); err != nil {
			return err
		}
		off += size
	}
	return nil
}

// be16, be32 e be64 leem inteiros big-endian conferindo os limites, para que um
// stream truncado vire erro em vez de panic.
func be16(b []byte, off int) (uint16, bool) {
	if off < 0 || off+2 > len(b) {
		return 0, false
	}
	return binary.BigEndian.Uint16(b[off : off+2]), true
}

func be32(b []byte, off int) (uint32, bool) {
	if off < 0 || off+4 > len(b) {
		return 0, false
	}
	return binary.BigEndian.Uint32(b[off : off+4]), true
}

func be64(b []byte, off int) (uint64, bool) {
	if off < 0 || off+8 > len(b) {
		return 0, false
	}
	return binary.BigEndian.Uint64(b[off : off+8]), true
}
