package rpc

import (
	"encoding/json"
	"testing"
)

func assertJSONField(t *testing.T, raw map[string]json.RawMessage, field, want string) {
	t.Helper()
	got, ok := raw[field]
	if !ok {
		t.Fatalf("missing %q field", field)
	}
	if string(got) != want {
		t.Fatalf("%s = %s, want %s", field, got, want)
	}
}

func assertNoJSONField(t *testing.T, raw map[string]json.RawMessage, field string) {
	t.Helper()
	if _, ok := raw[field]; ok {
		t.Fatalf("unexpected %q field present", field)
	}
}

func marshalToRaw(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	return raw
}

func TestEncodeRequest(t *testing.T) {
	req := Request{
		ID:     NewID(42),
		Method: "tools/list",
		Params: json.RawMessage(`{"cursor":"abc"}`),
	}
	raw := marshalToRaw(t, req)

	assertJSONField(t, raw, "jsonrpc", `"2.0"`)
	assertJSONField(t, raw, "id", "42")
	assertJSONField(t, raw, "method", `"tools/list"`)
	assertJSONField(t, raw, "params", `{"cursor":"abc"}`)
}

func TestEncodeNotification(t *testing.T) {
	req := Request{
		ID:     nil,
		Method: "notifications/initialized",
	}
	raw := marshalToRaw(t, req)

	assertJSONField(t, raw, "jsonrpc", `"2.0"`)
	assertNoJSONField(t, raw, "id")
	assertJSONField(t, raw, "method", `"notifications/initialized"`)
}

func TestEncodeResponse(t *testing.T) {
	resp := Response{
		ID:     NewID(7),
		Result: json.RawMessage(`{"tools":[]}`),
	}
	raw := marshalToRaw(t, resp)

	assertJSONField(t, raw, "jsonrpc", `"2.0"`)
	assertJSONField(t, raw, "id", "7")
	assertJSONField(t, raw, "result", `{"tools":[]}`)
	assertNoJSONField(t, raw, "error")
}

func TestEncodeErrorResponse(t *testing.T) {
	resp := Response{
		ID:    &ID{str: "req-1", isStr: true},
		Error: &Error{Code: -32601, Message: "Method not found"},
	}
	raw := marshalToRaw(t, resp)

	assertJSONField(t, raw, "jsonrpc", `"2.0"`)
	assertJSONField(t, raw, "id", `"req-1"`)

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

	assertNoJSONField(t, raw, "result")
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
