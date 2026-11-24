package protocol05

import "crypto/sha256"

// Group passwords: see o2-server's p5auth.go. A password keeps uninvited players out of a group; it isn't meant to
// hold up against someone who can see the network's packets. The join-group carries a token made from the password
// instead of the password: the first player to join a group sets its token, and every later join must carry the same.

// join-nak reasons of group passwords:
const (
	// NakAuthRequired: the group has a password and the join had no token.
	NakAuthRequired = 2
	// NakAuthFailed: the token isn't the group's.
	NakAuthFailed = 3
	// NakAuthUnexpected: the group exists without a password.
	NakAuthUnexpected = 4
)

// AuthToken makes the token of a password; nil for no password. AngelScript hosts only need sha256 for this.
func AuthToken(password string) []byte {
	if password == "" {
		return nil
	}
	sum := sha256.Sum256([]byte("o2-p5-group:" + password))
	return sum[:]
}
