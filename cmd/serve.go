package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kirsrus/fdbtable/internal/colorlog"
	"github.com/kirsrus/fdbtable/internal/workers"
	"github.com/kirsrus/fdbtable/internal/workers/collector"
	"github.com/kirsrus/fdbtable/internal/workers/web"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/sync/errgroup"
)

// copyright — копирайт приложения.
const copyright = "Copyright (c) 2026 Sterlikov Kirill"

// serveCmd — команда запуска сервера.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Запуск сервера",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Уровень логирования из флага --level.
		levelStr, err := cmd.Flags().GetString("level")
		if err != nil {
			return errors.WithStack(err)
		}

		// Уровень логирования по умолчанию — info.
		level := slog.LevelInfo
		switch levelStr {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		default:
			return errors.WithStack(fmt.Errorf("неизвестный уровень логирования %q: допустимые значения debug|info|warn|error", levelStr))
		}

		// Инициализация цветного логирования.
		slog.SetDefault(slog.New(colorlog.New(os.Stderr, level)))

		// Информация о программе.
		slog.Info("<--- fdbtable стартовал", "copyright", copyright, "version", version)

		// Путь к файлу базы данных из флага --db.
		dbPath, err := cmd.Flags().GetString("db")
		if err != nil {
			return errors.WithStack(err)
		}

		// Инициализация базы данных bbolt.
		db, err := bolt.Open(dbPath, 0600, nil)
		if err != nil {
			return errors.WithStack(fmt.Errorf("не удалось открыть базу данных %q: %w", dbPath, err))
		}
		defer db.Close()
		slog.Info("открыта база данных", slog.String("database", dbPath))

		// Порт web-сервера из флага --port.
		port, err := cmd.Flags().GetInt("port")
		if err != nil {
			return errors.WithStack(err)
		}

		// Контекст с отменой для управления жизненным циклом воркеров.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Группа воркеров: если один вернёт ошибку, остальные будут остановлены.
		g, gctx := errgroup.WithContext(ctx)

		// Канал команд на сбор информации с коммутаторов.
		// Web-воркер пишет в него (на запись), воркер collector читает (на чтение).
		// По каналу передаётся полная информация о коммутаторе (SwInfo).
		collectCh := make(chan web.SwInfo, 10)

		// Список воркеров приложения.
		workersList := []workers.Worker{
			// Воркер web-сервера. Пробрасываем порт, базу данных, логгер, версию
			// и канал команд на сбор информации (на запись).
			web.New(port, db, slog.Default(), version, collectCh),
			// Воркер сбора информации с коммутаторов. Пробрасываем базу данных,
			// логгер и канал команд на сбор информации (на чтение).
			collector.New(db, slog.Default(), collectCh),
		}

		// Запуск всех воркеров.
		for _, w := range workersList {
			w := w
			g.Go(func() error {
				return w.Run(gctx)
			})
		}

		// Ожидание сигналов от ОС для graceful shutdown.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sigCh)

		go func() {
			sig := <-sigCh
			slog.Info("получен сигнал завершения, останавливаем воркеры", slog.String("signal", sig.String()))
			cancel()
		}()

		// Ожидание завершения всех воркеров.
		if err := g.Wait(); err != nil {
			return errors.WithStack(err)
		}

		slog.Info("все воркеры завершили работу")
		return nil
	},
}

func init() {
	// Флаг пути к файлу базы данных.
	serveCmd.Flags().String("db", "database.bolt", "путь к файлу базы данных bolt")
	// Флаг порта web-сервера.
	serveCmd.Flags().Int("port", 8080, "порт web-сервера")
	// Флаг уровня логирования
	serveCmd.Flags().String("level", "info", "уровень логирования debug|info|warn|error")
	rootCmd.AddCommand(serveCmd)
}
