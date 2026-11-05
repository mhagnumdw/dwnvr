package usuarios

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"strings"
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

// recusasDoNovo é a régua de quem vai ser criado, campo a campo: a do Criar e
// a do ConferirNovo, que é a mesma para a tela não dizer "certo" a algo que o
// Criar recusa. Confere todos os campos, para a tela mostrar cada recusa
// embaixo do seu.
func (c *Cadastro) recusasDoNovo(a *Arquivo, usuario, nome, email string) Recusas {
	r := Recusas{}
	switch {
	case ValidarUsuario(usuario) != nil:
		r[CampoUsuario] = ValidarUsuario(usuario).Error()
	case usuario == c.dono:
		r[CampoUsuario] = "este é o usuário do dono, no dwnvr.yaml"
	// Qualquer entrada com o mesmo login, mesmo a ignorada por inválida: a
	// nova viria depois dela e seria a ignorada.
	case slices.ContainsFunc(a.Usuarios, func(o Usuario) bool { return o.Usuario == usuario }):
		r[CampoUsuario] = "já existe alguém com este usuário"
	}
	if _, err := ValidarNome(nome); err != nil {
		r[CampoNome] = err.Error()
	}
	// Ninguém a excetuar: a pessoa nova ainda não tem registro. Excetuar o
	// login proposto deixaria passar o e-mail de quem já tem esse login.
	if m := recusaDoEmail(a, email, ""); m != "" {
		r[CampoEmail] = m
	}
	return r
}

// recusaDoEmail é a régua do e-mail da pessoa exceto, que é dela ou vai ser:
// a mensagem da recusa, ou "" se o e-mail vale. Vazio vale.
func recusaDoEmail(a *Arquivo, email, exceto string) string {
	email, err := ValidarEmail(email)
	switch {
	case err != nil:
		return err.Error()
	case email != "" && emailEmUso(a, email, exceto):
		return "já existe alguém com este e-mail"
	}
	return ""
}

// ConferirNovo diz o que o Criar recusaria com estes dados, sem gravar nada:
// é a conferência da tela enquanto o admin preenche o formulário. Vazia é
// "tudo certo". Com o arquivo ilegível vem o erro, como no Alterar: o
// cadastro em memória está vazio, e "tudo certo" seria mentira.
func (c *Cadastro) ConferirNovo(usuario, nome, email string) (Recusas, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ilegivel != nil {
		return nil, c.ilegivel
	}
	return c.recusasDoNovo(&c.arq, usuario, nome, email), nil
}

// ConferirEmail diz o que o TrocarEmail recusaria com este e-mail, sem gravar
// nada. ErrNaoExiste se a pessoa não está no cadastro; com o arquivo
// ilegível, o erro dele, como no ConferirNovo.
func (c *Cadastro) ConferirEmail(usuario, email string) (Recusas, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ilegivel != nil {
		return nil, c.ilegivel
	}
	if c.indice(&c.arq, usuario) < 0 {
		return nil, ErrNaoExiste
	}
	r := Recusas{}
	if m := recusaDoEmail(&c.arq, email, usuario); m != "" {
		r[CampoEmail] = m
	}
	return r, nil
}

// Criar cadastra um viewer, ainda sem senha, e devolve o link para ela
// definir a sua. O e-mail é opcional: vazio, o link só vai pela tela. A
// recusa vem como Recusas, campo a campo.
func (c *Cadastro) Criar(usuario, nome, email string) (Convite, error) {
	// O que o ValidarNome e o ValidarEmail devolvem quando aceitam.
	nome, email = strings.TrimSpace(nome), strings.TrimSpace(email)
	token, link, err := c.novoLink()
	if err != nil {
		return Convite{}, err
	}
	u := Usuario{Usuario: usuario, Nome: nome, Email: email, Papel: PapelViewer, Link: link,
		CriadoEm: c.agora().Truncate(time.Second)}

	err = c.Alterar(func(a *Arquivo) error {
		if r := c.recusasDoNovo(a, usuario, nome, email); len(r) > 0 {
			return r
		}
		a.Usuarios = append(a.Usuarios, u)
		return nil
	})
	if err != nil {
		return Convite{}, err
	}
	return Convite{Usuario: u, Token: token}, nil
}

// TrocarEmail troca o e-mail da pessoa, ou o apaga, com email vazio. Não
// mexe no link nem na senha. A recusa vem como Recusas, no CampoEmail.
func (c *Cadastro) TrocarEmail(usuario, email string) (Usuario, error) {
	email = strings.TrimSpace(email) // o que o ValidarEmail devolve quando aceita
	var u Usuario
	err := c.Alterar(func(a *Arquivo) error {
		i := c.indice(a, usuario)
		if i < 0 {
			return ErrNaoExiste
		}
		if m := recusaDoEmail(a, email, usuario); m != "" {
			return Recusas{CampoEmail: m}
		}
		a.Usuarios[i].Email = email
		u = a.Usuarios[i]
		return nil
	})
	return u, err
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
		a.Usuarios[i].AtivoDesde = time.Time{}
		a.Usuarios[i].Link = link
		u = a.Usuarios[i]
		return nil
	})
	if err != nil {
		return Convite{}, err
	}
	return Convite{Usuario: u, Token: token}, nil
}

// Remover apaga a pessoa do cadastro, e a foto dela junto.
func (c *Cadastro) Remover(usuario string) error {
	var avatar string
	err := c.Alterar(func(a *Arquivo) error {
		i := c.indice(a, usuario)
		if i < 0 {
			return ErrNaoExiste
		}
		avatar = a.Usuarios[i].Avatar
		a.Usuarios = slices.Delete(a.Usuarios, i, i+1)
		return nil
	})
	if err == nil {
		c.apagarAvatar(avatar)
	}
	return err
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
		a.Usuarios[i].AtivoDesde = c.agora().Truncate(time.Second)
		a.Usuarios[i].Link = nil
		u = a.Usuarios[i]
		return nil
	})
	return u, err
}
