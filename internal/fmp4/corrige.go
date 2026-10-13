package fmp4

import (
	"bytes"
	"encoding/binary"
)

// Correção do init que descreve um stream que não é o transmitido.
//
// O hvcC/avcC que o go2rtc escreve pode não ter os parameter sets do stream.
// No H.265, quando a câmera não os manda no SDP, o go2rtc grava um SPS fixo,
// do próprio código, que descreve 2560x1440 (docs/go2rtc-h265-parameter-sets.md);
// no H.264, há câmera cujo SDP descreve outro tamanho. Os verdadeiros vêm
// in-band, antes de cada IDR. O Chrome do computador obedece o in-band. O
// decodificador de hardware de alguns celulares se prepara pelo init e desenha
// o quadro de 640x360 no canto de uma superfície de 2560x1440: a imagem sai
// com 1/4 do tamanho, presa no canto de cima à esquerda.
//
// A correção troca os parameter sets do init pelos in-band e acerta as
// dimensões da sample entry e do tkhd. Os bytes de mídia continuam intocados.

// Tipos de NAL dos parameter sets. O SPS de cada codec já está em sps.go.
const (
	nalTypePPS264 = 8
	nalTypeVPS265 = 32
	nalTypePPS265 = 34
)

// tkhdDimensoes é o tamanho do corpo do tkhd até o fim de width/height, que
// são os dois últimos campos dele (16.16 cada), nas versões 0 e 1.
const (
	tkhdCorpoV0 = 84
	tkhdCorpoV1 = 96
)

// CorrigeInit devolve o init com os parameter sets que vieram in-band no
// keyframe, quando a resolução do SPS deles discorda da do init. Sem SPS
// in-band, ou com a mesma resolução, devolve false e o init original vale.
//
// keyframe é o conteúdo do mdat do primeiro keyframe da conexão, onde os
// parameter sets aparecem logo antes do IDR.
//
// Só corrige quando a resolução discorda, e não quando falta um PPS no init: o
// decodificador já lida com o PPS in-band, e mexer em todo init mudaria a
// geração de todas as câmeras sem nenhum defeito a resolver.
func CorrigeInit(init []byte, vt Track, keyframe []byte) ([]byte, bool) {
	hevc := vt.Codec == "hev1" || vt.Codec == "hvc1"
	if !hevc && vt.Codec != "avc1" && vt.Codec != "avc3" {
		return nil, false
	}

	ps := parameterSets(hevc, keyframe, vt.NALLengthSize)
	if len(ps.sps) == 0 {
		return nil, false
	}
	w, h, ok := SPSSize(vt.Codec, ps.sps[0])
	if !ok || (w == vt.Width && h == vt.Height) {
		return nil, false
	}

	c := &correcao{codec: vt.Codec, hevc: hevc, ps: ps, w: w, h: h}
	var out []byte
	for off := 0; off+8 <= len(init); {
		hdrLen, size, ok := boxHeaderLen(init[off:])
		if !ok {
			return nil, false
		}
		caixa := init[off : off+size]
		if boxType(caixa) == "moov" {
			nova, mudou := c.caixa(caixa, hdrLen)
			if c.falhou {
				return nil, false
			}
			if !mudou {
				return nil, false
			}
			caixa = nova
		}
		out = append(out, caixa...)
		off += size
	}
	return out, true
}

// conjunto são os parameter sets in-band, na ordem em que vieram.
type conjunto struct {
	vps, sps, pps [][]byte
}

// parameterSets junta os parameter sets do keyframe. Para no primeiro NAL de
// imagem: eles vêm sempre antes dele, e o resto do mdat é a imagem em si.
func parameterSets(hevc bool, data []byte, lengthSize int) conjunto {
	var ps conjunto
	if lengthSize < 1 || lengthSize > 4 {
		return ps
	}
	for p := 0; p+lengthSize <= len(data); {
		n := 0
		for i := range lengthSize {
			n = n<<8 | int(data[p+i])
		}
		p += lengthSize
		if n <= 0 || p+n > len(data) {
			return ps
		}
		nal := data[p : p+n]
		p += n

		if hevc {
			switch typ := int(nal[0]>>1) & 0x3f; {
			case typ == nalTypeVPS265:
				ps.vps = acrescenta(ps.vps, nal)
			case typ == nalTypeSPS265:
				ps.sps = acrescenta(ps.sps, nal)
			case typ == nalTypePPS265:
				ps.pps = acrescenta(ps.pps, nal)
			case typ < nalTypeVPS265: // imagem (VCL)
				return ps
			}
			continue
		}
		switch typ := int(nal[0] & 0x1f); {
		case typ == nalTypeSPS264:
			ps.sps = acrescenta(ps.sps, nal)
		case typ == nalTypePPS264:
			ps.pps = acrescenta(ps.pps, nal)
		case typ >= 1 && typ <= 5: // imagem (VCL)
			return ps
		}
	}
	return ps
}

