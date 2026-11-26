// Package cli implementa los comandos de flowsint-desk con cobra.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

var rootCmd = &cobra.Command{
	Use:   "flowsint-desk",
	Short: "Flowsint de escritorio, efímero en tmpfs, con copias cifradas (restic) en pCloud",
	Long: `flowsint-desk levanta Flowsint (OSINT de grafos) en Docker rootless con todo el
estado en tmpfs: al cerrar, nada queda en claro en el disco local. La persistencia
son snapshots cifrados con restic en una carpeta de pCloud, protegidos solo por la
frase de paso.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// HARDRULE: nunca como root. Como root se podría tocar el Docker
		// rootful del sistema y los datos dejarían de estar bajo el usuario.
		if os.Geteuid() == 0 {
			fmt.Fprintln(os.Stderr, "error: flowsint-desk NUNCA se ejecuta como root (prohibido sudo): la CLI se niega a arrancar con euid=0")
			os.Exit(1)
		}
		// ~/.local/bin delante del PATH del proceso: ahí instala `setup` restic
		// y los binarios rootless, y muchos shells (fish) no lo traen en el
		// PATH. Sin esto doctor seguía en rojo tras instalarlos.
		if home, err := os.UserHomeDir(); err == nil {
			_ = os.Setenv("PATH", filepath.Join(home, ".local", "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
		// endurecimiento obligatorio: umask restrictiva y sin cores
		syscall.Umask(0o077)
		_ = unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0})
	},
	SilenceUsage: true,
}

// Execute arranca la CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(doctorCmd, setupCmd, initCmd, upCmd, downCmd, statusCmd,
		backupCmd, snapshotsCmd, unlockCmd, forgetCmd, passwdCmd, logsCmd,
		verifyCmd, versionCmd)
}
