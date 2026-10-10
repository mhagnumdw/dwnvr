package usuarios

import "context"

// A Minha conta: o que cada pessoa muda de si mesma. O nome e a foto valem
// também para o dono; a senha dele continua no dwnvr.yaml.

// TrocarSenha troca a senha de quem já tem uma, pedindo a atual. As sessões
// da pessoa caem, porque a chave do cookie sai da senha guardada (ver
// api.chaveDaSessao): quem trocou recebe um cookie novo, e os outros
// aparelhos voltam para a tela de login.
//
// São duas contas de senha, uma para conferir a atual e outra para gerar a
// nova, cada uma esperando a vez na fila (ver GerarSenha).
func (c *Cadastro) TrocarSenha(ctx context.Context, usuario, atual, nova string) (Usuario, error) {
	u, ok := c.Buscar(usuario)
	if !ok || u.Senha == "" {
		return Usuario{}, ErrNaoExiste
	}
	if err := ValidarSenha(nova); err != nil {
		return Usuario{}, err
	}
	confere, err := ConferirSenha(ctx, atual, u.Senha)
	if err != nil {
		return Usuario{}, err
	}
	if !confere {
		return Usuario{}, Recusado("a senha atual não confere")
	}
	guardado, err := GerarSenha(ctx, nova)
	if err != nil {
		return Usuario{}, err
	}
	err = c.Alterar(func(a *Arquivo) error {
		i := c.indice(a, usuario)
		if i < 0 {
			return ErrNaoExiste
		}
		// O admin gerou um link novo, ou outro aparelho trocou a senha,
		// durante a conta: a senha conferida não é mais a que vale.
		if a.Usuarios[i].Senha != u.Senha {
			return Recusado("a senha mudou enquanto isso: entre de novo")
		}
		a.Usuarios[i].Senha = guardado
		u = a.Usuarios[i]
		return nil
	})
	return u, err
}

// MudarNome troca o nome de exibição da pessoa, ou o do dono, e devolve o
// nome como ficou.
func (c *Cadastro) MudarNome(usuario, nome string) (string, error) {
	nome, err := ValidarNome(nome)
	if err != nil {
		return "", err
	}
	return nome, c.Alterar(func(a *Arquivo) error {
		if usuario == c.dono {
			a.Dono.Nome = nome
			return nil
		}
		i := c.indice(a, usuario)
		if i < 0 {
			return ErrNaoExiste
		}
		a.Usuarios[i].Nome = nome
		return nil
	})
}
