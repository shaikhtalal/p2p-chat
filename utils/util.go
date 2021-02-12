package utils

import "github.com/libp2p/go-libp2p-core/peer"

// shortID returns the last 8 chars of a base58-encoded peer id.
func ShortID(p peer.ID) string {
	pretty := p.Pretty()
	return pretty[len(pretty)-8:]
}
