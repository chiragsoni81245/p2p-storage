package protocol

type Message struct {
	Type string `json:"type"`
	Key  string `json:"key,omitempty"`
	Data string `json:"data,omitempty"`
}
