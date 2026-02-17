package connect

import "context"

// MessageHandler is called when a message is received
type MessageHandler func(ctx context.Context, msg IncomingMessage)

// IncomingMessage represents a received message
type IncomingMessage struct {
	ID       string
	SpaceID  string
	PersonID string
	Email    string
	Text     string
}

// Mode is the interface for connection modes
type Mode interface {
	// Connect establishes the connection
	Connect(ctx context.Context) error
	// OnMessage registers a handler for incoming messages
	OnMessage(handler MessageHandler)
	// SendMessage sends a text message to a space
	SendMessage(ctx context.Context, spaceID string, text string) error
	// Close disconnects
	Close() error
}
