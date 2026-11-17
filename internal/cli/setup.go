package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// setupCmd = doctor --fix + resumen final solo de lo pendiente (requiere sudo).
var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Instala automáticamente lo que no requiera sudo y lista los pasos pendientes",
	Long: `Equivale a "doctor --fix": instala en ~/.local/bin lo instalable sin sudo
(restic verificado por sha256, rootlesskit, slirp4netns, fuse-overlayfs,
dockerd-rootless.sh), crea la caché de imágenes y config.toml, y al final imprime
 SOLO los pasos que quedan pendientes y que sí requieren acción manual
(normalmente sudo una vez para /etc/subuid y /etc/subgid).
La CLI NUNCA ejecuta sudo: los comandos pendientes se muestran para copiarlos.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		pendientes, _ := runDoctor(true)
		if len(pendientes) == 0 {
			fmt.Println("\n✔ nada pendiente: el entorno quedó listo (los avisos ⚠ no bloquean).")
			return nil
		}
		fmt.Println("\n→ pendientes que requieren acción manual (cópialos y ejecútalos tú; la CLI nunca usa sudo):")
		for _, c := range pendientes {
			fmt.Printf("  ✖ %s\n     %s\n", c.name, c.fix)
		}
		return nil
	},
}
