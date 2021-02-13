package engine

import (
	"encoding/json"
	_ "o2/snes"
	_ "o2/snes/mock"
	"testing"
)

func TestController_HandleCommand(t *testing.T) {
	c := NewViewModel()
	c.Init()
	ce, err := c.CommandExecutor("snes", "connect")
	if err != nil {
		t.Fatal(err)
	}

	args := ce.CreateArgs()
	err = json.Unmarshal([]byte(`{"driver":"mock","device":1}`), args)
	if err != nil {
		t.Fatal(err)
	}

	err = ce.Execute(args)
	if err != nil {
		t.Fatal(err)
	}
}
