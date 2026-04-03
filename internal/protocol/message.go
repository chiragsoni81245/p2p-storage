package protocol

// Message types
const (
	TypeStoreFile    = "STORE_FILE"
	TypeStoreFileAck = "STORE_FILE_ACK"
	TypeGetFile      = "GET_FILE"
	TypeGetFileResp  = "GET_FILE_RESPONSE"
	TypeError        = "ERROR"
)

// Response data values
const (
	DataSuccess         = "success"
	DataFound           = "found"
	DataNotFound        = "file not found"
	DataReadFailed      = "failed to read file"
	DataEmptyKey        = "key cannot be empty"
	DataPeerNotFound    = "peer not registered"
	DataInvalidMsg      = "invalid message format"
	DataUnknownType     = "unknown message type"
	DataSessionRejected = "session rejected"
	DataAlreadyExists   = "file already exists"
)

type Message struct {
	Type    string `json:"type"`
	Key     string `json:"key,omitempty"`
	Data    string `json:"data,omitempty"`
	Session string `json:"session,omitempty"`
}
