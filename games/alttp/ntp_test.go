package alttp

import (
	"github.com/beevik/ntp"
	"testing"
	"time"
)

func TestNTP(t *testing.T) {
	options := ntp.QueryOptions{ Timeout: 5*time.Second }
	response, err := ntp.QueryWithOptions("alttp.online", options)
	if err != nil {
		t.Fatal(err)
	}
	time := time.Now().Add(response.ClockOffset)
	t.Log("local ", time)
}
