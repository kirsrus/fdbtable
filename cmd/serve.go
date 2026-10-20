package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/kirsrus/fdbtable/internal/colorlog"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
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

		return errors.WithStack(fmt.Errorf("тестовая ошибка"))
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
