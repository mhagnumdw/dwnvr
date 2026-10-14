package usuarios

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// Acessos guarda quando cada pessoa usou o dwnvr pela última vez: a última
// requisição com sessão válida, e não o último login. A sessão se renova
// sozinha (ver api.sessionRenew), então o login diria pouco.
//
// A requisição só mexe na memória. O disco fica com o acessos.json, gravado
// em segundo plano, no máximo a cada EsperaParaGravarOsAcessos, e no
// desligamento. Nenhuma requisição espera o disco.
//
// Fica num arquivo à parte, e não no usuarios.json, por dois motivos. O
// Cadastro segura o lock dele enquanto grava, e toda requisição autenticada o
// lê: gravar o acesso ali a cada poucos minutos pararia todas as requisições
// no fsync. E o arquivo com as senhas guardadas não precisa ser reescrito só
// porque alguém abriu o ao vivo.
type Acessos struct {
	caminho string
	log     *slog.Logger

	mu     sync.Mutex
	ultimo map[string]time.Time
	// pendente diz que a memória tem o que o arquivo ainda não tem.
	pendente bool
	// timer é a gravação agendada, ou nil.
	timer *time.Timer

	// gravando põe uma gravação por vez: a agendada e a do desligamento podem
	// se cruzar.
	gravando sync.Mutex

	// agora e espera são o relógio e a EsperaParaGravarOsAcessos. Os testes
	// os trocam para não esperar.
	agora  func() time.Time
	espera time.Duration
}

// arquivoDeAcessos é o acessos.json.
type arquivoDeAcessos struct {
	// UltimoAcesso vai do login ao instante.
	UltimoAcesso map[string]time.Time `json:"lastAccess"`
}

// AbrirAcessos lê o acessos.json. Arquivo que não existe é tudo vazio.
//
// Arquivo ilegível não impede o boot: vem o erro, para o log, e os acessos
// recomeçam vazios. A próxima gravação o substitui, porque o que se perde é
// só o último acesso de cada um, que volta na próxima vez que a pessoa usar.
func AbrirAcessos(caminho string, log *slog.Logger) (*Acessos, error) {
	a := &Acessos{caminho: caminho, log: log, ultimo: map[string]time.Time{},
		agora: time.Now, espera: EsperaParaGravarOsAcessos}
	b, err := os.ReadFile(caminho)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	var arq arquivoDeAcessos
	if err == nil {
		err = json.Unmarshal(b, &arq)
	}
	if err != nil {
		return a, fmt.Errorf("%s ilegível: %w", caminho, err)
	}
	if arq.UltimoAcesso != nil {
		a.ultimo = arq.UltimoAcesso
	}
	return a, nil
}

// Registrar anota que a pessoa acabou de usar o dwnvr. É chamado a cada
// requisição autenticada, e por isso só toca a memória, e só quando o
// último acesso dela tem mais de um PassoDoUltimoAcesso.
func (a *Acessos) Registrar(usuario string) {
	agora := a.agora()
	a.mu.Lock()
	defer a.mu.Unlock()
	// Com o relógio voltando para trás (o NTP corrigindo um relógio
	// adiantado), a diferença sai negativa e o instante é regravado: senão o
	// acesso ficaria parado no futuro até o relógio alcançá-lo.
	if d := agora.Sub(a.ultimo[usuario]); d >= 0 && d < PassoDoUltimoAcesso {
		return
	}
	a.ultimo[usuario] = agora.Truncate(time.Second)
	a.agendar()
}

// Ultimo devolve o último acesso da pessoa, ou zero se não há registro.
func (a *Acessos) Ultimo(usuario string) time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ultimo[usuario]
}

// Esquecer apaga o registro da pessoa. Vale para quem é removido e para quem
// é criado: sem isso, um login reaproveitado herdaria o acesso de outra
// pessoa.
func (a *Acessos) Esquecer(usuario string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.ultimo[usuario]; !ok {
		return
	}
	delete(a.ultimo, usuario)
	a.agendar()
}

// agendar marca o pendente e agenda a gravação, se ainda não há uma. Chamado
// com o mu preso.
func (a *Acessos) agendar() {
	a.pendente = true
	if a.timer == nil {
		a.timer = time.AfterFunc(a.espera, func() {
			if err := a.GravarPendente(); err != nil {
				a.log.Error("gravando os últimos acessos", "arquivo", a.caminho, "erro", err)
			}
		})
	}
}

// GravarPendente grava no acessos.json o que a memória tem de novo, se tiver.
// Roda no timer e no desligamento. Se a gravação falhar, o pendente continua
// marcado e outra tentativa fica agendada, sem esperar o próximo acesso: sem
// acesso, o que falhou só iria para o disco no desligamento, e uma queda de
// energia perderia mais que a espera.
func (a *Acessos) GravarPendente() error {
	a.gravando.Lock()
	defer a.gravando.Unlock()

	a.mu.Lock()
	if a.timer != nil {
		a.timer.Stop()
		a.timer = nil
	}
	if !a.pendente {
		a.mu.Unlock()
		return nil
	}
	foto := maps.Clone(a.ultimo)
	a.pendente = false
	a.mu.Unlock()

	b, err := json.MarshalIndent(arquivoDeAcessos{UltimoAcesso: foto}, "", "  ")
	if err == nil {
		// 0600, como o usuarios.json: diz quando cada pessoa usou o dwnvr.
		err = config.GravarAtomico(a.caminho, append(b, '\n'), 0o600)
	}
	if err != nil {
		a.mu.Lock()
		a.agendar()
		a.mu.Unlock()
	}
	return err
}
