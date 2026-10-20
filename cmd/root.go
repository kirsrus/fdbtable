// Package cmd содержит команды Cobra приложения fdbtable.
package cmd

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// rootCmd — корневая команда приложения.
var rootCmd = &cobra.Command{
	Use:   "fdbtable",
	Short: "Получение FDB таблиц с сетевых коммутаторов",
	Long: `fdbtable — утилита для получения FDB таблиц с сетевых коммутаторов.
`,
	SilenceErrors: true, // Отключает вывод описния ошибок
	SilenceUsage:  true, // Отключает вывод текста "описание использования" при ошибке

	// При запуске без подкоманды выводим справку.
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// Execute запускает корневую команду и завершает процесс при ошибке.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		// Выводим трейс ошибки, если он был сохранён через github.com/pkg/errors

		if errWithStack, ok := err.(stackTracer); ok {
			trace := make([]string, 0)
			for _, s := range errWithStack.StackTrace() {
				d, f := filepath.Split(fmt.Sprintf("%+s", s))
				l := fmt.Sprintf("%s:%s", filepath.Join(filepath.Base(d), f), fmt.Sprintf("%d", s))
				l = strings.ReplaceAll(l, "\\", "/")
				trace = append(trace, l)
			}
			slog.Error(fmt.Sprintf("работа программы завершена с ошибкой: %v", err), slog.String("stacktrace", strings.Join(trace, " ")))
		} else {
			slog.Error(err.Error())
		}

		// os.Exit(1)
	}
}

func init() {
	// Глобальные флаги, доступные всем подкомандам.
	rootCmd.PersistentFlags().String("db", "database.bolt", "путь к файлу базы данных bolt")
}

type stackTracer interface {
	StackTrace() errors.StackTrace
}
