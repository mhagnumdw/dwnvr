package fmp4

// Leitura de resolução a partir do SPS (Sequence Parameter Set) de H.264 e
// H.265.
//
// Existe porque os campos width/height da VisualSampleEntry MENTEM. A cozinha
// do primeiro deployment é o caso real: a câmera anuncia 2560x1440 no DESCRIBE
// do RTSP, o go2rtc monta o hvcC com esse anúncio, e o stream que chega é
// 1920x1080 com um SPS in-band diferente. Quem decodifica obedece o SPS, então
// é o SPS que diz a verdade sobre o que está gravado no disco.
//
// Nada disso decodifica mídia: o SPS é um cabeçalho de ~60 bytes, lido uma vez
// por conexão.

// Tipos de NAL que carregam o SPS em cada codec.
const (
	nalTypeSPS264 = 7
	nalTypeSPS265 = 33
)

// bitReader lê campos de largura em bits de um RBSP já desescapado.
type bitReader struct {
	b   []byte
	pos int // em bits
	err bool
}

func (r *bitReader) u(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		if r.pos >= len(r.b)*8 {
			r.err = true
			return v
		}
		bit := (r.b[r.pos>>3] >> (7 - uint(r.pos&7))) & 1
		v = v<<1 | uint32(bit)
		r.pos++
	}
	return v
}

func (r *bitReader) skip(n int) {
	if r.pos+n > len(r.b)*8 {
		r.err = true
		r.pos = len(r.b) * 8
		return
	}
	r.pos += n
}

func (r *bitReader) flag() bool { return r.u(1) == 1 }

// ue lê um inteiro Exp-Golomb sem sinal.
func (r *bitReader) ue() uint32 {
	zeros := 0
	for !r.err && r.u(1) == 0 {
		zeros++
		// Um prefixo maior que isto não é um SPS válido, é lixo — e sem o teto
		// um stream corrompido prenderia o laço até o fim do buffer.
		if zeros > 32 {
			r.err = true
			return 0
		}
	}
	if r.err || zeros == 0 {
		return 0
	}
	return uint32(1)<<uint(zeros) - 1 + r.u(zeros)
}

func (r *bitReader) se() int32 {
	k := r.ue()
	if k%2 == 1 {
		return int32(k+1) / 2
	}
	return -int32(k / 2)
}

// unescape remove os bytes de prevenção de emulação (00 00 03 -> 00 00), que
// existem no NAL para que a sequência de início nunca apareça no meio dos
// dados. Sem tirá-los, os campos saem deslocados.
func unescape(nal []byte) []byte {
	out := make([]byte, 0, len(nal))
	zeros := 0
	for _, c := range nal {
		if zeros == 2 && c == 3 {
			zeros = 0
			continue
		}
		if c == 0 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, c)
	}
	return out
}

// chromaSub devolve SubWidthC e SubHeightC, os fatores que convertem a janela
// de corte (expressa em amostras de croma) para amostras de luma.
func chromaSub(format uint32) (uint32, uint32) {
	switch format {
	case 1: // 4:2:0
		return 2, 2
	case 2: // 4:2:2
		return 2, 1
	default: // monocromático e 4:4:4 não subamostram
		return 1, 1
	}
}

// SPSSize devolve a resolução declarada num SPS, já descontada a janela de
// corte. O codec vem do 4CC da sample entry.
func SPSSize(codec string, nal []byte) (w, h uint16, ok bool) {
	switch codec {
	case "avc1", "avc3":
		return h264SPSSize(nal)
	case "hev1", "hvc1":
		return h265SPSSize(nal)
	}
	return 0, 0, false
}

// h264SPSSize lê a resolução de um SPS de H.264 (ISO/IEC 14496-10, 7.3.2.1.1).
func h264SPSSize(nal []byte) (uint16, uint16, bool) {
	if len(nal) < 4 {
		return 0, 0, false
	}
	r := &bitReader{b: unescape(nal[1:])} // 1 byte de cabeçalho do NAL

	profile := r.u(8)
	r.skip(16) // constraint flags + level_idc
	r.ue()     // seq_parameter_set_id

	chroma := uint32(1) // 4:2:0 é o implícito nos perfis que não trazem o campo
	switch profile {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		chroma = r.ue()
		if chroma == 3 {
			r.skip(1) // separate_colour_plane_flag
		}
		r.ue()        // bit_depth_luma_minus8
		r.ue()        // bit_depth_chroma_minus8
		r.skip(1)     // qpprime_y_zero_transform_bypass_flag
		if r.flag() { // seq_scaling_matrix_present_flag
			lists := 8
			if chroma == 3 {
				lists = 12
			}
			for i := 0; i < lists; i++ {
				if r.flag() {
					size := 16
					if i >= 6 {
						size = 64
					}
					skipScalingList(r, size)
				}
			}
		}
	}

	r.ue() // log2_max_frame_num_minus4
	switch r.ue() {
	case 0:
		r.ue() // log2_max_pic_order_cnt_lsb_minus4
	case 1:
		r.skip(1) // delta_pic_order_always_zero_flag
		r.se()    // offset_for_non_ref_pic
		r.se()    // offset_for_top_to_bottom_field
		for n := r.ue(); n > 0 && !r.err; n-- {
			r.se()
		}
	}
	r.ue()    // max_num_ref_frames
	r.skip(1) // gaps_in_frame_num_value_allowed_flag

	widthMBs := r.ue() + 1
	heightUnits := r.ue() + 1
	frameMBsOnly := r.flag()
	if !frameMBsOnly {
		r.skip(1) // mb_adaptive_frame_field_flag
	}
	r.skip(1) // direct_8x8_inference_flag

	w := widthMBs * 16
	h := heightUnits * 16
	if !frameMBsOnly {
		// Sem frame_mbs_only cada unidade do mapa é meio quadro.
		h *= 2
	}

	if r.flag() { // frame_cropping_flag
		left, right, top, bottom := r.ue(), r.ue(), r.ue(), r.ue()
		cx, cy := chromaSub(chroma)
		if chroma == 0 {
			cx = 1
		}
		if !frameMBsOnly {
			cy *= 2
		}
		w = subCrop(w, cx*(left+right))
		h = subCrop(h, cy*(top+bottom))
	}
	return finish(r, w, h)
}

