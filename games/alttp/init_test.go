package alttp

import "log"

func init() {
	log.SetFlags(log.LUTC | log.Lmicroseconds | log.Ltime)
}
