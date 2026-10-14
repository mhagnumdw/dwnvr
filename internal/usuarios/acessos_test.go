package usuarios

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// acessosEm abre o acessos.json do diretório, com o relógio em *agora.
func acessosEm(t *testing.T, dir string, agora *time.Time) *Acessos {
	t.Helper()
	a, err := AbrirAcessos(filepath.Join(dir, "acessos.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	a.agora = func() time.Time { return *agora }
	// Os testes gravam pelo GravarPendente; o timer não pode se adiantar.
	a.espera = time.Hour
	return a
}

func TestUltimoAcessoAndaDePassoEmPasso(t *testing.T) {
	t0 := time.Date(2026, 10, 14, 8, 12, 0, 0, time.Local)
	agora := t0
	a := acessosEm(t, t.TempDir(), &agora)

	if got := a.Ultimo("maria"); !got.IsZero() {
		t.Fatalf("sem registro, veio %v", got)
	}
	a.Registrar("maria")

	for _, caso := range []struct {
		nome   string
		depois time.Duration
		quer   time.Time
	}{
		{"menos de um passo não mexe", 30 * time.Second, t0},
		{"um passo depois, anda", PassoDoUltimoAcesso, t0.Add(PassoDoUltimoAcesso)},
		// O relógio corrigido para trás regrava: senão o acesso ficaria no
		// futuro até o relógio alcançá-lo.
		{"relógio para trás, regrava", -time.Hour, t0.Add(-time.Hour)},
	} {
		agora = t0.Add(caso.depois)
		a.Registrar("maria")
		if got := a.Ultimo("maria"); !got.Equal(caso.quer) {
			t.Errorf("%s: último acesso %v, esperava %v", caso.nome, got, caso.quer)
		}
	}
}

func TestUltimoAcessoSobreviveAoReinicio(t *testing.T) {
	dir := t.TempDir()
	agora := time.Date(2026, 10, 14, 8, 12, 31, 500e6, time.Local)
	a := acessosEm(t, dir, &agora)
	a.Registrar("maria")
	a.Registrar("admin")
	if err := a.GravarPendente(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, "acessos.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("acessos.json com modo %v, esperava 0600", info.Mode().Perm())
	}

	de := acessosEm(t, dir, &agora)
	quer := agora.Truncate(time.Second)
	for _, u := range []string{"maria", "admin"} {
		if got := de.Ultimo(u); !got.Equal(quer) {
			t.Errorf("%s depois de reabrir: %v, esperava %v", u, got, quer)
		}
	}
}

// Sem acesso novo não há o que gravar: quem não usa o dwnvr, ou o usa com a
// autenticação desligada, não ganha arquivo nenhum.
func TestSemAcessoNaoGravaNada(t *testing.T) {
	dir := t.TempDir()
	agora := time.Now()
	a := acessosEm(t, dir, &agora)
	if err := a.GravarPendente(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "acessos.json")); !os.IsNotExist(err) {
		t.Errorf("acessos.json criado sem acesso nenhum (stat: %v)", err)
	}
}

// O Registrar não grava: quem grava é o timer, depois da espera.
func TestGravacaoFicaParaOTimer(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "acessos.json")
	agora := time.Now()
	a := acessosEm(t, dir, &agora)
	a.espera = 20 * time.Millisecond

	a.Registrar("maria")
	if _, err := os.Stat(caminho); !os.IsNotExist(err) {
		t.Fatalf("o Registrar gravou na hora (stat: %v)", err)
	}
	prazo := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(caminho); err == nil {
			break
		}
		if time.Now().After(prazo) {
			t.Fatal("o timer não gravou o acessos.json")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A gravação que falha agenda outra tentativa sozinha: sem isso, ela
// esperaria o próximo acesso, e sem acesso ficaria para o desligamento.
func TestGravacaoQueFalhaTentaDeNovo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config") // ainda não existe
	caminho := filepath.Join(dir, "acessos.json")
	agora := time.Now()
	a := acessosEm(t, dir, &agora)

	a.Registrar("maria")
	a.espera = 20 * time.Millisecond
	if err := a.GravarPendente(); err == nil {
		t.Fatal("gravou num diretório que não existe")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	prazo := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(caminho); err == nil {
			break
		}
		if time.Now().After(prazo) {
			t.Fatal("depois da falha, nenhuma tentativa gravou o acessos.json")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEsquecerApagaOAcesso(t *testing.T) {
	dir := t.TempDir()
	agora := time.Now()
	a := acessosEm(t, dir, &agora)
	a.Registrar("maria")
	if err := a.GravarPendente(); err != nil {
		t.Fatal(err)
	}
	a.Esquecer("maria")
	if err := a.GravarPendente(); err != nil {
		t.Fatal(err)
	}
	if got := acessosEm(t, dir, &agora).Ultimo("maria"); !got.IsZero() {
		t.Errorf("depois de esquecer e reabrir, veio %v", got)
	}
}

// Arquivo ilegível não impede nada: vem o erro, os acessos recomeçam vazios,
// e a próxima gravação o substitui.
func TestAcessosIlegiveisRecomecamVazios(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "acessos.json")
	if err := os.WriteFile(caminho, []byte("{não é json"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := AbrirAcessos(caminho, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("arquivo ilegível sem erro")
	}
	a.Registrar("maria")
	if err := a.GravarPendente(); err != nil {
		t.Fatal(err)
	}
	agora := time.Now()
	if got := acessosEm(t, dir, &agora).Ultimo("maria"); got.IsZero() {
		t.Error("a gravação não substituiu o arquivo ilegível")
	}
}
