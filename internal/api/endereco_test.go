package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// O endereço de quem fez a requisição, direto e atrás de proxy, com o
// trustedProxies padrão.
func TestClienteDe(t *testing.T) {
	confiaveis, err := config.Prefixos(config.TrustedProxiesPadrao)
	if err != nil {
		t.Fatal(err)
	}
	for _, caso := range []struct {
		nome, remoto string
		xff          []string
		quer         string
	}{
		// Direto na 8080, da rede de casa: o header é de quem o mandou.
		{"direto, sem header", "192.168.1.5:5000", nil, "192.168.1.5"},
		{"direto, header forjado", "192.168.1.5:5000", []string{"203.0.113.9"}, "192.168.1.5"},
		// Pelo `tailscale serve` ou pelo Caddy: a conexão vem da rede do compose.
		{"pelo proxy", "172.18.0.1:5000", []string{"100.64.0.7"}, "100.64.0.7"},
		{"pelo proxy, sem header", "172.18.0.1:5000", nil, "172.18.0.1"},
		// Um proxy que acrescenta ao header que veio: o forjado fica à esquerda.
		{"proxy que acrescenta", "172.18.0.3:5000", []string{"203.0.113.9, 192.168.1.5"}, "192.168.1.5"},
		{"dois headers", "172.18.0.3:5000", []string{"203.0.113.9", "192.168.1.5"}, "192.168.1.5"},
		// Toda a cadeia confiável: fica o mais distante.
		{"cadeia confiável", "127.0.0.1:5000", []string{"172.18.0.1"}, "172.18.0.1"},
		// Lixo no header: fica o último endereço bom antes dele.
		{"lixo no header", "172.18.0.1:5000", []string{"lixo, 172.20.0.2"}, "172.20.0.2"},
		{"IPv4 dentro de IPv6", "[::ffff:172.18.0.1]:5000", []string{"100.64.0.7"}, "100.64.0.7"},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
			r.RemoteAddr = caso.remoto
			for _, v := range caso.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clienteDe(r, confiaveis).String(); got != caso.quer {
				t.Errorf("%s com %q: %s, esperava %s", caso.remoto, caso.xff, got, caso.quer)
			}
		})
	}
}

// Com a lista vazia, ninguém é confiável: vale sempre a conexão.
func TestClienteDeSemProxies(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	r.RemoteAddr = "172.18.0.1:5000"
	r.Header.Set("X-Forwarded-For", "100.64.0.7")
	if got := clienteDe(r, nil).String(); got != "172.18.0.1" {
		t.Errorf("%s, esperava a conexão", got)
	}
}
