package config

import (
	"crypto/rand"
	"fmt"
	"slices"
	"strings"

	"github.com/mhagnumdw/dwnvr/internal/area"
)

// Area é uma área desenhada sobre a imagem da câmera, com o nome que a pessoa
// deu. O polígono e as regras são do pacote area; aqui fica o que o cadastro
// guarda.
type Area struct {
	// ID é curto e gerado pelo servidor quando a área chega sem ele. O link de
	// Detecções aponta para ele, e renomear a área não o quebra.
	ID   string `json:"id"`
	Name string `json:"name"`

	// Rule é a `touch` ou a `foot`. Ausente, é a area.RegraDaAreaPadrao.
	Rule   string       `json:"rule,omitempty"`
	Points []area.Ponto `json:"points"`
}

// tamanhoDoIDDaArea são 6 letras e dígitos: 36^6 são ~2 bilhões, e a conta só
// precisa não repetir dentro de uma câmera, com no máximo area.MaxAreasPorCamera.
const tamanhoDoIDDaArea = 6

const alfabetoDoIDDaArea = "abcdefghijklmnopqrstuvwxyz0123456789"

// ComIDsNasAreas devolve a câmera com um ID em cada área que chegou sem ele.
// Não toca no ID que já existe, nem no repetido: quem confere é o
// ConfereCamera, e uma área não pode trocar de ID escondida.
func (cam Camera) ComIDsNasAreas() Camera {
	if !slices.ContainsFunc(cam.Areas, func(a Area) bool { return a.ID == "" }) {
		return cam
	}
	usados := make(map[string]bool, len(cam.Areas))
	for _, a := range cam.Areas {
		usados[a.ID] = true
	}
	novas := slices.Clone(cam.Areas)
	for i := range novas {
		if novas[i].ID != "" {
			continue
		}
		for {
			id := novoIDDaArea()
			if !usados[id] {
				usados[id] = true
				novas[i].ID = id
				break
			}
		}
	}
	cam.Areas = novas
	return cam
}

func novoIDDaArea() string {
	b := make([]byte, tamanhoDoIDDaArea)
	if _, err := rand.Read(b); err != nil {
		// O crypto/rand só falha se o sistema não tem entropia, e então nada
		// mais no dwnvr funciona também (a sessão usa o mesmo).
		panic(fmt.Errorf("sem aleatoriedade para o ID da área: %w", err))
	}
	for i, v := range b {
		b[i] = alfabetoDoIDDaArea[int(v)%len(alfabetoDoIDDaArea)]
	}
	return string(b)
}

// confereAreas devolve só as áreas válidas e um Aviso por área tirada. A
// primeira de um nome, ou de um ID, vale; a repetida sai.
func confereAreas(cam Camera, av *[]Aviso) []Area {
	if len(cam.Areas) == 0 {
		return cam.Areas
	}
	ficam := make([]Area, 0, len(cam.Areas))
	nomes := map[string]bool{}
	ids := map[string]bool{}
	recusa := func(i int, a Area, aceito string) {
		*av = append(*av, Aviso{Camera: cam.ID, Campo: fmt.Sprintf("areas[%d]", i),
			Valor: fmt.Sprintf("%q", a.Name), Aceito: aceito, Usando: "a área fica de fora"})
	}
	for i, a := range cam.Areas {
		nome := strings.ToLower(strings.TrimSpace(a.Name))
		_, errRegra := area.NovaRegra(a.Rule)
		_, errPontos := area.Nova(a.Points)
		switch {
		case len(ficam) >= area.MaxAreasPorCamera:
			recusa(i, a, fmt.Sprintf("até %d áreas por câmera", area.MaxAreasPorCamera))
		case nome == "":
			recusa(i, a, "um nome, que não fique vazio")
		case nomes[nome]:
			recusa(i, a, "um nome que não se repita na câmera")
		case !idDaAreaValido(a.ID):
			recusa(i, a, fmt.Sprintf("um id de 1 a %d letras minúsculas e dígitos", maxIDDaArea))
		case ids[a.ID]:
			recusa(i, a, "um id que não se repita na câmera")
		case errRegra != nil:
			recusa(i, a, "a regra "+regrasAceitas())
		case errPontos != nil:
			recusa(i, a, fmt.Sprintf("de %d a %d pontos, com as coordenadas de 0 a 1",
				area.MinPontosDaArea, area.MaxPontosDaArea))
		default:
			nomes[nome] = true
			ids[a.ID] = true
			ficam = append(ficam, a)
		}
	}
	return ficam
}

// maxIDDaArea dá folga ao id escrito à mão no cameras.json; o gerado tem
// tamanhoDoIDDaArea.
const maxIDDaArea = 16

// idDaAreaValido aceita o que cabe sem escape na URL de Detecções
// (`areas=<cam>/<id>`): só letras minúsculas e dígitos.
func idDaAreaValido(id string) bool {
	if id == "" || len(id) > maxIDDaArea {
		return false
	}
	return strings.Trim(id, alfabetoDoIDDaArea) == ""
}

func regrasAceitas() string {
	nomes := make([]string, 0, len(area.Regras))
	for n := range area.Regras {
		nomes = append(nomes, n)
	}
	slices.Sort(nomes)
	return strings.Join(nomes, " ou ")
}
