// Package cmd содержит команды Cobra приложения fdbtable.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd — корневая команда приложения.
var rootCmd = &cobra.Command{
	Use:   "fdbtable",
	Short: "Получение FDB таблиц с сетевых коммутаторов",
	Long: `fdbtable — утилита для получения FDB таблиц с сетевых коммутаторов.
`,
	// При запуске без подкоманды выводим справку.
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// Execute запускает корневую команду и завершает процесс при ошибке.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Глобальные флаги, доступные всем подкомандам.
	rootCmd.PersistentFlags().String("db", "database.bolt", "путь к файлу базы данных bolt")
}
