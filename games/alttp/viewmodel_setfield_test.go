package alttp

import (
	"reflect"
	"strings"
	"testing"
)

// every json-tagged bool setting of Game that the web UI shows must be accepted by the setField command, or toggling
// it in the UI is silently dropped:
func TestSetFieldAcceptsEveryToggle(t *testing.T) {
	args := map[string]bool{}
	at := reflect.TypeOf(setFieldArgs{})
	for i := 0; i < at.NumField(); i++ {
		args[strings.Split(at.Field(i).Tag.Get("json"), ",")[0]] = true
	}
	gt := reflect.TypeOf(Game{})
	for i := 0; i < gt.NumField(); i++ {
		f := gt.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if f.Type.Kind() != reflect.Bool || name == "" || name == "-" {
			continue
		}
		switch name {
		case "isCreated", "romVerified":
			continue
		}
		if !args[name] {
			t.Errorf("setFieldArgs does not accept the %q setting", name)
		}
	}
}
