package cmd

import (
	"log/slog"
	"os"

	"github.com/kirsrus/fdbtable/internal/colorlog"
	"github.com/spf13/cobra"
)

// copyright — копирайт приложения.
const copyright = "Copyright (c) 2026 Sterlikov Kirill"

// serveCmd — команда запуска сервера.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Запуск сервера",
	Run: func(cmd *cobra.Command, args []string) {
		// Инициализация цветного логирования.
		slog.SetDefault(slog.New(colorlog.New(os.Stderr, slog.LevelDebug)))

		// Информация о программе.
		slog.Info("fdbtable", "copyright", copyright, "version", version)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
