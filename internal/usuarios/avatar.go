package usuarios

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"regexp"
)

// O avatar é a foto da pessoa, um JPEG quadrado de até LadoDoAvatar pixels. A
// redução é do navegador: a foto original não sai do aparelho, e o servidor
// só confere e guarda os bytes como vieram, sem comprimir de novo.
//
// Cada foto é um arquivo avatares/<id>.jpg, e o usuarios.json guarda o id. O id
// muda a cada troca, o que deixa o navegador guardar a imagem para sempre:
// foto nova é URL nova.

var formatoDoIdDoAvatar = regexp.MustCompile(fmt.Sprintf(`^[0-9a-f]{%d}$`, 2*BytesDoIdDoAvatar))

// ConferirAvatar diz se os bytes são um avatar aceito: até TetoDoAvatar bytes,
// e um JPEG inteiro, quadrado, de até LadoDoAvatar pixels. Decodifica a
// imagem toda, e não só o cabeçalho, para um arquivo truncado ou que só
// começa como JPEG não passar.
func ConferirAvatar(b []byte) error {
	if len(b) > TetoDoAvatar {
		return Recusado(fmt.Sprintf("a foto passa de %d KB", TetoDoAvatar>>10))
	}
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		return Recusado("a foto não é um JPEG legível")
	}
	l := img.Bounds()
	if l.Dx() != l.Dy() || l.Dx() > LadoDoAvatar {
		return Recusado(fmt.Sprintf("a foto precisa ser quadrada, de até %d pixels", LadoDoAvatar))
	}
	return nil
}

// campoDoAvatar diz onde o arquivo guarda o avatar da pessoa, ou nil se ela
// não existe. O dono também tem avatar.
func (c *Cadastro) campoDoAvatar(a *Arquivo, usuario string) *string {
	if usuario == c.dono {
		return &a.Dono.Avatar
	}
	if i := c.indice(a, usuario); i >= 0 {
		return &a.Usuarios[i].Avatar
	}
	return nil
}

func (c *Cadastro) caminhoDoAvatar(id string) string {
	return filepath.Join(c.avatares, id+".jpg")
}

// apagarAvatar apaga o arquivo da foto. Falhar só deixa um arquivo sobrando,
// que ninguém alcança: o id saiu do usuarios.json.
func (c *Cadastro) apagarAvatar(id string) {
	if formatoDoIdDoAvatar.MatchString(id) {
		_ = os.Remove(c.caminhoDoAvatar(id))
	}
}

// TrocarAvatar guarda a foto da pessoa e devolve o id dela. A foto anterior é
// apagada.
func (c *Cadastro) TrocarAvatar(usuario string, b []byte) (string, error) {
	if err := ConferirAvatar(b); err != nil {
		return "", err
	}
	r := make([]byte, BytesDoIdDoAvatar)
	if _, err := rand.Read(r); err != nil {
		return "", err
	}
	id := hex.EncodeToString(r)

	// O arquivo vem antes do usuarios.json: um id gravado sem o arquivo seria
	// uma foto quebrada na tela; o arquivo sem o id é só um arquivo a mais.
	// 0700 e 0600 como o resto da pasta de config: são fotos de gente.
	if err := os.MkdirAll(c.avatares, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(c.caminhoDoAvatar(id), b, 0o600); err != nil {
		return "", err
	}
	var antigo string
	err := c.Alterar(func(a *Arquivo) error {
		campo := c.campoDoAvatar(a, usuario)
		if campo == nil {
			return ErrNaoExiste
		}
		antigo, *campo = *campo, id
		return nil
	})
	if err != nil {
		c.apagarAvatar(id)
		return "", err
	}
	c.apagarAvatar(antigo)
	return id, nil
}

// TirarAvatar apaga a foto da pessoa. Quem não tem foto fica como está.
func (c *Cadastro) TirarAvatar(usuario string) error {
	var antigo string
	err := c.Alterar(func(a *Arquivo) error {
		campo := c.campoDoAvatar(a, usuario)
		if campo == nil {
			return ErrNaoExiste
		}
		antigo, *campo = *campo, ""
		return nil
	})
	if err == nil {
		c.apagarAvatar(antigo)
	}
	return err
}

// ErrSemAvatar é o id que não é a foto de ninguém.
var ErrSemAvatar = errors.New("foto não encontrada")

// Avatar acha a foto pelo id: o caminho do arquivo e de quem ela é, para quem
// serve decidir se quem pede pode vê-la.
func (c *Cadastro) Avatar(id string) (caminho, usuario string, err error) {
	if !formatoDoIdDoAvatar.MatchString(id) {
		return "", "", ErrSemAvatar
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.arq.Dono.Avatar == id {
		return c.caminhoDoAvatar(id), c.dono, nil
	}
	vistos := map[string]bool{}
	for _, u := range c.arq.Usuarios {
		valida := c.motivoParaIgnorar(u, vistos) == ""
		vistos[u.Usuario] = true
		if valida && u.Avatar == id {
			return c.caminhoDoAvatar(id), u.Usuario, nil
		}
	}
	return "", "", ErrSemAvatar
}
