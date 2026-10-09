package usuarios

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"time"
)

// Sem SMTP, quem entra pela primeira vez entra por um link: o admin cria a
// pessoa, o dwnvr gera o link, e ela mesma define a senha. O mesmo link serve
// para "esqueci a senha" e "perdi o celular": gerar outro apaga a senha atual.

// Convite é o link recém-gerado. O Token só existe aqui, na resposta a quem o
// gerou: o arquivo guarda o hash dele.
type Convite struct {
	Usuario Usuario
	Token   string
}

// novoLink sorteia o token e devolve o que vai para o arquivo.
func (c *Cadastro) novoLink() (token string, l *Link, err error) {
	b := make([]byte, BytesDoToken)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	vence := c.agora().Add(ValidadeDoLink).Truncate(time.Second)
	return token, &Link{TokenSha256: hashDoToken(token), VenceEm: vence}, nil
}

func hashDoToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Criar cadastra uma pessoa comum, ainda sem senha, e devolve o link para ela
// definir a sua.
func (c *Cadastro) Criar(usuario, nome string) (Convite, error) {
	if err := ValidarUsuario(usuario); err != nil {
		return Convite{}, err
	}
	nome, err := ValidarNome(nome)
	if err != nil {
		return Convite{}, err
	}
	token, link, err := c.novoLink()
	if err != nil {
		return Convite{}, err
	}
	u := Usuario{Usuario: usuario, Nome: nome, Papel: PapelComum, Link: link,
		CriadoEm: c.agora().Truncate(time.Second)}

	err = c.Alterar(func(a *Arquivo) error {
		if usuario == c.dono {
			return Recusado("este é o usuário do dono, no dwnvr.yaml")
		}
		// Qualquer entrada com o mesmo login, mesmo a ignorada por inválida:
		// a nova viria depois dela e seria a ignorada.
		if slices.ContainsFunc(a.Usuarios, func(o Usuario) bool { return o.Usuario == usuario }) {
			return Recusado("já existe alguém com este usuário")
		}
		a.Usuarios = append(a.Usuarios, u)
		return nil
	})
	if err != nil {
		return Convite{}, err
	}
	return Convite{Usuario: u, Token: token}, nil
}

// NovoLink gera outro link para a pessoa. A senha dela deixa de valer na hora,
// e o link anterior também: só o novo define a senha. As sessões dela caem
// porque a chave do cookie sai da senha guardada (ver api.chaveDaSessao).
func (c *Cadastro) NovoLink(usuario string) (Convite, error) {
	token, link, err := c.novoLink()
	if err != nil {
		return Convite{}, err
	}
	var u Usuario
	err = c.Alterar(func(a *Arquivo) error {
		i := c.indice(a, usuario)
		if i < 0 {
			return ErrNaoExiste
		}
		a.Usuarios[i].Senha = ""
		a.Usuarios[i].Link = link
		u = a.Usuarios[i]
		return nil
	})
	if err != nil {
		return Convite{}, err
	}
	return Convite{Usuario: u, Token: token}, nil
}

// Remover apaga a pessoa do cadastro.
func (c *Cadastro) Remover(usuario string) error {
	return c.Alterar(func(a *Arquivo) error {
		i := c.indice(a, usuario)
		if i < 0 {
			return ErrNaoExiste
		}
		a.Usuarios = slices.Delete(a.Usuarios, i, i+1)
		return nil
	})
}

// porToken acha a pessoa do link, se ele ainda vale.
func (c *Cadastro) porToken(a *Arquivo, token string) int {
	if token == "" {
		return -1
	}
	quer := []byte(hashDoToken(token))
	agora := c.agora()
	vistos := map[string]bool{}
	for i, u := range a.Usuarios {
		valida := c.motivoParaIgnorar(u, vistos) == ""
		vistos[u.Usuario] = true
		if !valida || u.Link == nil || !agora.Before(u.Link.VenceEm) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(u.Link.TokenSha256), quer) == 1 {
			return i
		}
	}
	return -1
}

// Conferir diz de quem é o link, se ele ainda vale.
func (c *Cadastro) Conferir(token string) (Usuario, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	i := c.porToken(&c.arq, token)
	if i < 0 {
		return Usuario{}, ErrLinkInvalido
	}
	return c.arq.Usuarios[i], nil
}

// DefinirSenha grava a senha da pessoa do link e apaga o link: ele é de uso
// único. A conta da senha espera a vez na fila (ver GerarSenha) e é feita fora
// da trava do cadastro; o link é conferido de novo na hora de gravar, e se ele
// venceu ou foi usado nesse meio-tempo, nada muda.
func (c *Cadastro) DefinirSenha(ctx context.Context, token, senha string) (Usuario, error) {
	if _, err := c.Conferir(token); err != nil {
		return Usuario{}, err
	}
	if err := ValidarSenha(senha); err != nil {
		return Usuario{}, err
	}
	guardado, err := GerarSenha(ctx, senha)
	if err != nil {
		return Usuario{}, err
	}
	var u Usuario
	err = c.Alterar(func(a *Arquivo) error {
		i := c.porToken(a, token)
		if i < 0 {
			return ErrLinkInvalido
		}
		a.Usuarios[i].Senha = guardado
		a.Usuarios[i].Link = nil
		u = a.Usuarios[i]
		return nil
	})
	return u, err
}
