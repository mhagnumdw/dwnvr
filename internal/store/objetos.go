package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"sync"
)

// objetosDoDia é o que fica memorizado de um arquivo de eventos: só as marcas
// de objeto, e até onde o arquivo já foi lido.
type objetosDoDia struct {
	// info é o arquivo como estava na última leitura. Outro inode quer dizer
	// que ele foi regravado (a retenção apara com rename), e aí o que está
	// memorizado não vale mais.
	info os.FileInfo
	// lido é até onde o arquivo foi consumido: sempre o fim de uma linha
	// inteira, nunca o meio da que o recorder pode estar escrevendo.
	lido    int64
	objetos []Evento
}

// objetosMemo guarda os dias já lidos, por câmera. Fica fora do Camera.mu de
// propósito: ler um arquivo de eventos segurando aquele lock pararia o
// recorder, que o toma a cada segmento.
type objetosMemo struct {
	mu   sync.Mutex
	dias map[string]*objetosDoDia
}

// marcaDeObjeto é o que distingue a linha de objeto da de onset sem decodificar
// o JSON: a de onset só tem `instanteMs`. Num dia são milhares de onsets para
// dezenas de objetos, e o Unmarshal é o que custa.
var marcaDeObjeto = []byte(`"familia"`)

// ObjetosDoDia devolve as marcas de objeto de um dia, em ordem de instante. É o
// que a tela de Detecções pagina.
//
// A leitura é memorizada por dia, e é o que deixa a rolagem barata: sem isso,
// cada página relê e decodifica milhares de onsets de nove câmeras para achar
// algumas dezenas de marcas. Um dia passado é lido uma vez só. O dia corrente
// cresce, e aí só o pedaço novo do arquivo é lido - ele é append-only.
//
// A fatia devolvida é só de leitura, e continua válida depois: a próxima
// leitura monta uma fatia nova em vez de mexer nesta.
func (c *Camera) ObjetosDoDia(day string) ([]Evento, error) {
	c.memo.mu.Lock()
	defer c.memo.mu.Unlock()

	caminho := c.EventosPath(day)
	info, err := os.Stat(caminho)
	if errors.Is(err, os.ErrNotExist) {
		// Dia sem detecção, ou já levado pela retenção.
		delete(c.memo.dias, day)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	m := c.memo.dias[day]
	if m != nil && os.SameFile(m.info, info) && info.Size() == m.lido {
		return m.objetos, nil
	}
	if m == nil || !os.SameFile(m.info, info) || info.Size() < m.lido {
		m = &objetosDoDia{}
	}

	novos, lido, err := leObjetos(caminho, m.lido)
	if err != nil {
		return nil, err
	}
	if len(novos) > 0 {
		// Fatia nova, e não append na antiga: quem recebeu a anterior pode
		// estar lendo, e ordenar no lugar mexeria debaixo dele. A ordem
		// precisa ser refeita porque a marca entra no arquivo quando o
		// detector responde, e a fila pode responder fora da ordem dos onsets.
		todos := make([]Evento, 0, len(m.objetos)+len(novos))
		todos = append(append(todos, m.objetos...), novos...)
		sort.SliceStable(todos, func(i, j int) bool { return todos[i].InstanteMs < todos[j].InstanteMs })
		m = &objetosDoDia{objetos: todos}
	}
	m.info = info
	m.lido = lido
	if c.memo.dias == nil {
		c.memo.dias = map[string]*objetosDoDia{}
	}
	c.memo.dias[day] = m
	return m.objetos, nil
}

// leObjetos lê as marcas de objeto do arquivo a partir de `desde`, e devolve
// até onde leu. A última linha sem '\n' fica para a próxima vez: é a que o
// recorder pode estar no meio de escrever.
func leObjetos(caminho string, desde int64) ([]Evento, int64, error) {
	f, err := os.Open(caminho)
	if err != nil {
		return nil, desde, err
	}
	defer f.Close()
	if _, err := f.Seek(desde, io.SeekStart); err != nil {
		return nil, desde, err
	}

	var out []Evento
	lido := desde
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		linha, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// Linha maior que 64 KB não é marca nenhuma: é lixo de uma queda.
			// Descarta até o fim dela.
			lido += int64(len(linha))
			for err == bufio.ErrBufferFull {
				linha, err = r.ReadSlice('\n')
				lido += int64(len(linha))
			}
			continue
		}
		if err == io.EOF {
			return out, lido, nil // o que sobrou não tem '\n' ainda
		}
		if err != nil {
			return nil, desde, err
		}
		lido += int64(len(linha))
		if !bytes.Contains(linha, marcaDeObjeto) {
			continue
		}
		var ev Evento
		// Mesma tolerância do LoadEventos: uma queda deixa linha cortada ou
		// bytes nulos, e a linha vai embora.
		if json.Unmarshal(linha, &ev) != nil || !ev.EhObjeto() {
			continue
		}
		out = append(out, ev)
	}
}
