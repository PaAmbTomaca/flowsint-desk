package config

import "testing"

func TestDefaultEsValida(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRechazaValoresPeligrosos(t *testing.T) {
	casos := map[string]func(*Config){
		"intervalo 0":      func(c *Config) { c.SaveIntervalMin = 0 },
		"puerto 0":         func(c *Config) { c.Port = 0 },
		"puerto < 1024":    func(c *Config) { c.Port = 80 },
		"puerto > 65535":   func(c *Config) { c.Port = 70000 },
		"retención neg.":   func(c *Config) { c.KeepDaily = -1 },
		"retención a cero": func(c *Config) { c.KeepHourly, c.KeepDaily, c.KeepMonthly = 0, 0, 0 },
		"almacén vacío":    func(c *Config) { c.VaultPath = "" },
	}
	for nombre, rompe := range casos {
		c := Default()
		rompe(c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: esperaba error", nombre)
		}
	}
}
