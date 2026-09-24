package go2rtc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// O go2rtc só lê o go2rtc.yaml quando sobe. Quem edita o arquivo com tudo no
// ar não vê efeito nenhum - nem câmera nova na tela de cadastro, nem a URL
// trocada de uma câmera que já grava - até reiniciá-lo. Este arquivo é o que
// deixa o dwnvr perceber isso e oferecer o reinício.

// Prazo das duas conversas abaixo: respostas triviais, pedidas por uma tela
// aberta, como a de Versao.
const arquivoTimeout = 3 * time.Second

// Divergencia é o que o go2rtc.yaml diz e o go2rtc em execução ainda não faz.
//
// A comparação é só da seção streams: mudança em outra seção (porta, WebRTC)
// não aparece aqui.
type Divergencia struct {
	// Novas estão no arquivo e não foram carregadas.
	Novas []string `json:"novas,omitempty"`
	// Alteradas existem nos dois lados, com fonte diferente.
	Alteradas []string `json:"alteradas,omitempty"`
	// ForaDoArquivo estão rodando e não estão mais no arquivo.
	ForaDoArquivo []string `json:"foraDoArquivo,omitempty"`
}

func (d Divergencia) Vazia() bool {
	return len(d.Novas) == 0 && len(d.Alteradas) == 0 && len(d.ForaDoArquivo) == 0
}

// Arquivo devolve o go2rtc.yaml como o go2rtc o lê do disco agora.
//
// O conteúdo tem as URLs RTSP com usuário e senha: fica no servidor, só para
// Divergencias, e nunca vai ao navegador.
func (c *Client) Arquivo(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, arquivoTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/config", nil)
	if err != nil {
		return nil, err
	}
	c.auth(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("go2rtc devolveu HTTP %d em /api/config", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// Reiniciar pede ao go2rtc que reinicie o próprio processo, e com isso volte a
// ler o go2rtc.yaml. O container continua o mesmo; todos os streams caem por
// alguns segundos, e os gravadores reconectam sozinhos.
func (c *Client) Reiniciar(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, arquivoTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/restart", nil)
	if err != nil {
		return err
	}
	c.auth(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("go2rtc devolveu HTTP %d em /api/restart", resp.StatusCode)
	}
	return nil
}

// Divergencias compara a seção streams do go2rtc.yaml com o que o go2rtc serve.
//
// Nova e fora do arquivo são sempre detectáveis: basta o nome. Alterada, só
// quando o go2rtc mostra a fonte, e ele não mostra sempre: enquanto uma fonte
// ffmpeg: ou exec: está sendo consumida, o produtor traz o comando já expandido
// no lugar do url. Sem o url não há com o que comparar, e a câmera fica de
// fora - melhor calar do que avisar mudança que não houve. RTSP ativo mantém
// o url, e é esse o caso de câmera de verdade.
func Divergencias(arquivo []byte, rodando map[string]Stream) (Divergencia, error) {
	var doc struct {
		Streams map[string]yaml.Node `yaml:"streams"`
	}
	if err := yaml.Unmarshal(arquivo, &doc); err != nil {
		return Divergencia{}, fmt.Errorf("go2rtc.yaml ilegível: %w", err)
	}

	var d Divergencia
	for nome, no := range doc.Streams {
		st, ok := rodando[nome]
		if !ok {
			d.Novas = append(d.Novas, nome)
			continue
		}
		if fontesMudaram(fontes(no), st.Producers) {
			d.Alteradas = append(d.Alteradas, nome)
		}
	}
	for nome := range rodando {
		if _, ok := doc.Streams[nome]; !ok {
			d.ForaDoArquivo = append(d.ForaDoArquivo, nome)
		}
	}

	// Os dois lados são map: sem ordenar, a lista mudaria de ordem a cada
	// recarga da tela.
	slices.Sort(d.Novas)
	slices.Sort(d.Alteradas)
	slices.Sort(d.ForaDoArquivo)
	return d, nil
}

// fontes lê o valor de um stream, que o go2rtc aceita como texto, lista de
// textos ou vazio.
func fontes(no yaml.Node) []string {
	switch no.Kind {
	case yaml.ScalarNode:
		if no.Value == "" {
			return nil
		}
		return []string{no.Value}
	case yaml.SequenceNode:
		var out []string
		for _, item := range no.Content {
			if item.Kind == yaml.ScalarNode && item.Value != "" {
				out = append(out, item.Value)
			}
		}
		return out
	}
	return nil
}

func fontesMudaram(noArquivo []string, producers []Producer) bool {
	// O go2rtc cria um produtor por fonte, na ordem do arquivo.
	if len(noArquivo) != len(producers) {
		return true
	}
	for i, p := range producers {
		if p.URL == "" {
			continue // fonte em uso sem url: não dá para saber
		}
		if !mesmaFonte(noArquivo[i], p.URL) {
			return true
		}
	}
	return false
}

// mesmaFonte compara duas fontes, tolerando o go2rtc ter guardado a URL sem os
// parâmetros depois do #. Trocar só um desses parâmetros passa despercebido; o
// contrário seria um aviso permanente de mudança que não houve.
func mesmaFonte(a, b string) bool {
	if a == b {
		return true
	}
	semParametros := func(s string) string {
		s, _, _ = strings.Cut(s, "#")
		return s
	}
	return semParametros(a) == semParametros(b)
}
