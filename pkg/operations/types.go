package operations

// RequestID uniquely identifies an async operation so callers can correlate events.
type RequestID = string

// StoreResult holds the outcome of a StoreLocally call.
type StoreResult struct {
	Key string
}

// SendResult holds the outcome of a SendFile call.
type SendResult struct {
	Key    string
	PeerID string
}

// SendOpts configures the behaviour of SendFile.
type SendOpts struct {
	Session string
}
