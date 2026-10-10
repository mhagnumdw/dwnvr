package usuarios

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Algoritmo é o contrato de um jeito de guardar senha, o que em Java seria uma
// interface. O valor guardado segue o formato de texto do PHC, o mesmo do
// `$argon2id$v=19$...` de outras ferramentas: `$<nome>$<parâmetros>`, e o nome
// diz qual Algoritmo o confere.
type Algoritmo interface {
	// Gerar devolve o valor a guardar para a senha, já com o nome e os
	// parâmetros, e com um salt novo: duas chamadas com a mesma senha dão
	// valores diferentes.
	Gerar(senha string) (string, error)

	// Conferir diz se a senha bate com o valor guardado. Erro é valor
	// malformado, e não senha errada.
	Conferir(senha, guardado string) (bool, error)
}

// algoritmos liga o nome no valor guardado à implementação. Um algoritmo novo
// (o Argon2id, se o golang.org/x/crypto um dia entrar no projeto) é uma linha
// aqui; trocar o algoritmoPadrao passa a valer para as senhas definidas dali em
// diante, e as antigas continuam sendo conferidas pelo nome delas.
var algoritmos = map[string]Algoritmo{
	nomePBKDF2: pbkdf2SHA256{},
}

const algoritmoPadrao = nomePBKDF2

// vez é a fila das contas de senha: uma por vez no processo inteiro. Cada uma
// prende um núcleo por 0,77 s no Orange Pi, e o login não limita tentativas;
// sem a fila, algumas tentativas simultâneas ocupariam os núcleos da gravação
// e do detector de objetos. Com ela, o pior caso é um núcleo ocupado e o
// login dos outros esperando a vez.
var vez = make(chan struct{}, 1)

// naVez roda f quando chegar a vez. Quem desistir antes (o navegador fechou a
// conexão) sai da fila sem gastar a conta.
func naVez(ctx context.Context, f func()) error {
	select {
	case vez <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-vez }()
	f()
	return nil
}

// GerarSenha devolve o valor a guardar para a senha, com o algoritmo padrão,
// esperando a vez na fila.
func GerarSenha(ctx context.Context, senha string) (guardado string, err error) {
	if errFila := naVez(ctx, func() {
		guardado, err = algoritmos[algoritmoPadrao].Gerar(senha)
	}); errFila != nil {
		return "", errFila
	}
	return guardado, err
}

// ConferirSenha diz se a senha bate com o valor guardado, esperando a vez na
// fila.
//
// Guardado vazio é "não há com o que comparar": o usuário não existe, ou ainda
// não definiu a senha. A conta é feita do mesmo jeito, contra um valor que
// nenhuma senha alcança, e a resposta é false. Assim o tempo de resposta do
// login não diz quem existe. Um valor com algoritmo desconhecido também paga a
// conta antes de virar erro.
func ConferirSenha(ctx context.Context, senha, guardado string) (ok bool, err error) {
	alg, errNome := algoritmoDe(guardado)
	contra := guardado
	if errNome != nil {
		alg, contra = algoritmos[algoritmoPadrao], semSenha
	}
	if errFila := naVez(ctx, func() {
		ok, err = alg.Conferir(senha, contra)
	}); errFila != nil {
		return false, errFila
	}
	switch {
	case guardado == "":
		return false, nil
	case errNome != nil:
		return false, errNome
	}
	return ok, err
}

// algoritmoDe acha, pelo nome no começo do valor guardado, quem o confere.
func algoritmoDe(guardado string) (Algoritmo, error) {
	partes := strings.SplitN(guardado, "$", 3)
	if len(partes) < 3 || partes[0] != "" {
		return nil, errors.New("senha guardada fora do formato $<algoritmo>$<parâmetros>")
	}
	alg, ok := algoritmos[partes[1]]
	if !ok {
		// Sem citar o que está guardado: estes erros vão para o log, e um
		// pedaço do valor guardado não tem o que fazer lá.
		return nil, errors.New("senha guardada com algoritmo desconhecido")
	}
	return alg, nil
}

// --- PBKDF2-HMAC-SHA256 -----------------------------------------------------

const nomePBKDF2 = "pbkdf2-sha256"

// pbkdf2SHA256 guarda a senha como
// `$pbkdf2-sha256$i=<iterações>$<salt>$<hash>`, com o salt e o hash em base64
// sem padding. É o quarto da lista da OWASP, escolhido por vir na biblioteca
// padrão: nenhuma dependência nova (ver docs/TODO/TODO_hash-da-senha.md).
type pbkdf2SHA256 struct{}

var b64 = base64.RawStdEncoding

// semSenha é o valor contra o qual ConferirSenha calcula quando não há senha
// guardada. Tem o mesmo custo de uma senha de verdade, e o hash zerado não é
// alcançado por senha nenhuma na prática.
var semSenha = fmt.Sprintf("$%s$i=%d$%s$%s", nomePBKDF2, IteracoesPBKDF2,
	b64.EncodeToString(make([]byte, BytesDoSalt)), b64.EncodeToString(make([]byte, BytesDoHash)))

func (pbkdf2SHA256) Gerar(senha string) (string, error) {
	salt := make([]byte, BytesDoSalt)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash, err := pbkdf2.Key(sha256.New, senha, salt, IteracoesPBKDF2, BytesDoHash)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("$%s$i=%d$%s$%s", nomePBKDF2, IteracoesPBKDF2,
		b64.EncodeToString(salt), b64.EncodeToString(hash)), nil
}

func (pbkdf2SHA256) Conferir(senha, guardado string) (bool, error) {
	// "", "pbkdf2-sha256", "i=600000", salt, hash
	partes := strings.Split(guardado, "$")
	if len(partes) != 5 || partes[0] != "" || partes[1] != nomePBKDF2 {
		return false, errors.New("senha guardada fora do formato $pbkdf2-sha256$i=<n>$<salt>$<hash>")
	}
	texto, ok := strings.CutPrefix(partes[2], "i=")
	if !ok {
		return false, errors.New("senha guardada sem as iterações (i=)")
	}
	iteracoes, err := strconv.Atoi(texto)
	if err != nil || iteracoes < IteracoesMinimas || iteracoes > IteracoesMaximas {
		return false, fmt.Errorf("senha guardada com iterações fora de %d a %d",
			IteracoesMinimas, IteracoesMaximas)
	}
	salt, err := b64.DecodeString(partes[3])
	if err != nil || len(salt) == 0 {
		return false, errors.New("senha guardada com salt ilegível")
	}
	quer, err := b64.DecodeString(partes[4])
	if err != nil || len(quer) != BytesDoHash {
		return false, errors.New("senha guardada com hash ilegível")
	}

	tem, err := pbkdf2.Key(sha256.New, senha, salt, iteracoes, len(quer))
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(tem, quer) == 1, nil
}
