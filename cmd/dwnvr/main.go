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
	"syscall"
	"time"

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
	)
	flag.Parse()

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

	ret := retention.New(cfg, st, cams, log)
	go ret.Run(ctx)

	mgr := recorder.NewManager(cfg, client, st, log)
	mgr.Start(ctx, cams)

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
		Handler: api.New(cfg, st, client, mgr, cams, secret, log).Handler(),
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

	if cfg.Storage.RequireSeparateDisk {
		ok, err := retention.IsOnSeparateFilesystem(cfg.Storage.Root)
		if err != nil {
			return fmt.Errorf("verificando o sistema de arquivos do storage: %w", err)
		}
		if !ok {
			return fmt.Errorf("storage.root %q está no mesmo sistema de arquivos da raiz "+
				"(o disco externo está desmontado?); gravar aqui encheria o rootfs. "+
				"Se a intenção é gravar no disco do sistema, desligue storage.requireSeparateDisk",
				cfg.Storage.Root)
		}
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
