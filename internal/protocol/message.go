package protocol

import "encoding/json"

// Message types
const (
	TypeStoreFile     = "STORE_FILE"
	TypeStoreFileResp = "STORE_FILE_RESP"
	TypeGetFile       = "GET_FILE"
	TypeError         = "ERROR"
)

// Status values used in response payloads
const (
	StatusSuccess         = "success"
	StatusNotFound        = "file not found"
	StatusReadFailed      = "failed to read file"
	StatusEmptyKey        = "key cannot be empty"
	StatusInvalidMsg      = "invalid message format"
	StatusUnknownType     = "unknown message type"
	StatusSessionRejected = "session rejected"
	StatusAlreadyExists   = "file already exists"
)

// Message is the protocol envelope. Type identifies the message kind;
// Data holds the type-specific payload as raw JSON.
type Message struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// NewMessage marshals payload into a Message. Panics if payload cannot be marshalled.
func NewMessage(msgType string, payload any) Message {
	data, err := json.Marshal(payload)
	if err != nil {
		panic("protocol.NewMessage: failed to marshal payload: " + err.Error())
	}
	return Message{Type: msgType, Data: data}
}

// Decode unmarshals the message Data into T.
func Decode[T any](m Message) (T, error) {
	var v T
	if err := json.Unmarshal(m.Data, &v); err != nil {
		return v, err
	}
	return v, nil
}

// --- Payload structs ---

type StoreFilePayload struct {
	Key     string `json:"key"`
	Session string `json:"session,omitempty"`
}

type StoreFileRespPayload struct {
	Key    string `json:"key,omitempty"`
	Status string `json:"status"`
}

// GetFilePayload covers both TTL=1 (direct peer) and TTL=N (multi-hop).
type GetFilePayload struct {
	Key            string   `json:"key"`
	MsgID          string   `json:"msg_id"`
	TTL            int      `json:"ttl"`
	RequesterID    string   `json:"requester_id"`
	RequesterAddrs []string `json:"requester_addrs"`
}

type ErrorPayload struct {
	Reason string `json:"reason"`
}
