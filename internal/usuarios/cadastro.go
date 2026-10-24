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
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// Papel diz o que a pessoa pode fazer. O admin faz tudo; o viewer vê as
// câmeras, as gravações e as detecções, e não mexe no cadastro nem vê o
// diagnóstico do servidor.
type Papel string

const (
	PapelAdmin  Papel = "admin"
	PapelViewer Papel = "viewer"
)

func (p Papel) valido() bool { return p == PapelAdmin || p == PapelViewer }

// Arquivo é o usuarios.json.
type Arquivo struct {
	// Dono guarda o que o dwnvr.yaml não tem do dono. A senha dele continua
	// no yaml.
	Dono     Dono      `json:"owner"`
	Usuarios []Usuario `json:"users"`
}

type Dono struct {
	// Nome de exibição. Vazio é "use o usuário do dwnvr.yaml".
	Nome string `json:"name,omitempty"`
	// Avatar é o id da foto na pasta avatares/, ou vazio (ver TrocarAvatar).
	Avatar string `json:"avatar,omitempty"`
}

type Usuario struct {
	// Usuario é o login: curto, fixo, escolhido pelo admin (ver
	// ValidarUsuario).
	Usuario string `json:"username"`
	// Nome é o de exibição.
	Nome string `json:"name"`
	// Email é para onde vai o link de convite, ou vazio. Único entre as
	// pessoas, sem diferença de maiúscula e minúscula (ver ValidarEmail).
	Email string `json:"email,omitempty"`
	Papel Papel  `json:"role"`
	// Senha é o valor guardado (ver GerarSenha), nunca a senha. Vazia é
	// "ainda não definiu": ninguém entra com este usuário.
	Senha string `json:"passwordHash,omitempty"`
	// Link é o link de convite em aberto, ou nil. Um por pessoa: gerar outro
	// substitui este.
	Link *Link `json:"invite,omitempty"`
	// Avatar é o id da foto na pasta avatares/, ou vazio (ver TrocarAvatar).
	Avatar   string    `json:"avatar,omitempty"`
	CriadoEm time.Time `json:"createdAt"`
	// AtivoDesde é quando a pessoa definiu a senha pelo link, ou zero enquanto
	// não definiu. Trocar a própria senha não mexe nele; link novo o zera.
	AtivoDesde time.Time `json:"activeSince,omitzero"`
}

// Link é o que o usuarios.json guarda do link de convite: o SHA-256 do token,
// nunca o token. Quem ler o arquivo não consegue montar o link.
type Link struct {
	TokenSha256 string    `json:"tokenSha256"`
	VenceEm     time.Time `json:"expiresAt"`
}

// Situacao diz em que pé está a pessoa: "active" (tem senha), "invitePending"
// (ainda pode definir a senha pelo link) ou "inviteExpired" (precisa de outro
// link para entrar).
func (u Usuario) Situacao(agora time.Time) string {
	switch {
	case u.Link != nil && agora.Before(u.Link.VenceEm):
		return "invitePending"
	case u.Link == nil && u.Senha != "":
		return "active"
	}
	return "inviteExpired"
}

// Recusado é o erro do que alguém digitou ou pediu, com a mensagem para ser
// lida na tela. A API o devolve como 400.
type Recusado string

func (e Recusado) Error() string { return string(e) }

var (
	// ErrNaoExiste é a pessoa que não está no cadastro.
	ErrNaoExiste = errors.New("usuário não encontrado")
	// ErrLinkInvalido é o link vencido, já usado, substituído por outro ou
	// inventado: para quem o abre, a saída é a mesma, pedir outro.
	ErrLinkInvalido = errors.New("link vencido ou já usado: peça outro ao administrador")
)

var formatoDoUsuario = regexp.MustCompile(`^[a-z0-9._-]+$`)

// ValidarUsuario confere o login: letras minúsculas, números, ".", "_" e "-",
// até TamanhoMaximoDoUsuario caracteres.
func ValidarUsuario(u string) error {
	if u == "" {
		return Recusado("usuário vazio")
	}
	if len(u) > TamanhoMaximoDoUsuario {
		return Recusado(fmt.Sprintf("usuário com mais de %d caracteres", TamanhoMaximoDoUsuario))
	}
	if !formatoDoUsuario.MatchString(u) {
		return Recusado("usuário só aceita letras minúsculas, números, ponto, _ e -")
	}
	return nil
}

// ValidarNome confere o nome de exibição e o devolve sem os espaços das pontas.
func ValidarNome(nome string) (string, error) {
	nome = strings.TrimSpace(nome)
	switch {
	case nome == "":
		return "", Recusado("nome vazio")
	case utf8.RuneCountInString(nome) > TamanhoMaximoDoNome:
		return "", Recusado(fmt.Sprintf("nome com mais de %d caracteres", TamanhoMaximoDoNome))
	case strings.IndexFunc(nome, unicode.IsControl) >= 0:
		return "", Recusado("nome com caractere de controle")
	}
	return nome, nil
}

// ValidarEmail confere o e-mail da pessoa: um endereço só, sem nome na
// frente, como "fulano@exemplo.com". Vazio vale: é a pessoa sem e-mail.
func ValidarEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	switch {
	case email == "":
		return "", nil
	case len(email) > TamanhoMaximoDoEmail:
		return "", Recusado(fmt.Sprintf("e-mail com mais de %d caracteres", TamanhoMaximoDoEmail))
	}
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		return "", Recusado("e-mail inválido: escreva só o endereço, como fulano@exemplo.com")
	}
	return email, nil
}

