// Package rpc provides JSON-RPC 2.0 message types and a codec for the
// GoHome daemon wire protocol.
package rpc

import (
	"encoding/json"
	"fmt"
)

// ---------- ID ----------

// ID represents a JSON-RPC 2.0 request identifier. Per the spec an ID may be
// an integer, a string, or null. A nil *ID indicates a notification (no id).
type ID struct {
	num    int64
	str    string
	isStr  bool
}

// NewID creates a numeric ID.
func NewID(n int64) *ID {
	return &ID{num: n}
}

// Int64 returns the numeric value of the ID. For string IDs it returns 0.
func (id *ID) Int64() int64 {
	if id == nil {
		return 0
	}
	return id.num
}

// MarshalJSON implements json.Marshaler.
func (id *ID) MarshalJSON() ([]byte, error) {
	if id == nil {
		return []byte("null"), nil
	}
	if id.isStr {
		return json.Marshal(id.str)
	}
	return json.Marshal(id.num)
}

// UnmarshalJSON implements json.Unmarshaler.
func (id *ID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*id = ID{}
		return nil
	}

	// Try number first.
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		id.num = n
		id.str = ""
		id.isStr = false
		return nil
	}

	// Try string.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		id.str = s
		id.num = 0
		id.isStr = true
		return nil
	}

	return fmt.Errorf("rpc: id must be integer, string, or null; got %s", data)
}

// ---------- Error ----------

// Error represents a JSON-RPC 2.0 error object.
type Error struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    json.RawMessage  `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// ---------- Request ----------

// Request represents a JSON-RPC 2.0 request or notification. When ID is nil
// the message is a notification (no response expected).
type Request struct {
	ID     *ID              `json:"-"`
	Method string           `json:"-"`
	Params json.RawMessage  `json:"-"`
}

// MarshalJSON implements json.Marshaler. It always includes "jsonrpc":"2.0"
// and omits "id" for notifications.
func (r Request) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, 4)
	m["jsonrpc"] = "2.0"
	m["method"] = r.Method

	if r.ID != nil {
		m["id"] = r.ID
	}

	if r.Params != nil {
		m["params"] = r.Params
	}

	return json.Marshal(m)
}

// ---------- Response ----------

// Response represents a JSON-RPC 2.0 response. Exactly one of Result or Error
// must be set per the specification.
type Response struct {
	ID     *ID              `json:"-"`
	Result json.RawMessage  `json:"-"`
	Error  *Error           `json:"-"`
}

// MarshalJSON implements json.Marshaler. It always includes "jsonrpc":"2.0".
// When Error is non-nil, "error" is included and "result" is omitted. When
// Error is nil, "result" is included (even if null).
func (r Response) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, 3)
	m["jsonrpc"] = "2.0"

	if r.ID != nil {
		m["id"] = r.ID
	} else {
		m["id"] = nil
	}

	if r.Error != nil {
		m["error"] = r.Error
	} else {
		if r.Result != nil {
			m["result"] = r.Result
		} else {
			m["result"] = nil
		}
	}

	return json.Marshal(m)
}

// ---------- Message ----------

// Message is a decoded JSON-RPC 2.0 message. After calling Decode the caller
// uses IsRequest, IsNotification, or IsResponse to determine which fields are
// relevant.
type Message struct {
	ID     *ID              `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Params json.RawMessage  `json:"params,omitempty"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *Error           `json:"error,omitempty"`
}

// IsRequest returns true when the message has both a method and an id.
func (m *Message) IsRequest() bool {
	return m.Method != "" && m.ID != nil
}

// IsNotification returns true when the message has a method but no id.
func (m *Message) IsNotification() bool {
	return m.Method != "" && m.ID == nil
}

// IsResponse returns true when the message has no method (it is a response).
func (m *Message) IsResponse() bool {
	return m.Method == ""
}

// ---------- Decode ----------

// Decode parses raw JSON into a Message. It performs basic structural
// validation but does not enforce the full JSON-RPC 2.0 specification (e.g.
// it does not reject unknown fields).
func Decode(data []byte) (*Message, error) {
	// We use a temporary struct so we can handle the id field specially.
	var raw struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      json.RawMessage  `json:"id,omitempty"`
		Method  string           `json:"method,omitempty"`
		Params  json.RawMessage  `json:"params,omitempty"`
		Result  json.RawMessage  `json:"result,omitempty"`
		Error   *Error           `json:"error,omitempty"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("rpc: decode: %w", err)
	}

	msg := &Message{
		Method: raw.Method,
		Params: raw.Params,
		Result: raw.Result,
		Error:  raw.Error,
	}

	// Parse ID if present.
	if len(raw.ID) > 0 && string(raw.ID) != "null" {
		var id ID
		if err := json.Unmarshal(raw.ID, &id); err != nil {
			return nil, fmt.Errorf("rpc: decode id: %w", err)
		}
		msg.ID = &id
	}

	return msg, nil
}
