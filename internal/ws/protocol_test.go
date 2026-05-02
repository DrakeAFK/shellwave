package ws

import "testing"

func TestDecodeClientMessageRequiresType(t *testing.T) {
	_, err := DecodeClientMessage([]byte(`{"data":"ls\n"}`))
	if err == nil {
		t.Fatal("expected missing type error")
	}
}

func TestValidateConnect(t *testing.T) {
	msg, err := DecodeClientMessage([]byte(`{"type":"connect","host":"100.64.0.1","user":"root","port":22,"auth":{"type":"password","password":"secret"}}`))
	if err != nil {
		t.Fatalf("decode connect: %v", err)
	}
	if err := ValidateConnect(msg); err != nil {
		t.Fatalf("validate connect: %v", err)
	}
}

func TestEncodeServerMessage(t *testing.T) {
	data, err := EncodeServerMessage(Output("hello"))
	if err != nil {
		t.Fatalf("encode output: %v", err)
	}
	if string(data) != `{"type":"output","data":"hello"}` {
		t.Fatalf("unexpected output json: %s", data)
	}
}