// acrescenta guarda uma cópia do NAL, sem repetir um igual: a câmera pode
// repetir o parameter set, e o registro de configuração não precisa de dois.
func acrescenta(lista [][]byte, nal []byte) [][]byte {
	for _, j := range lista {
		if bytes.Equal(j, nal) {
			return lista
		}
	}
	return append(lista, bytes.Clone(nal))
}

// correcao remonta o moov descendo só pelo caminho até o hvcC/avcC. Cada caixa
// do caminho é refeita com o tamanho novo; as outras são copiadas como vieram.
type correcao struct {
	codec  string
	hevc   bool
	ps     conjunto
	w, h   uint16
	falhou bool
}

// caixa devolve a caixa refeita e se algo mudou dentro dela.
func (c *correcao) caixa(b []byte, hdrLen int) ([]byte, bool) {
	typ := boxType(b)
	_, size, _ := boxHeaderLen(b)
	corpo := b[hdrLen:size]

	switch typ {
	case "moov", "trak", "mdia", "minf", "stbl":
		nova, mudou := c.container(typ, nil, corpo)
		if !mudou {
			return b, false
		}
		if typ == "trak" {
			c.dimensoesDoTkhd(nova)
		}
		return nova, true
	case "stsd":
		// version+flags(4) e entry_count(4) antes das sample entries
		if len(corpo) < 8 {
			return b, false
		}
		nova, mudou := c.container(typ, corpo[:8], corpo[8:])
		if !mudou {
			return b, false
		}
		return nova, true
	case c.codec:
		// Os campos fixos da VisualSampleEntry vêm antes das caixas filhas;
		// width e height estão nos bytes 24 a 27 deles.
		fixos := visualSampleEntryFixed - 8
		if len(corpo) < fixos {
			return b, false
		}
		prefixo := bytes.Clone(corpo[:fixos])
		binary.BigEndian.PutUint16(prefixo[24:], c.w)
		binary.BigEndian.PutUint16(prefixo[26:], c.h)
		return c.container(typ, prefixo, corpo[fixos:])
	case "hvcC":
		novo := c.hvcC(corpo)
		if novo == nil {
			c.falhou = true
			return b, false
		}
		return caixaCom(typ, novo), true
	case "avcC":
		novo := c.avcC(corpo)
		if novo == nil {
			c.falhou = true
			return b, false
		}
		return caixaCom(typ, novo), true
	}
	return b, false
}

// container refaz uma caixa com o prefixo e os filhos, corrigindo cada filho,
// e diz se algum filho mudou. Se nenhum mudou, quem chamou fica com a caixa
// original, byte a byte.
func (c *correcao) container(typ string, prefixo, filhos []byte) ([]byte, bool) {
	corpo := append([]byte(nil), prefixo...)
	mudou := false
	for off := 0; off+8 <= len(filhos); {
		hdrLen, size, ok := boxHeaderLen(filhos[off:])
		if !ok {
			c.falhou = true
			return nil, false
		}
		nova, m := c.caixa(filhos[off:off+size], hdrLen)
		if c.falhou {
			return nil, false
		}
		mudou = mudou || m
		corpo = append(corpo, nova...)
		off += size
	}
	return caixaCom(typ, corpo), mudou
}

// dimensoesDoTkhd acerta width e height do tkhd da trilha, no lugar. São os
// dois últimos campos do corpo, em ponto fixo 16.16.
func (c *correcao) dimensoesDoTkhd(trak []byte) {
	_ = walk(trak[8:], func(typ string, corpo []byte) error {
		if typ != "tkhd" || len(corpo) < 1 {
			return nil
		}
		fim := tkhdCorpoV0
		if corpo[0] == 1 {
			fim = tkhdCorpoV1
		}
		if len(corpo) < fim {
			return nil
		}
		binary.BigEndian.PutUint32(corpo[fim-8:], uint32(c.w)<<16)
		binary.BigEndian.PutUint32(corpo[fim-4:], uint32(c.h)<<16)
		return nil
	})
}

