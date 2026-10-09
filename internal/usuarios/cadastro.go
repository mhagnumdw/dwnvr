// Package usuarios guarda quem pode entrar no dwnvr além do dono: o
// usuarios.json, a senha de cada um e o papel.
//
// O dono continua no dwnvr.yaml (server.username e server.password), sempre
// admin, e é a recuperação se tudo der errado. Aqui fica só o que a tela
// grava.
package usuarios

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// Papel diz o que a pessoa pode fazer. O admin faz tudo; o comum vê as
// câmeras, as gravações e as detecções, e não mexe no cadastro nem vê o
// diagnóstico do servidor.
type Papel string

const (
	PapelAdmin Papel = "admin"
	PapelComum Papel = "comum"
)

func (p Papel) valido() bool { return p == PapelAdmin || p == PapelComum }

// Arquivo é o usuarios.json.
type Arquivo struct {
	// Dono guarda o que o dwnvr.yaml não tem do dono. A senha dele continua
	// no yaml.
	Dono     Dono      `json:"dono"`
	Usuarios []Usuario `json:"usuarios"`
}

type Dono struct {
	// Nome de exibição. Vazio é "use o usuário do dwnvr.yaml".
	Nome string `json:"nome,omitempty"`
}

type Usuario struct {
	// Usuario é o login: curto, fixo, escolhido pelo admin (ver
	// ValidarUsuario).
	Usuario string `json:"usuario"`
	// Nome é o de exibição.
	Nome  string `json:"nome"`
	Papel Papel  `json:"papel"`
	// Senha é o valor guardado (ver GerarSenha), nunca a senha. Vazia é
	// "ainda não definiu": ninguém entra com este usuário.
	Senha string `json:"senha,omitempty"`
}

var formatoDoUsuario = regexp.MustCompile(`^[a-z0-9._-]+$`)

// ValidarUsuario confere o login: letras minúsculas, números, ".", "_" e "-",
// até TamanhoMaximoDoUsuario caracteres.
func ValidarUsuario(u string) error {
	if u == "" {
		return errors.New("usuário vazio")
	}
	if len(u) > TamanhoMaximoDoUsuario {
		return fmt.Errorf("usuário com mais de %d caracteres", TamanhoMaximoDoUsuario)
	}
	if !formatoDoUsuario.MatchString(u) {
		return errors.New("usuário só aceita letras minúsculas, números, ponto, _ e -")
	}
	return nil
}

// Cadastro é o usuarios.json carregado. É lido uma vez, no boot, e cada
// alteração grava o arquivo inteiro e troca a cópia em memória.
type Cadastro struct {
	caminho string
	// dono é o server.username do dwnvr.yaml: ninguém do arquivo pode ter o
	// mesmo login.
	dono string

	mu  sync.RWMutex
	arq Arquivo
	// ilegivel guarda por que o arquivo não pôde ser lido. Com ele, o cadastro
	// fica vazio e Alterar recusa: gravar por cima apagaria quem está no
	// arquivo.
	ilegivel error
}

// Abrir lê o usuarios.json. Arquivo que não existe é cadastro vazio.
//
// Entrada inválida não impede o boot nem some do arquivo: fica de fora de
// Buscar e vira aviso, para o log. Arquivo ilegível também não impede o boot,
// porque a gravação não pode parar por causa dele: vem o erro, e o cadastro
// vem vazio e recusa gravar. Só o dono entra até o arquivo ser corrigido.
func Abrir(caminho, dono string) (c *Cadastro, avisos []string, err error) {
	c = &Cadastro{caminho: caminho, dono: dono}
	b, err := os.ReadFile(caminho)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &c.arq)
	}
	if err != nil {
		c.arq, c.ilegivel = Arquivo{}, fmt.Errorf("%s ilegível: %w", caminho, err)
		return c, nil, c.ilegivel
	}

	vistos := map[string]bool{}
	for i, u := range c.arq.Usuarios {
		if motivo := c.motivoParaIgnorar(u, vistos); motivo != "" {
			avisos = append(avisos, fmt.Sprintf("usuário #%d (%q) ignorado: %s", i, u.Usuario, motivo))
		}
		vistos[u.Usuario] = true
	}
	return c, avisos, nil
}

// motivoParaIgnorar diz por que a entrada não vale, ou "" se vale. vistos são
// os logins das entradas anteriores: no repetido, vale a primeira.
func (c *Cadastro) motivoParaIgnorar(u Usuario, vistos map[string]bool) string {
	switch {
	case ValidarUsuario(u.Usuario) != nil:
		return ValidarUsuario(u.Usuario).Error()
	case u.Usuario == c.dono:
		return "é o mesmo usuário do dono, no dwnvr.yaml"
	case vistos[u.Usuario]:
		return "repetido; vale a primeira entrada"
	case !u.Papel.valido():
		return fmt.Sprintf("papel %q desconhecido", u.Papel)
	}
	return ""
}

// Buscar acha a pessoa pelo login. O dono não está aqui: ele vem do
// dwnvr.yaml.
func (c *Cadastro) Buscar(usuario string) (Usuario, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	vistos := map[string]bool{}
	for _, u := range c.arq.Usuarios {
		if c.motivoParaIgnorar(u, vistos) == "" && u.Usuario == usuario {
			return u, true
		}
		vistos[u.Usuario] = true
	}
	return Usuario{}, false
}

// Dono devolve o que o arquivo guarda do dono.
func (c *Cadastro) Dono() Dono {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.arq.Dono
}

// Quantos diz quantas entradas o arquivo tem, válidas ou não.
func (c *Cadastro) Quantos() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.arq.Usuarios)
}

// Alterar aplica f numa cópia do arquivo, grava e só então troca a cópia em
// memória: se f ou a gravação falharem, nada muda. Uma alteração por vez,
// para duas abas não perderem a alteração uma da outra.
func (c *Cadastro) Alterar(f func(*Arquivo) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ilegivel != nil {
		return fmt.Errorf("o cadastro não é gravado enquanto o arquivo estiver ilegível: %w", c.ilegivel)
	}

	novo := Arquivo{Dono: c.arq.Dono, Usuarios: append([]Usuario(nil), c.arq.Usuarios...)}
	if err := f(&novo); err != nil {
		return err
	}
	if novo.Usuarios == nil {
		novo.Usuarios = []Usuario{} // "usuarios": [], e não null
	}
	b, err := json.MarshalIndent(novo, "", "  ")
	if err != nil {
		return err
	}
	// 0600, como o .session-secret: o arquivo tem as senhas guardadas.
	if err := config.GravarAtomico(c.caminho, append(b, '\n'), 0o600); err != nil {
		return err
	}
	c.arq = novo
	return nil
}
