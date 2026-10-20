package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version — версия приложения. Задаётся при сборке через -ldflags,
// например: go build -ldflags "-X github.com/kirsrus/fdbtable/cmd.version=1.0.0".
var version = "v0.0.0"

// versionCmd — команда вывода версии приложения.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Вывод версии приложения",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s\n", version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