// hvcC remonta o HEVCDecoderConfigurationRecord com os parameter sets
// in-band. Os 22 bytes de cabeçalho ficam, menos perfil, tier e nível, que
// passam a ser os do SPS novo. Arrays de outros tipos (SEI) ficam como vieram.
//
//	configurationVersion(1) general_profile_space|tier|profile_idc(1)
//	compatibility_flags(4) constraint_flags(6) level_idc(1) ...(9)
//	lengthSizeMinusOne no byte 21, numOfArrays no 22
//	{ completeness|NAL_unit_type(1) numNalus(2) { nalUnitLength(2) nal }... }...
func (c *correcao) hvcC(antigo []byte) []byte {
	if len(antigo) < 23 {
		return nil
	}
	novo := bytes.Clone(antigo[:22])

	// O SPS de H.265, depois dos 2 bytes de cabeçalho do NAL e de 1 byte de
	// ids, traz os mesmos 12 bytes de perfil e nível do registro, na mesma
	// ordem.
	if sps := unescape(c.ps.sps[0]); len(sps) >= 15 {
		copy(novo[1:13], sps[3:15])
	}

	// Lê os arrays antigos: o primeiro byte de cada tipo é mantido, para não
	// mudar o array_completeness, e os de outros tipos vão inteiros.
	cabecas := map[int]byte{}
	var outros [][]byte
	p := 23
	for n := int(antigo[22]); n > 0; n-- {
		if p+3 > len(antigo) {
			return nil
		}
		inicio := p
		typ := int(antigo[p] & 0x3f)
		cabecas[typ] = antigo[p]
		count, _ := be16(antigo, p+1)
		p += 3
		for ; count > 0; count-- {
			tam, ok := be16(antigo, p)
			if !ok || p+2+int(tam) > len(antigo) {
				return nil
			}
			p += 2 + int(tam)
		}
		if typ != nalTypeVPS265 && typ != nalTypeSPS265 && typ != nalTypePPS265 {
			outros = append(outros, antigo[inicio:p])
		}
	}

	var arrays []byte
	num := 0
	for _, a := range []struct {
		typ  int
		nals [][]byte
	}{{nalTypeVPS265, c.ps.vps}, {nalTypeSPS265, c.ps.sps}, {nalTypePPS265, c.ps.pps}} {
		if len(a.nals) == 0 {
			continue
		}
		cabeca, ok := cabecas[a.typ]
		if !ok {
			// No hvc1 os parameter sets só podem estar no init; no hev1
			// podem vir in-band também.
			cabeca = byte(a.typ)
			if c.codec == "hvc1" {
				cabeca |= 0x80
			}
		}
		arrays = append(arrays, cabeca)
		arrays = binary.BigEndian.AppendUint16(arrays, uint16(len(a.nals)))
		for _, nal := range a.nals {
			arrays = binary.BigEndian.AppendUint16(arrays, uint16(len(nal)))
			arrays = append(arrays, nal...)
		}
		num++
	}
	for _, o := range outros {
		arrays = append(arrays, o...)
		num++
	}

	novo = append(novo, byte(num))
	return append(novo, arrays...)
}

// avcC remonta o AVCDecoderConfigurationRecord com os parameter sets in-band.
//
//	version(1) profile(1) compat(1) level(1)
//	0b111111|lengthSizeMinusOne(1)  0b111|numOfSPS(1) { spsLength(2) sps }...
//	numOfPPS(1) { ppsLength(2) pps }...
//	e, nos perfis High, 4 bytes de croma e profundidade (mais as extensões)
func (c *correcao) avcC(antigo []byte) []byte {
	if len(antigo) < 7 {
		return nil
	}
	sps := c.ps.sps[0]
	if len(sps) < 4 {
		return nil
	}
	novo := []byte{1, sps[1], sps[2], sps[3], antigo[4], 0xe0 | byte(len(c.ps.sps))}
	for _, nal := range c.ps.sps {
		novo = binary.BigEndian.AppendUint16(novo, uint16(len(nal)))
		novo = append(novo, nal...)
	}
	novo = append(novo, byte(len(c.ps.pps)))
	for _, nal := range c.ps.pps {
		novo = binary.BigEndian.AppendUint16(novo, uint16(len(nal)))
		novo = append(novo, nal...)
	}

	switch sps[1] {
	case 100, 110, 122, 144:
		// Os perfis High exigem o bloco de croma e profundidade. Vale o do
		// init, se houver; senão, 4:2:0 em 8 bits, sem extensões.
		if cauda := caudaDoAvcC(antigo); len(cauda) > 0 {
			novo = append(novo, cauda...)
		} else {
			novo = append(novo, 0xfd, 0xf8, 0xf8, 0)
		}
	}
	return novo
}

// caudaDoAvcC devolve o que vem depois da lista de PPS de um avcC: o bloco
// dos perfis High, quando existe.
func caudaDoAvcC(b []byte) []byte {
	p := 5
	for range 2 { // SPS e depois PPS
		if p >= len(b) {
			return nil
		}
		n := int(b[p])
		if p == 5 {
			n &= 0x1f
		}
		p++
		for ; n > 0; n-- {
			tam, ok := be16(b, p)
			if !ok || p+2+int(tam) > len(b) {
				return nil
			}
			p += 2 + int(tam)
		}
	}
	return b[p:]
}

// caixaCom monta uma caixa com cabeçalho de 8 bytes. O init cabe com folga
// nos 32 bits de tamanho.
func caixaCom(typ string, corpo []byte) []byte {
	out := make([]byte, 8, 8+len(corpo))
	binary.BigEndian.PutUint32(out, uint32(8+len(corpo)))
	copy(out[4:], typ)
	return append(out, corpo...)
}
