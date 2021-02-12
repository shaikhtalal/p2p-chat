package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	peerstore "github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/libp2p/go-libp2p-examples/pubsub/chat/chatroom"
	"github.com/libp2p/go-libp2p-examples/pubsub/chat/ui"
	"github.com/libp2p/go-libp2p-examples/pubsub/chat/utils"
	"github.com/libp2p/go-libp2p/p2p/discovery"
	"github.com/multiformats/go-multiaddr"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

// DiscoveryInterval is how often we re-publish our mDNS records.
const DiscoveryInterval = time.Hour

// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
const DiscoveryServiceTag = "pubsub-chat-example"

func main() {
	// parse some flags to set our nickname and the room to join
	sourcePort := flag.Int("sp", 0, "Source port number")
	dest := flag.String("d", "", "Destination multiaddr string")
	nickFlag := flag.String("nick", "talal", "nickname to use in chat. will be generated if empty")
	roomFlag := flag.String("room", "awesome-chat-room", "name of chat room to join")
	flag.Parse()

	ctx := context.Background()

	if *sourcePort == 0 {
		*sourcePort = 0
	}

	// use the nickname from the cli flag, or a default if blank
	nick := *nickFlag
	if len(nick) == 0 {
		nick = "nick"
	}

	room := *roomFlag

	pc := setPeerConnection(ctx, nick, room, dest, sourcePort)
	pc.SetP2p()

}

type peerConnection struct {
	ctx             context.Context
	ps              *pubsub.PubSub
	peerID          peer.ID
	port            *int
	dest            *string
	nick            string
	room            string
	connectToNodeID string
}

func setPeerConnection(ctx context.Context, nick string, room string, dest *string, sourcePort *int) *peerConnection {
	return &peerConnection{
		ctx:  ctx,
		port: sourcePort,
		dest: dest,
		nick: nick,
		room: room,
	}
}

// printErr is like fmt.Printf, but writes to stderr.
func printErr(m string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, m, args...)
}

// defaultNick generates a nickname based on the $USER environment variable and
// the last 8 chars of a peer ID.
func defaultNick(p peer.ID) string {
	return fmt.Sprintf("%s-%s", os.Getenv("USER"), utils.ShortID(p))
}

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
type discoveryNotifee struct {
	h host.Host
}

// HandlePeerFound connects to peers discovered via mDNS. Once they're connected,
// the PubSub system will automatically start interacting with them if they also
// support PubSub.
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	fmt.Printf("discovered new peer %s\n", pi.ID.Pretty())
	err := n.h.Connect(context.Background(), pi)
	if err != nil {
		fmt.Printf("error connecting to peer %s: %s\n", pi.ID.Pretty(), err)
	}
}

// setupDiscovery creates an mDNS discovery service and attaches it to the libp2p Host.
// This lets us automatically discover peers on the same LAN and connect to them.
func setupDiscovery(ctx context.Context, h host.Host) error {
	// setup mDNS discovery to find local peers
	disc, err := discovery.NewMdnsService(ctx, h, DiscoveryInterval, DiscoveryServiceTag)
	if err != nil {
		return err
	}

	n := discoveryNotifee{h: h}
	disc.RegisterNotifee(&n)
	return nil
}

func (pc *peerConnection) handleStream(s network.Stream) {
	log.Println("Got a new stream")
	// join the chat room
	cr, err := chatroom.JoinChatRoom(pc.ctx, pc.ps, pc.peerID, pc.nick, pc.room, pc.connectToNodeID)
	if err != nil {
		panic(err)
	}

	// draw the UI
	ui := ui.NewChatUI(cr)
	if err = ui.Run(); err != nil {
		printErr("error running text UI: %s", err)
	}
}

func (pc *peerConnection) SetP2p() {
	if *pc.port == 0 {
		*pc.port = 0
	}

	r := rand.New(rand.NewSource(int64(*pc.port)))

	// Creates a new RSA key pair for this host.
	prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	if err != nil {
		panic(err)
	}
	// 0.0.0.0 will listen on any interface device.
	sourceMultiAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", *pc.port))

	// create a new libp2p Host that listens on a random TCP port
	h, err := libp2p.New(pc.ctx, libp2p.ListenAddrs(sourceMultiAddr),
		libp2p.Identity(prvKey))
	if err != nil {
		fmt.Printf("%+v", err)
	}
	var connectToNodeID string
	if *pc.dest == "" {
		h.SetStreamHandler("/chat/1.0.0", pc.handleStream)
		var port string
		for _, la := range h.Network().ListenAddresses() {
			if p, err := la.ValueForProtocol(multiaddr.P_TCP); err == nil {
				port = p
				break
			}
		}
		if port == "" {
			panic("was not able to find actual local port")
		}
		connectToNodeID = fmt.Sprintf("Copy to connect to other node /ip4/127.0.0.1/tcp/%v/p2p/%s' on another console and machine.\n", port, h.ID().Pretty())
		pc.connectToNodeID = connectToNodeID
	}

	// create a new PubSub service using the GossipSub router
	pc.ps, err = pubsub.NewGossipSub(pc.ctx, h)
	if err != nil {
		panic(err)
	}

	// setup local mDNS discovery
	err = setupDiscovery(pc.ctx, h)
	if err != nil {
		panic(err)
	}

	// use the nickname from the cli flag, or a default if blank
	if len(pc.nick) == 0 {
		pc.nick = defaultNick(h.ID())
	}

	// join the room from the cli flag, or the flag default
	var s network.Stream
	var peerID peer.ID
	peerID = h.ID()
	if *pc.dest != "" {
		// Turn the destination into a multiaddr.
		maddr, err := multiaddr.NewMultiaddr(*pc.dest)
		if err != nil {
			log.Fatalln(err)
		}
		// Extract the peer ID from the multiaddr.
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			log.Fatalln(err)
		}
		// Add the destination's peer multiaddress in the peerstore.
		// This will be used during connection and stream creation by libp2p.
		h.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)
		// Start a stream with the destination.
		// Multiaddress of the destination peer is fetched from the peerstore using 'peerId'.
		s, err = h.NewStream(context.Background(), info.ID, "/chat/1.0.0")
		if err != nil {
			panic(err)
		}
	}

	if s != nil {
		s.Conn()
		peerID = s.Conn().LocalPeer()
		pc.peerID = peerID
		pc.handleStream(s)
	}

	// join the chat room
	cr, err := chatroom.JoinChatRoom(pc.ctx, pc.ps, peerID, pc.nick, pc.room, connectToNodeID)
	if err != nil {
		panic(err)
	}

	// draw the UI
	ui := ui.NewChatUI(cr)
	if err = ui.Run(); err != nil {
		printErr("error running text UI: %s", err)
	}
}
