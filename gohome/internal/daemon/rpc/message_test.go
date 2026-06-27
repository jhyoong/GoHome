package rpc

import (
	"encoding/json"
	"testing"
)

func TestEncodeRequest(t *testing.T) {
	id := NewID(42)
	req := Request{
		ID:     id,
		Method: "tools/list",
		Params: json.RawMessage(`{"cursor":"abc"}`),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	// Must include jsonrpc "2.0"
	ver, ok := raw["jsonrpc"]
	if !ok {
		t.Fatal("missing jsonrpc field")
	}
	if string(ver) != `"2.0"` {
		t.Fatalf("jsonrpc = %s, want \"2.0\"", ver)
	}

	// Must include id
	idRaw, ok := raw["id"]
	if !ok {
		t.Fatal("missing id field")
	}
	if string(idRaw) != "42" {
		t.Fatalf("id = %s, want 42", idRaw)
	}

	// Must include method
	methodRaw, ok := raw["method"]
	if !ok {
		t.Fatal("missing method field")
	}
	if string(methodRaw) != `"tools/list"` {
		t.Fatalf("method = %s, want \"tools/list\"", methodRaw)
	}

	// Must include params
	paramsRaw, ok := raw["params"]
	if !ok {
		t.Fatal("missing params field")
	}
	if string(paramsRaw) != `{"cursor":"abc"}` {
		t.Fatalf("params = %s, want {\"cursor\":\"abc\"}", paramsRaw)
	}
}

func TestEncodeNotification(t *testing.T) {
	req := Request{
		ID:     nil, // notification: no ID
		Method: "notifications/initialized",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	// Must include jsonrpc "2.0"
	ver, ok := raw["jsonrpc"]
	if !ok {
		t.Fatal("missing jsonrpc field")
	}
	if string(ver) != `"2.0"` {
		t.Fatalf("jsonrpc = %s, want \"2.0\"", ver)
	}

	// Must NOT include id
	if _, ok := raw["id"]; ok {
		t.Fatal("notification must not have id field")
	}

	// Must include method
	methodRaw, ok := raw["method"]
	if !ok {
		t.Fatal("missing method field")
	}
	if string(methodRaw) != `"notifications/initialized"` {
		t.Fatalf("method = %s, want \"notifications/initialized\"", methodRaw)
	}
}

func TestEncodeResponse(t *testing.T) {
	id := NewID(7)
	resp := Response{
		ID:     id,
		Result: json.RawMessage(`{"tools":[]}`),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	// Must include jsonrpc "2.0"
	ver, ok := raw["jsonrpc"]
	if !ok {
		t.Fatal("missing jsonrpc field")
	}
	if string(ver) != `"2.0"` {
		t.Fatalf("jsonrpc = %s, want \"2.0\"", ver)
	}

	// Must include id
	idRaw, ok := raw["id"]
	if !ok {
		t.Fatal("missing id field")
	}
	if string(idRaw) != "7" {
		t.Fatalf("id = %s, want 7", idRaw)
	}

	// Must include result
	resultRaw, ok := raw["result"]
	if !ok {
		t.Fatal("missing result field")
	}
	if string(resultRaw) != `{"tools":[]}` {
		t.Fatalf("result = %s, want {\"tools\":[]}", resultRaw)
	}

	// Must NOT include error
	if _, ok := raw["error"]; ok {
		t.Fatal("success response must not have error field")
	}
}

func TestEncodeErrorResponse(t *testing.T) {
	id := &ID{str: "req-1", isStr: true}
	resp := Response{
		ID: id,
		Error: &Error{
			Code:    -32601,
			Message: "Method not found",
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	// Must include jsonrpc "2.0"
	ver, ok := raw["jsonrpc"]
	if !ok {
		t.Fatal("missing jsonrpc field")
	}
	if string(ver) != `"2.0"` {
		t.Fatalf("jsonrpc = %s, want \"2.0\"", ver)
	}

	// Must include id (string)
	idRaw, ok := raw["id"]
	if !ok {
		t.Fatal("missing id field")
	}
	if string(idRaw) != `"req-1"` {
		t.Fatalf("id = %s, want \"req-1\"", idRaw)
	}

	// Must include error
	errRaw, ok := raw["error"]
	if !ok {
		t.Fatal("missing error field")
	}
	var errObj Error
	if err := json.Unmarshal(errRaw, &errObj); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if errObj.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", errObj.Code)
	}
	if errObj.Message != "Method not found" {
		t.Fatalf("error message = %q, want \"Method not found\"", errObj.Message)
	}

	// Must NOT include result
	if _, ok := raw["result"]; ok {
		t.Fatal("error response must not have result field")
	}
}

func TestDecodeMessage_Request(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"name":"test"}}`)

	msg, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !msg.IsRequest() {
		t.Fatal("expected IsRequest() to be true")
	}
	if msg.IsNotification() {
		t.Fatal("expected IsNotification() to be false")
	}
	if msg.IsResponse() {
		t.Fatal("expected IsResponse() to be false")
	}

	if msg.Method != "initialize" {
		t.Fatalf("method = %q, want \"initialize\"", msg.Method)
	}
	if msg.ID == nil {
		t.Fatal("expected non-nil ID")
	}
	if msg.ID.Int64() != 1 {
		t.Fatalf("id = %d, want 1", msg.ID.Int64())
	}
	if string(msg.Params) != `{"name":"test"}` {
		t.Fatalf("params = %s, want {\"name\":\"test\"}", msg.Params)
	}
}

func TestDecodeMessage_Notification(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	msg, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if msg.IsRequest() {
		t.Fatal("expected IsRequest() to be false")
	}
	if !msg.IsNotification() {
		t.Fatal("expected IsNotification() to be true")
	}
	if msg.IsResponse() {
		t.Fatal("expected IsResponse() to be false")
	}

	if msg.Method != "notifications/initialized" {
		t.Fatalf("method = %q, want \"notifications/initialized\"", msg.Method)
	}
	if msg.ID != nil {
		t.Fatal("expected nil ID for notification")
	}
}

func TestDecodeMessage_Response(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":5,"result":{"status":"ok"}}`)

	msg, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if msg.IsRequest() {
		t.Fatal("expected IsRequest() to be false")
	}
	if msg.IsNotification() {
		t.Fatal("expected IsNotification() to be false")
	}
	if !msg.IsResponse() {
		t.Fatal("expected IsResponse() to be true")
	}

	if msg.ID == nil {
		t.Fatal("expected non-nil ID")
	}
	if msg.ID.Int64() != 5 {
		t.Fatalf("id = %d, want 5", msg.ID.Int64())
	}
	if string(msg.Result) != `{"status":"ok"}` {
		t.Fatalf("result = %s, want {\"status\":\"ok\"}", msg.Result)
	}
}

func TestIDMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		id      *ID
		wantJSON string
	}{
		{"integer", NewID(42), "42"},
		{"string", &ID{str: "abc", isStr: true}, `"abc"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.id)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tt.wantJSON {
				t.Fatalf("got %s, want %s", data, tt.wantJSON)
			}

			var got ID
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			// Re-marshal to compare
			data2, err := json.Marshal(&got)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if string(data2) != tt.wantJSON {
				t.Fatalf("round-trip got %s, want %s", data2, tt.wantJSON)
			}
		})
	}
}

func TestErrorInterface(t *testing.T) {
	e := &Error{Code: -32600, Message: "Invalid Request"}
	var err error = e
	if err.Error() != "rpc error -32600: Invalid Request" {
		t.Fatalf("Error() = %q", err.Error())
	}
}

func TestDecodeMessage_ErrorResponse(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":3,"error":{"code":-32601,"message":"Method not found"}}`)

	msg, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !msg.IsResponse() {
		t.Fatal("expected IsResponse() to be true")
	}
	if msg.Error == nil {
		t.Fatal("expected non-nil Error")
	}
	if msg.Error.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", msg.Error.Code)
	}
}
