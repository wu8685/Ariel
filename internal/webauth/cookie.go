package webauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

type cookieCodec struct {
	aead cipher.AEAD
	now  func() time.Time
}

type cookieEnvelope struct {
	IssuedAt  int64           `json:"iat"`
	ExpiresAt int64           `json:"exp"`
	Payload   json.RawMessage `json:"payload"`
}

func newCookieCodec(key []byte, now func() time.Time) (*cookieCodec, error) {
	if len(key) != 32 {
		return nil, errors.New("session key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &cookieCodec{aead: aead, now: now}, nil
}

func (c *cookieCodec) seal(purpose string, payload any, ttl time.Duration) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	now := c.now()
	envelope, err := json.Marshal(cookieEnvelope{IssuedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(), Payload: raw})
	if err != nil {
		return "", err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, envelope, []byte(purpose))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (c *cookieCodec) open(purpose, token string, dst any) error {
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(sealed) < c.aead.NonceSize()+c.aead.Overhead() {
		return errors.New("invalid cookie")
	}
	nonce := sealed[:c.aead.NonceSize()]
	plain, err := c.aead.Open(nil, nonce, sealed[c.aead.NonceSize():], []byte(purpose))
	if err != nil {
		return errors.New("invalid cookie")
	}
	var envelope cookieEnvelope
	if json.Unmarshal(plain, &envelope) != nil || envelope.ExpiresAt <= c.now().Unix() || envelope.IssuedAt > c.now().Add(time.Minute).Unix() {
		return errors.New("expired or invalid cookie")
	}
	if err := json.Unmarshal(envelope.Payload, dst); err != nil {
		return errors.New("invalid cookie payload")
	}
	return nil
}
