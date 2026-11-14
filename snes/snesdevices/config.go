package snesdevices

import (
	"log"
	"os"
	"strings"
	"time"

	devconfig "github.com/alttpo/snes-devices/config"
)

// o2Config provides o2's configuration to the snes-devices drivers. Each key is read from the O2_<KEY> environment
// variable (e.g. "fxpakpro_disable" from O2_FXPAKPRO_DISABLE), falling back to o2's defaults.
type o2Config struct {
	defaults map[string]string
}

// defaults are o2's default driver settings; drivers fall back to their own built-in defaults for anything not here.
var defaults = map[string]string{
	// RetroArch's default network command port:
	"retroarch_hosts": "localhost:55355",
	// first port of the Emulator Network Access port range; the emunwa driver probes the range:
	"nwa_port_range": "0xbeef",
	// QUsb2Snes support is off by default; SNI is preferred:
	"qusb2snes_disable": "1",
}

func newO2Config() *o2Config {
	c := &o2Config{defaults: defaults}
	// log any driver settings coming from the environment once:
	for _, kv := range os.Environ() {
		if name, value, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(name, "O2_") {
			log.Printf("snes: %s=%q\n", name, value)
		}
	}
	return c
}

func (c *o2Config) Name(key string) string {
	return "O2_" + strings.ToUpper(key)
}

func (c *o2Config) lookup(key string) (string, bool) {
	if v, ok := os.LookupEnv(c.Name(key)); ok {
		return v, true
	}
	v, ok := c.defaults[key]
	return v, ok
}

func (c *o2Config) IsSet(key string) bool {
	_, ok := c.lookup(key)
	return ok
}

func (c *o2Config) GetString(key string) string {
	v, _ := c.lookup(key)
	return v
}

func (c *o2Config) GetBool(key string) bool {
	return devconfig.IsTruthy(c.GetString(key))
}

func (c *o2Config) GetDuration(key string) time.Duration {
	d, err := time.ParseDuration(c.GetString(key))
	if err != nil {
		return 0
	}
	return d
}
