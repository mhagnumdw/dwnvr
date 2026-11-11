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
type connections struct {
	mu        sync.Mutex
	perPerson map[string]map[net.Conn]struct{}
}

// registrar anota a conexão da pessoa. soltar a tira da lista, sem fechá-la.
func (c *connections) registrar(username string, conn net.Conn) (release func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.perPerson == nil {
		c.perPerson = map[string]map[net.Conn]struct{}{}
	}
	if c.perPerson[username] == nil {
		c.perPerson[username] = map[net.Conn]struct{}{}
	}
	c.perPerson[username][conn] = struct{}{}
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.perPerson[username], conn)
		if len(c.perPerson[username]) == 0 {
			delete(c.perPerson, username)
		}
	}
}

// derrubar fecha todas as conexões abertas da pessoa e diz quantas eram.
func (c *connections) drop(username string) int {
	c.mu.Lock()
	open := c.perPerson[username]
	delete(c.perPerson, username)
	c.mu.Unlock()
	for conn := range open {
		_ = conn.Close()
	}
	return len(open)
}

// derrubarPessoa fecha o que a pessoa tem aberto, depois de ela ter sido
// removida ou ganhado link novo.
func (s *Server) dropPerson(username, reason string) {
	if n := s.open.drop(username); n > 0 {
		s.log.Info("conexões da pessoa derrubadas", "usuario", username, "motivo", reason, "conexoes", n)
	}
}

// anotaHijack é o ResponseWriter do proxy do ao vivo: quando o
// httputil.ReverseProxy toma a conexão para o websocket, ela é registrada
// como da pessoa.
type hijackRecorder struct {
	http.ResponseWriter
	onTake func(net.Conn)
}

func (w hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.onTake(conn)
	}
	return conn, rw, err
}

func (w hijackRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// handleLive é o proxy do ao vivo com a conexão anotada: derrubar a pessoa
// fecha o websocket, e com ele o vídeo que vem por dentro dele (MSE) e a
// negociação em curso.
func (s *Server) handleLive(proxy http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var release func()
		username := personOf(r).Username
		proxy.ServeHTTP(hijackRecorder{w, func(c net.Conn) { release = s.open.registrar(username, c) }}, r)
		// O ReverseProxy só volta quando o websocket fecha.
		if release != nil {
			release()
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
func (s *Server) handleSessionWarning(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || key == "" ||
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

	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n" +
		"Connection: Upgrade\r\nSec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n")
	if rw.Flush() != nil {
		return
	}
	// O ReadHeaderTimeout do servidor deixou um prazo na conexão.
	_ = conn.SetDeadline(time.Time{})

	release := s.open.registrar(personOf(r).Username, conn)
	defer release()
	// O navegador não manda nada além do fechamento: ler até o fim é esperar
	// a aba fechar, ou o servidor derrubar.
	_, _ = io.Copy(io.Discard, rw)
}
