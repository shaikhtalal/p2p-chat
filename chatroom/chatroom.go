package chatroom

import (
	"context"
	"encoding/json"

	"github.com/libp2p/go-libp2p-core/peer"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

// ChatRoomBufSize is the number of incoming messages to buffer for each topic.
const ChatRoomBufSize = 128

// ChatRoom represents a subscription to a single PubSub topic. Messages
// can be published to the topic with ChatRoom.Publish, and received
// messages are pushed to the Messages channel.
type ChatRoom struct {
	// Messages is a channel of messages received from other peers in the chat room
	Messages chan *ChatMessage

	Ctx   context.Context
	Ps    *pubsub.PubSub
	Topic *pubsub.Topic
	Sub   *pubsub.Subscription

	RoomName        string
	Self            peer.ID
	Nick            string
	ConnectToNodeID string
}

// ChatMessage gets converted to/from JSON and sent in the body of pubsub messages.
type ChatMessage struct {
	Message    string
	SenderID   string
	SenderNick string
}

// JoinChatRoom tries to subscribe to the PubSub topic for the room name, returning
// a ChatRoom on success.
func JoinChatRoom(ctx context.Context, ps *pubsub.PubSub, selfID peer.ID, nickname string, roomName string, connectToNodeID string) (*ChatRoom, error) {
	// join the pubsub topic
	topic, err := ps.Join(topicName(roomName))
	if err != nil {
		return nil, err
	}

	// and subscribe to it
	sub, err := topic.Subscribe()
	if err != nil {
		return nil, err
	}

	cr := &ChatRoom{
		Ctx:             ctx,
		Ps:              ps,
		Topic:           topic,
		Sub:             sub,
		Self:            selfID,
		Nick:            nickname,
		RoomName:        roomName,
		ConnectToNodeID: connectToNodeID,
		Messages:        make(chan *ChatMessage, ChatRoomBufSize),
	}

	// start reading messages from the subscription in a loop
	go cr.readLoop()
	return cr, nil
}

// Publish sends a message to the pubsub topic.
func (cr *ChatRoom) Publish(message string) error {
	m := ChatMessage{
		Message:    message,
		SenderID:   cr.Self.Pretty(),
		SenderNick: cr.Nick,
	}
	msgBytes, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return cr.Topic.Publish(cr.Ctx, msgBytes)
}

func (cr *ChatRoom) ListPeers() []peer.ID {
	return cr.Ps.ListPeers(topicName(cr.RoomName))
}

// readLoop pulls messages from the pubsub topic and pushes them onto the Messages channel.
func (cr *ChatRoom) readLoop() {
	for {
		msg, err := cr.Sub.Next(cr.Ctx)
		if err != nil {
			close(cr.Messages)
			return
		}
		// only forward messages delivered by others
		if msg.ReceivedFrom == cr.Self {
			continue
		}
		cm := new(ChatMessage)
		err = json.Unmarshal(msg.Data, cm)
		if err != nil {
			continue
		}
		// send valid messages onto the Messages channel
		cr.Messages <- cm
	}
}

func topicName(roomName string) string {
	return "chat-room:" + roomName
}
