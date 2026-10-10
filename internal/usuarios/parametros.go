package usuarios

import "time"

// Os números do cadastro de usuários. Trocar um deles é trocar a linha aqui.
const (
	// ValidadeDoLink é quanto o link de convite vale depois de gerado. Curta
	// de propósito: a ideia é gerar o link conversando com a pessoa, que o
	// abre na hora. Vencido, o admin gera outro.
	ValidadeDoLink = 10 * time.Minute

	// BytesDoToken é o tamanho do token do link, do crypto/rand: 256 bits não
	// se adivinham, então o link não precisa de limite de tentativas.
	BytesDoToken = 32

	// SenhaMinima e SenhaMaxima são os limites da senha, em caracteres, e não
	// há regra de composição: é o que pede o NIST SP 800-63B. O teto só
	// existe para o corpo da requisição ter tamanho conhecido; o NIST pede
	// aceitar pelo menos 64.
	SenhaMinima = 8
	SenhaMaxima = 128

	// TamanhoMaximoDoNome é o teto do nome de exibição, em caracteres.
	TamanhoMaximoDoNome = 60

	// IteracoesPBKDF2 é o custo de cada senha: 600.000 é o número da OWASP
	// para o PBKDF2-HMAC-SHA256, e dá 0,77 s por login num Orange Pi Zero 3
	// (medido em 07/10/2026, ver docs/TODO/TODO_hash-da-senha.md). As
	// iterações vão no valor guardado: subir este número depois não invalida
	// as senhas que já existem, que continuam com o número de quando foram
	// definidas.
	IteracoesPBKDF2 = 600_000

	// IteracoesMinimas e IteracoesMaximas são a faixa aceita ao ler um valor
	// guardado. Abaixo, a senha estaria protegida de menos; acima, um valor
	// estragado no arquivo prenderia um núcleo por minutos a cada login.
	IteracoesMinimas = 100_000
	IteracoesMaximas = 10_000_000

	// BytesDoSalt é o tamanho do salt, do crypto/rand, um por senha.
	BytesDoSalt = 16

	// BytesDoHash é o tamanho do hash guardado: o do SHA-256.
	BytesDoHash = 32

	// TamanhoMaximoDoUsuario é o teto do nome de login.
	TamanhoMaximoDoUsuario = 32

	// LadoDoAvatar é o lado, em pixels, do quadrado que o navegador corta e
	// reduz da foto escolhida. 256 dá nitidez ao maior desenho do avatar na
	// tela (88 px) em celular de densidade 3.
	LadoDoAvatar = 256

	// TetoDoAvatar é o maior JPEG aceito, em bytes, já reduzido. Na
	// QualidadeDoAvatar, as fotos medidas em 09/10/2026 deram de 10 a 23 KB;
	// ruído puro, o pior caso, deu 29 KB na qualidade 80.
	TetoDoAvatar = 32 << 10

	// QualidadeDoAvatar é a qualidade do JPEG que o navegador gera, de 0 a
	// 100. Ele só baixa a qualidade se o resultado passar do TetoDoAvatar.
	QualidadeDoAvatar = 92

	// BytesDoIdDoAvatar é o tamanho do nome do arquivo do avatar, do
	// crypto/rand. O nome muda a cada troca e vai na URL, o que deixa o
	// navegador guardar a imagem para sempre.
	BytesDoIdDoAvatar = 8
)
