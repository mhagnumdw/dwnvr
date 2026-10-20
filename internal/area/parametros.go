package area

// Este arquivo guarda todos os números do pacote, cada um com o motivo ao
// lado.

const (
	// RegraDaAreaPadrao é a regra de quem não escolhe: a `foot` (pisa). Nos
	// casos reais de 16 a 20/10/2026, de 20% a 43% do que a `touch` mostrava
	// era gente que só aparecia por cima da área, perto da câmera, ou um falso
	// positivo parado na borda. O preço é desenhar a área cobrindo todo o chão
	// onde o pé pode estar.
	RegraDaAreaPadrao = "foot"

	// MinPontosDaArea é o triângulo: com menos, não há área.
	MinPontosDaArea = 3

	// MaxPontosDaArea dá folga a um contorno à mão pelo chão de um quintal, e
	// segura o tamanho: 20 pontos em milésimos são ~160 caracteres na URL de
	// Detecções, e oito áreas de 20 pontos ocupam ~3 KB dos 16 KB que o
	// POST /api/cameras lê.
	MaxPontosDaArea = 20

	// ToleranciaDaBorda é a distância abaixo da qual um ponto conta como sobre
	// a borda. As coordenadas chegam com 3 a 4 casas (milésimos na query, 4
	// casas no cadastro), e o erro do float64 nas contas fica na casa de 1e-16:
	// a tolerância só absorve esse erro, sem engordar a área.
	ToleranciaDaBorda = 1e-9
)
