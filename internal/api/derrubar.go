package api

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Derrubar alguém tem duas metades. A sessão cai sozinha: a chave do cookie
// sai da senha guardada (ver chaveDaSessao), e remover a pessoa ou gerar link
// novo muda essa senha, então toda requisição dela passa a ser recusada. O
// que não cai sozinho são as conexões já abertas, que não voltam a apresentar
// o cookie: o websocket do ao vivo e o aviso de sessão. É delas que este
// arquivo cuida.
//
// O WebRTC não passa por aqui, e o dwnvr não tem como cortá-lo: a mídia vai
// direto do go2rtc ao navegador pela UDP, e o próprio player fecha o
// websocket da negociação assim que o vídeo toca. O go2rtc 1.9 não tem rota
// para derrubar um consumidor; só reiniciá-lo, o que pararia todas as câmeras
// e a gravação. Quem fecha o WebRTC é a tela: o aviso de sessão a avisa na
// hora, e a conferência periódica do Ao vivo é a rede de segurança.

// conexoes são as conexões abertas de cada pessoa, pelo login.
type conexoes struct {
	mu        sync.Mutex
	porPessoa map[string]map[net.Conn]struct{}
}

// registrar anota a conexão da pessoa. soltar a tira da lista, sem fechá-la.
func (c *conexoes) registrar(usuario string, conn net.Conn) (soltar func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.porPessoa == nil {
		c.porPessoa = map[string]map[net.Conn]struct{}{}
	}
	if c.porPessoa[usuario] == nil {
		c.porPessoa[usuario] = map[net.Conn]struct{}{}
	}
	c.porPessoa[usuario][conn] = struct{}{}
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.porPessoa[usuario], conn)
		if len(c.porPessoa[usuario]) == 0 {
			delete(c.porPessoa, usuario)
		}
	}
}

// derrubar fecha todas as conexões abertas da pessoa e diz quantas eram.
func (c *conexoes) derrubar(usuario string) int {
	c.mu.Lock()
	abertas := c.porPessoa[usuario]
	delete(c.porPessoa, usuario)
	c.mu.Unlock()
	for conn := range abertas {
		_ = conn.Close()
	}
	return len(abertas)
}

// derrubarPessoa fecha o que a pessoa tem aberto, depois de ela ter sido
// removida ou ganhado link novo.
func (s *Server) derrubarPessoa(usuario, motivo string) {
	if n := s.abertas.derrubar(usuario); n > 0 {
		s.log.Info("conexões da pessoa derrubadas", "usuario", usuario, "motivo", motivo, "conexoes", n)
	}
}

// anotaHijack é o ResponseWriter do proxy do ao vivo: quando o
// httputil.ReverseProxy toma a conexão para o websocket, ela é registrada
// como da pessoa.
type anotaHijack struct {
	http.ResponseWriter
	aoTomar func(net.Conn)
}

func (w anotaHijack) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.aoTomar(conn)
	}
	return conn, rw, err
}

func (w anotaHijack) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// handleLive é o proxy do ao vivo com a conexão anotada: derrubar a pessoa
// fecha o websocket, e com ele o vídeo que vem por dentro dele (MSE) e a
// negociação em curso.
func (s *Server) handleLive(proxy http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var soltar func()
		usuario := pessoaDe(r).Usuario
		proxy.ServeHTTP(anotaHijack{w, func(c net.Conn) { soltar = s.abertas.registrar(usuario, c) }}, r)
		// O ReverseProxy só volta quando o websocket fecha.
		if soltar != nil {
			soltar()
		}
	}
}

// handleAvisoDeSessao é um websocket que não leva mensagem nenhuma: o
// servidor o fecha quando a pessoa é removida ou ganha link novo, e a tela,
// ao vê-lo fechar, confere a sessão. É como o Ao vivo fica sabendo na hora
// que precisa fechar o WebRTC, que o dwnvr não alcança.
//
// Escrito à mão, sem biblioteca: sem mensagens, o protocolo se resume ao
// aperto de mão da RFC 6455 e a esperar o outro lado fechar.
func (s *Server) handleAvisoDeSessao(w http.ResponseWriter, r *http.Request) {
	chave := r.Header.Get("Sec-WebSocket-Key")
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || chave == "" ||
		r.Header.Get("Sec-WebSocket-Version") != "13" {
		writeError(w, http.StatusBadRequest, "esperava um websocket")
		return
	}
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		s.log.Warn("aviso de sessão sem websocket", "erro", err)
		return
	}
	defer conn.Close()

	h := sha1.Sum([]byte(chave + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n" +
		"Connection: Upgrade\r\nSec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n")
	if rw.Flush() != nil {
		return
	}
	// O ReadHeaderTimeout do servidor deixou um prazo na conexão.
	_ = conn.SetDeadline(time.Time{})

	soltar := s.abertas.registrar(pessoaDe(r).Usuario, conn)
	defer soltar()
	// O navegador não manda nada além do fechamento: ler até o fim é esperar
	// a aba fechar, ou o servidor derrubar.
	_, _ = io.Copy(io.Discard, rw)
}
