package webauth

import (
	"strings"
	"testing"
	"time"
)

func TestCookieCodecRoundTripTamperExpiryAndPurposeIsolation(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	codec, err := newCookieCodec([]byte(strings.Repeat("k", 32)), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	token, err := codec.seal("session", map[string]string{"subject": "owner"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := codec.open("session", token, &got); err != nil || got["subject"] != "owner" {
		t.Fatalf("round trip: got=%v err=%v", got, err)
	}
	if err := codec.open("ceremony", token, &got); err == nil {
		t.Fatal("cookie opened under a different purpose")
	}
	position := len(token) / 2
	replacement := "A"
	if token[position] == 'A' {
		replacement = "B"
	}
	tampered := token[:position] + replacement + token[position+1:]
	if err := codec.open("session", tampered, &got); err == nil {
		t.Fatal("tampered cookie accepted")
	}
	now = now.Add(2 * time.Hour)
	if err := codec.open("session", token, &got); err == nil {
		t.Fatal("expired cookie accepted")
	}
}

func TestCookieCodecRequiresAES256Key(t *testing.T) {
	if _, err := newCookieCodec([]byte("short"), time.Now); err == nil {
		t.Fatal("short key accepted")
	}
}