// skipScalingList consome uma lista de escala sem guardá-la: só o tamanho da
// imagem interessa aqui, mas pular errado desalinharia tudo que vem depois.
func skipScalingList(r *bitReader, size int) {
	last, next := int32(8), int32(8)
	for i := 0; i < size && !r.err; i++ {
		if next != 0 {
			next = (last + r.se() + 256) % 256
		}
		if next != 0 {
			last = next
		}
	}
}

// h265SPSSize lê a resolução de um SPS de H.265 (ISO/IEC 23008-2, 7.3.2.2.1).
func h265SPSSize(nal []byte) (uint16, uint16, bool) {
	if len(nal) < 8 {
		return 0, 0, false
	}
	r := &bitReader{b: unescape(nal[2:])} // 2 bytes de cabeçalho do NAL

	r.skip(4) // sps_video_parameter_set_id
	maxSubLayers := int(r.u(3))
	r.skip(1) // sps_temporal_id_nesting_flag
	skipProfileTierLevel(r, maxSubLayers)

	r.ue() // sps_seq_parameter_set_id
	chroma := r.ue()
	if chroma == 3 {
		r.skip(1) // separate_colour_plane_flag
	}

	w := r.ue() // pic_width_in_luma_samples
	h := r.ue() // pic_height_in_luma_samples

	if r.flag() { // conformance_window_flag
		left, right, top, bottom := r.ue(), r.ue(), r.ue(), r.ue()
		cx, cy := chromaSub(chroma)
		w = subCrop(w, cx*(left+right))
		h = subCrop(h, cy*(top+bottom))
	}
	return finish(r, w, h)
}

// skipProfileTierLevel consome o bloco de perfil/nível, cujo tamanho depende do
// número de subcamadas. É o campo de comprimento variável que vem antes da
// resolução, então errar aqui é ler dimensão de lugar nenhum.
func skipProfileTierLevel(r *bitReader, maxSubLayers int) {
	r.skip(96) // perfil, tier, compatibilidade, restrições e nível gerais

	if maxSubLayers == 0 {
		return
	}
	var profilePresent, levelPresent [8]bool
	for i := 0; i < maxSubLayers && i < 8; i++ {
		profilePresent[i] = r.flag()
		levelPresent[i] = r.flag()
	}
	for i := maxSubLayers; i < 8; i++ {
		r.skip(2) // reserved_zero_2bits
	}
	for i := 0; i < maxSubLayers && i < 8; i++ {
		if profilePresent[i] {
			r.skip(88)
		}
		if levelPresent[i] {
			r.skip(8)
		}
	}
}

// subCrop tira o corte sem dar a volta no inteiro: um SPS corrompido não pode
// virar uma resolução gigante.
func subCrop(v, crop uint32) uint32 {
	if crop >= v {
		return 0
	}
	return v - crop
}

// finish valida o resultado. Uma leitura que estourou o buffer produz números
// sem significado, e mostrar isso na tela seria pior que não mostrar nada.
func finish(r *bitReader, w, h uint32) (uint16, uint16, bool) {
	const maxDim = 1 << 16
	if r.err || w == 0 || h == 0 || w >= maxDim || h >= maxDim {
		return 0, 0, false
	}
	return uint16(w), uint16(h), true
}

// FindSPS procura um SPS entre os NALs prefixados por tamanho de um fragmento.
//
// É por onde a verdade entra quando o init mente: parameter sets in-band vêm
// sempre logo antes do IDR, então basta olhar o fragmento do primeiro keyframe
// de cada conexão.
func FindSPS(codec string, data []byte, lengthSize int) ([]byte, bool) {
	if lengthSize < 1 || lengthSize > 4 {
		return nil, false
	}
	want := nalTypeSPS264
	if codec == "hev1" || codec == "hvc1" {
		want = nalTypeSPS265
	}

	for p := 0; p+lengthSize <= len(data); {
		n := 0
		for i := 0; i < lengthSize; i++ {
			n = n<<8 | int(data[p+i])
		}
		p += lengthSize
		if n <= 0 || p+n > len(data) {
			return nil, false
		}
		nal := data[p : p+n]

		typ := int(nal[0] & 0x1f)
		if want == nalTypeSPS265 {
			typ = int(nal[0]>>1) & 0x3f
		}
		if typ == want {
			return nal, true
		}
		p += n
	}
	return nil, false
}
