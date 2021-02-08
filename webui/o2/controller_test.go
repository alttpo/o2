package main

import (
	"testing"
)

func TestController_HandleCommand(t *testing.T) {
	c := NewController()
	err := c.HandleCommand("snes", "Connect", map[string]interface{}{
		"driver": 0,
		"device": 0,
	})
	if err != nil {
		t.Fatal(err)
	}
}