// EmailValido é o e-mail da pessoa, se ele passa no ValidarEmail, ou vazio: o
// escrito errado à mão no arquivo conta como sem e-mail, e não sai dele.
func (u Usuario) EmailValido() string {
	if e, err := ValidarEmail(u.Email); err == nil && e == u.Email {
		return e
	}
	return ""
}

// emailEmUso diz se outra pessoa, que não exceto, já tem o e-mail. Conta toda
// entrada do arquivo, até a ignorada: ela pode voltar a valer quando alguém a
// corrigir.
func emailEmUso(a *Arquivo, email, exceto string) bool {
	return slices.ContainsFunc(a.Usuarios, func(u Usuario) bool {
		return u.Usuario != exceto && strings.EqualFold(u.Email, email)
	})
}

// ValidarSenha confere só o tamanho, em caracteres: sem regra de composição,
// como pede o NIST SP 800-63B.
func ValidarSenha(senha string) error {
	n := utf8.RuneCountInString(senha)
	switch {
	case n < SenhaMinima:
		return Recusado(fmt.Sprintf("a senha precisa de pelo menos %d caracteres", SenhaMinima))
	case n > SenhaMaxima:
		return Recusado(fmt.Sprintf("a senha passa de %d caracteres", SenhaMaxima))
	}
	return nil
}

// Cadastro é o usuarios.json carregado. É lido uma vez, no boot, e cada
// alteração grava o arquivo inteiro e troca a cópia em memória.
type Cadastro struct {
	caminho string
	// avatares é a pasta das fotos, ao lado do usuarios.json.
	avatares string
	// dono é o server.username do dwnvr.yaml: ninguém do arquivo pode ter o
	// mesmo login.
	dono string

	mu  sync.RWMutex
	arq Arquivo
	// ilegivel guarda por que o arquivo não pôde ser lido. Com ele, o cadastro
	// fica vazio e Alterar recusa: gravar por cima apagaria quem está no
	// arquivo.
	ilegivel error

	// agora é o relógio dos links. Os testes o trocam para vencer um link sem
	// esperar.
	agora func() time.Time
}

// Abrir lê o usuarios.json. Arquivo que não existe é cadastro vazio.
//
// Entrada inválida não impede o boot nem some do arquivo: fica de fora de
// Buscar e vira aviso, para o log. Arquivo ilegível também não impede o boot,
// porque a gravação não pode parar por causa dele: vem o erro, e o cadastro
// vem vazio e recusa gravar. Só o dono entra até o arquivo ser corrigido.
func Abrir(caminho, dono string) (c *Cadastro, avisos []string, err error) {
	c = &Cadastro{caminho: caminho, avatares: filepath.Join(filepath.Dir(caminho), "avatares"),
		dono: dono, agora: time.Now}
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
	emails := map[string]string{} // o e-mail em minúsculas, e de quem é
	for i, u := range c.arq.Usuarios {
		if motivo := c.motivoParaIgnorar(u, vistos); motivo != "" {
			avisos = append(avisos, fmt.Sprintf("usuário #%d (%q) ignorado: %s", i, u.Usuario, motivo))
		}
		vistos[u.Usuario] = true
		if u.Email == "" {
			continue
		}
		if u.EmailValido() == "" {
			avisos = append(avisos, fmt.Sprintf("usuário %q: e-mail %q ignorado, a pessoa fica sem e-mail", u.Usuario, u.Email))
			continue
		}
		chave := strings.ToLower(u.Email)
		if outro, ok := emails[chave]; ok {
			avisos = append(avisos, fmt.Sprintf("usuário %q: o e-mail %q é o mesmo de %q", u.Usuario, u.Email, outro))
			continue
		}
		emails[chave] = u.Usuario
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
	if i := c.indice(&c.arq, usuario); i >= 0 {
		return c.arq.Usuarios[i], true
	}
	return Usuario{}, false
}

// indice acha a entrada válida da pessoa no arquivo, ou -1.
func (c *Cadastro) indice(arq *Arquivo, usuario string) int {
	vistos := map[string]bool{}
	for i, u := range arq.Usuarios {
		if c.motivoParaIgnorar(u, vistos) == "" && u.Usuario == usuario {
			return i
		}
		vistos[u.Usuario] = true
	}
	return -1
}

// Listar devolve as pessoas que valem, na ordem do arquivo. A entrada
// inválida fica de fora, como no Buscar.
func (c *Cadastro) Listar() []Usuario {
	c.mu.RLock()
	defer c.mu.RUnlock()
	lista := []Usuario{}
	vistos := map[string]bool{}
	for _, u := range c.arq.Usuarios {
		if c.motivoParaIgnorar(u, vistos) == "" {
			lista = append(lista, u)
		}
		vistos[u.Usuario] = true
	}
	return lista
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
	// O Link é ponteiro: sem a cópia, f mexeria no da cópia em memória antes
	// de a gravação dar certo.
	for i, u := range novo.Usuarios {
		if u.Link != nil {
			l := *u.Link
			novo.Usuarios[i].Link = &l
		}
	}
	if err := f(&novo); err != nil {
		return err
	}
	if novo.Usuarios == nil {
		novo.Usuarios = []Usuario{} // "users": [], e não null
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
