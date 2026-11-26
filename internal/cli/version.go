package cli

import (
	"fmt"
	"runtime"

	"flowsint-desk/internal/snap"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Versión de flowsint-desk",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("flowsint-desk %s (%s, %s/%s)\n", snap.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return nil
	},
}
