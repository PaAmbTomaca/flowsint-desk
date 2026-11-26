// Package config gestiona ~/.config/flowsint-desk/config.toml.
// NUNCA almacena secretos: solo ruta del almacén, puerto, intervalos y retención.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"flowsint-desk/internal/dock"
	"flowsint-desk/internal/sockproxy"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Path            string `toml:"-"`
	VaultPath       string `toml:"vault_path"`        // carpeta del almacén restic (pCloud)
	Port            int    `toml:"port"`              // puerto local de la UI (127.0.0.1)
	SaveIntervalMin int    `toml:"save_interval_min"` // snapshot automático
	KeepHourly      int    `toml:"keep_hourly"`
	KeepDaily       int    `toml:"keep_daily"`
	KeepMonthly     int    `toml:"keep_monthly"`
	LastForget      string `toml:"last_forget"` // RFC3339 de la última ejecución de forget
	// DockerSocket: qué socket de Docker ven api/celery (para los enrichers):
	// "filtrado" (proxy que solo deja lanzar contenedores sin montajes ni
	// privilegios), "directo" (el socket rootless tal cual) o "no".
	DockerSocket string `toml:"docker_socket"`
	// FlowsintVersion: etiqueta de las imágenes ghcr.io/reconurge/flowsint-*
	// ("latest" o una versión concreta publicada por Flowsint).
	FlowsintVersion string `toml:"flowsint_version"`
	// Engine: motor de contenedores, "docker" (Docker rootless + compose) o
	// "podman" (Podman rootless, sin daemon). Se elige en `setup`; para cambiar
	// después: `flowsint-desk engine <otro>`.
	Engine string `toml:"engine"`
	// MirrorPath: segunda copia del almacén (otro repo restic, p. ej. en un
	// disco externo). Vacío = sin segunda copia. Se rellena con `mirror --to`.
	MirrorPath string `toml:"mirror_path"`
	// VerifyEveryDays: cada cuántos días `up` comprueba la integridad del
	// almacén (restic check). 0 = nunca.
	VerifyEveryDays int    `toml:"verify_every_days"`
	LastVerify      string `toml:"last_verify"` // RFC3339 de la última comprobación correcta
	LastMirror      string `toml:"last_mirror"` // RFC3339 de la última segunda copia correcta
}

var validTag = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		VaultPath:       filepath.Join(home, "pCloudDrive", "flowsint-vault"),
		Port:            5273,
		SaveIntervalMin: 10,
		KeepHourly:      24,
		KeepDaily:       30,
		KeepMonthly:     12,
		DockerSocket:    sockproxy.ModeFiltered,
		FlowsintVersion: "latest",
		Engine:          dock.EngineDocker,
		VerifyEveryDays: 7,
	}
}

// EnvDir permite usar otro directorio de configuración (lo usa `selftest`
// para no tocar la config real, y sirve para probar con un almacén aparte).
const EnvDir = "FLOWSINT_DESK_CONFIG_DIR"

func Dir() (string, error) {
	if d := os.Getenv(EnvDir); d != "" {
		return filepath.Abs(d)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "flowsint-desk"), nil
}

func FilePath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.toml"), nil
}

// Load devuelve la config existente o los valores por defecto si no hay fichero.
func Load() (*Config, error) {
	p, err := FilePath()
	if err != nil {
		return nil, err
	}
	c := Default()
	c.Path = p
	if _, err := os.Stat(p); os.IsNotExist(err) {
		c.applyEnv()
		return c, c.Validate()
	}
	if _, err := toml.DecodeFile(p, c); err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", p, err)
	}
	c.Path = p
	c.applyEnv()
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return c, nil
}

// EnvEngine fuerza el motor para este proceso (pruebas y CI; en el uso normal
// manda config.toml).
const EnvEngine = "FLOWSINT_DESK_ENGINE"

func (c *Config) applyEnv() {
	if e := os.Getenv(EnvEngine); e != "" {
		c.Engine = e
	}
}

// Validate rechaza valores con los que la sesión arrancaría para romperse
// después (p. ej. save_interval_min = 0 hace saltar time.NewTicker con todo
// ya levantado y los datos descifrados en RAM).
func (c *Config) Validate() error {
	if c.VaultPath == "" {
		return fmt.Errorf("vault_path vacío")
	}
	if c.Port < 1024 || c.Port > 65535 {
		return fmt.Errorf("port = %d no vale: usa un puerto entre 1024 y 65535 (sin sudo no se puede abrir uno menor)", c.Port)
	}
	if c.SaveIntervalMin < 1 {
		return fmt.Errorf("save_interval_min = %d no vale: mínimo 1 minuto", c.SaveIntervalMin)
	}
	if c.VerifyEveryDays < 0 {
		return fmt.Errorf("verify_every_days no puede ser negativo (0 = no comprobar nunca)")
	}
	if !dock.ValidEngine(c.Engine) {
		return fmt.Errorf("engine = %q no vale: usa \"docker\" o \"podman\"", c.Engine)
	}
	if !validTag.MatchString(c.FlowsintVersion) {
		return fmt.Errorf("flowsint_version = %q no vale: debe ser una etiqueta de imagen (p. ej. \"latest\" o \"1.2.10\")", c.FlowsintVersion)
	}
	if !sockproxy.ValidMode(c.DockerSocket) {
		return fmt.Errorf("docker_socket = %q no vale: usa \"filtrado\", \"directo\" o \"no\"", c.DockerSocket)
	}
	if c.KeepHourly < 0 || c.KeepDaily < 0 || c.KeepMonthly < 0 {
		return fmt.Errorf("keep_hourly/keep_daily/keep_monthly no pueden ser negativos")
	}
	if c.KeepHourly+c.KeepDaily+c.KeepMonthly == 0 {
		return fmt.Errorf("keep_hourly, keep_daily y keep_monthly a 0 borrarían TODOS los snapshots en el próximo forget")
	}
	return nil
}

func (c *Config) Save() error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(c.Path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

func (c *Config) ForgetDue() bool {
	if c.LastForget == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, c.LastForget)
	if err != nil {
		return true
	}
	return time.Since(t) > 24*time.Hour
}

// VerifyDue: toca comprobar la integridad del almacén.
func (c *Config) VerifyDue() bool {
	if c.VerifyEveryDays <= 0 {
		return false
	}
	t, err := time.Parse(time.RFC3339, c.LastVerify)
	return err != nil || time.Since(t) > time.Duration(c.VerifyEveryDays)*24*time.Hour
}

// Update relee config.toml, aplica el cambio y lo guarda: así una sesión
// larga no pisa lo que el usuario haya editado a mano mientras tanto.
func Update(change func(c *Config)) error {
	c, err := Load()
	if err != nil {
		return err
	}
	change(c)
	return c.Save()
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// MarkVerified / MarkMirrored anotan (en disco) la última vez que fue bien.
func MarkVerified() error { return Update(func(c *Config) { c.LastVerify = now() }) }
func MarkMirrored() error { return Update(func(c *Config) { c.LastMirror = now() }) }

func (c *Config) MarkForgotten() {
	c.LastForget = time.Now().UTC().Format(time.RFC3339)
}
