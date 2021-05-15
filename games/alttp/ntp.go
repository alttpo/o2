package alttp

import (
	"github.com/beevik/ntp"
	"log"
	"time"
)

// cannot query NTP servers faster than once per:
const ntpRateLimit = time.Second * 2
// should refresh NTP ClockOffset every:
const ntpRefreshRate = time.Minute * 5

// this is run in a dedicated goroutine
func (g *Game) ntpQueryLoop() {
	t := time.NewTicker(ntpRefreshRate)
	for {
		select {
		case n := <-g.ntpC:
			if n == 0 {
				t.Reset(0)
			}
		case <-t.C:
			if time.Now().Sub(g.clockQueried) < ntpRateLimit {
				// wait until the rate limit is passed:
				t.Reset(ntpRateLimit)
				break
			}

			// query NTP:
			if g.QueryNTP() {
				t.Reset(ntpRefreshRate)
			} else {
				t.Reset(ntpRateLimit)
			}
		}
	}
}

func (g *Game) QueryNTP() bool {
	// query player-supplied hostname for NTP server, then fallback to alttp.online NTP server:
	hosts := make([]string, 0, 2)
	if g.client != nil {
		hostName := g.client.HostName()
		if hostName != "" {
			hosts = append(hosts, hostName)
		}
	}
	if len(hosts) == 0 || (len(hosts) == 1 && hosts[0] != "alttp.online") {
		hosts = append(hosts, "alttp.online")
	}

	for _, host := range hosts {
		log.Printf("game: ntp query: %s\n", host)
		options := ntp.QueryOptions{Timeout: 5 * time.Second}
		response, err := ntp.QueryWithOptions(host, options)
		if err == nil {
			g.clockQueried = time.Now()
			if response.Stratum > 0 {
				g.clockOffset = response.ClockOffset
				g.clockServer = host
				log.Printf("game: ntp result: %s; %v\n", host, g.clockOffset)
				log.Printf("game: ntp time: %v\n", time.Now().Add(g.clockOffset))
				return true
			} else {
				log.Printf("game: ntp query error: %s: stratum=%v, kissCode=%v\n", host, response.Stratum, response.KissCode)
			}
		}
		log.Printf("game: ntp query error: %s: %v\n", host, err)
	}

	return false
}
