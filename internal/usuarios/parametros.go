package usuarios

// Os números do cadastro de usuários. Trocar um deles é trocar a linha aqui.
const (
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
)
