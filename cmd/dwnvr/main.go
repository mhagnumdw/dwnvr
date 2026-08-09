// Command dwnvr é um NVR de gravação contínua para hardware modesto.
//
// Ele consome o fMP4 que o go2rtc já produz, corta em segmentos alinhados a
// keyframe e indexa em arquivos NDJSON. Não decodifica, não transcodifica e não
// usa banco de dados.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	// A base de fusos horários vai embutida no binário. O dwnvr usa hora local
	// para decidir a que dia um segmento pertence, e numa imagem FROM scratch
	// não existe /usr/share/zoneinfo — sem isto, TZ seria silenciosamente
	// ignorado e todos os dias virariam UTC.
	_ "time/tzdata"

	"github.com/mhagnumdw/dwnvr/internal/api"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/fmp4"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
	"github.com/mhagnumdw/dwnvr/internal/retention"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

func main() {
	var (
		cfgPath = flag.String("config", "/etc/dwnvr/dwnvr.yaml", "caminho do dwnvr.yaml")
		debug   = flag.Bool("debug", false, "log em nível debug")
		health  = flag.Bool("healthcheck", false, "consulta o dwnvr local e sai com 0 se estiver saudável")
	)
	flag.Parse()

	// O healthcheck é o próprio binário porque a imagem é FROM scratch: não há
	// shell nem curl lá dentro para o HEALTHCHECK do Docker chamar.
	if *health {
		os.Exit(healthcheck(*cfgPath))
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if err := run(log, *cfgPath); err != nil {
		log.Error("dwnvr encerrou com erro", "erro", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	cams, err := cfg.LoadCameras()
	if err != nil {
		return err
	}
	if len(cams) == 0 {
		log.Warn("nenhuma câmera cadastrada", "arquivo", cfg.CamerasPath())
	}

	if err := prepareStorage(log, cfg); err != nil {
		return err
	}

	st := store.New(cfg.Storage.Root)
	client := go2rtc.New(cfg.Go2RTC)

	// Carrega o índice do disco. O dia mais recente é conferido contra os
	// arquivos: é ali que uma queda deixa estrago, e conferir só ele evita
	// varrer centenas de milhares de arquivos a cada boot.
	start := time.Now()
	for _, cam := range cams {
		idx := st.Camera(cam.ID)
		if err := idx.Scan(true, probeSegment); err != nil {
			return fmt.Errorf("lendo índice de %s: %w", cam.ID, err)
		}
		days := idx.Days()
		log.Info("índice carregado", "cam", cam.ID,
			"dias", len(days), "mb", idx.TotalBytes()>>20)
	}
	log.Info("índice pronto", "em", time.Since(start).Round(time.Millisecond))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mgr := recorder.NewManager(cfg, client, st, log)
	mgr.Start(ctx, cams)

	// A retenção lê a lista viva do gerenciador: uma cota alterada pela tela de
	// cadastro precisa valer na passada seguinte, não só após reiniciar.
	ret := retention.New(cfg, st, mgr.Cameras, log)
	go ret.Run(ctx)

	secret, err := cfg.SessionSecret()
	if err != nil {
		return fmt.Errorf("preparando o segredo de sessão: %w", err)
	}
	if !cfg.Server.AuthEnabled() {
		log.Warn("autenticação desligada: qualquer um na rede vê as gravações de todas as câmeras",
			"como_ligar", "defina server.username e server.password em "+cfgPath)
	}

	srv := &http.Server{
		Addr:    cfg.Server.Listen,
		Handler: api.New(cfg, st, client, mgr, secret, log).Handler(),
		// Sem WriteTimeout: exportação e proxy de live são respostas longas por
		// natureza, e um teto aqui as cortaria no meio.
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("dwnvr no ar", "http", cfg.Server.Listen,
			"storage", cfg.Storage.Root, "go2rtc", cfg.Go2RTC.URL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("servidor HTTP caiu", "erro", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("encerrando; fechando segmentos em aberto")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	// Stop espera cada segmento em aberto ser fechado e indexado. Sem isso,
	// todo reinício perderia o último minuto de cada câmera.
	mgr.Stop()
	log.Info("encerrado")
	return nil
}

// prepareStorage valida o destino das gravações antes de qualquer escrita.
func prepareStorage(log *slog.Logger, cfg *config.Config) error {
	if err := os.MkdirAll(cfg.Storage.Root, 0o755); err != nil {
		return fmt.Errorf("criando %s: %w", cfg.Storage.Root, err)
	}

	free, err := retention.FreeBytes(cfg.Storage.Root)
	if err != nil {
		return err
	}
	total, err := retention.TotalBytes(cfg.Storage.Root)
	if err != nil {
		return err
	}
	log.Info("storage", "caminho", cfg.Storage.Root,
		"livre_gb", free>>30, "total_gb", total>>30, "min_livre_mb", cfg.Storage.MinFreeMB)

	if free < cfg.Storage.MinFreeMB<<20 {
		log.Warn("disco já abaixo do mínimo livre; a retenção vai evictar gravações antigas",
			"livre_mb", free>>20, "minimo_mb", cfg.Storage.MinFreeMB)
	}
	return nil
}

// probeSegment reconstrói a entrada de índice de um segmento órfão — gravado
// por completo, mas cuja linha de índice não chegou a ser escrita antes de uma
// queda.
func probeSegment(path string) (store.Entry, error) {
	info, err := fmp4.ProbeSegment(path)
	if err != nil {
		return store.Entry{}, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return store.Entry{}, err
	}
	return store.Entry{
		DurMs:     info.DurationMs,
		Size:      fi.Size(),
		Gen:       info.Gen,
		InitSize:  info.InitSize,
		FirstFrag: info.FirstFragSize,
	}, nil
}

// healthcheck consulta a instância local e devolve o código de saída para o
// HEALTHCHECK do Docker.
func healthcheck(cfgPath string) int {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck: configuração ilegível:", err)
		return 1
	}

	// listen costuma ser ":8080"; o healthcheck fala com o loopback.
	addr := cfg.Server.Listen
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/api/session")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: HTTP %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
