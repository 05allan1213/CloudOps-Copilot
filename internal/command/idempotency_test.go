package command

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestValidateRequestAndResponse(t *testing.T) {
	hash := strings.Repeat("a", 64)
	if err := validateRequest(Request{ActorIdentityHash: hash, CommandScope: "incident.close", IdempotencyKey: "k", RequestHash: hash}); err != nil {
		t.Fatal(err)
	}
	if _, err := hex.DecodeString(hash); err != nil {
		t.Fatal(err)
	}
	for _, request := range []Request{
		{ActorIdentityHash: "short", CommandScope: "x", IdempotencyKey: "k", RequestHash: hash},
		{ActorIdentityHash: hash, CommandScope: "x", IdempotencyKey: "k", RequestHash: "not-hex"},
		{ActorIdentityHash: hash, CommandScope: "", IdempotencyKey: "k", RequestHash: hash},
	} {
		if err := validateRequest(request); err == nil {
			t.Fatalf("request %+v unexpectedly valid", request)
		}
	}
	if err := validateResponse(Response{HTTPStatus: 202, Body: []byte(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	if err := validateResponse(Response{HTTPStatus: 202, Body: []byte("not-json")}); err == nil {
		t.Fatal("invalid response JSON accepted")
	}
}
