package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/kirsrus/fdbtable/internal/colorlog"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	bolt "go.etcd.io/bbolt"
)

// copyright — копирайт приложения.
const copyright = "Copyright (c) 2026 Sterlikov Kirill"

// serveCmd — команда запуска сервера.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Запуск сервера",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Инициализация цветного логирования.
		slog.SetDefault(slog.New(colorlog.New(os.Stderr, slog.LevelDebug)))

		// Информация о программе.
		slog.Info("fdbtable started", "copyright", copyright, "version", version)

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

		return nil
	},
}

func init() {
	// Флаг пути к файлу базы данных.
	serveCmd.Flags().String("db", "database.bolt", "путь к файлу базы данных bolt")
	rootCmd.AddCommand(serveCmd)
}
