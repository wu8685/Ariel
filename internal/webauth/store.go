package webauth

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	lib "github.com/go-webauthn/webauthn/webauthn"
)

const credentialFileVersion = 1

type credentialUser struct {
	ID          []byte           `json:"id"`
	Name        string           `json:"name"`
	DisplayName string           `json:"displayName"`
	Credentials []lib.Credential `json:"credentials"`
}

func (u credentialUser) WebAuthnID() []byte                    { return u.ID }
func (u credentialUser) WebAuthnName() string                  { return u.Name }
func (u credentialUser) WebAuthnDisplayName() string           { return u.DisplayName }
func (u credentialUser) WebAuthnCredentials() []lib.Credential { return u.Credentials }

type credentialFile struct {
	Version int            `json:"version"`
	User    credentialUser `json:"user"`
}

type credentialStore struct {
	mu   sync.RWMutex
	path string
	user credentialUser
}

func openCredentialStore(path string) (*credentialStore, error) {
	if path == "" {
		return nil, errors.New("credentials file is required")
	}
	fileHandle, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		id := make([]byte, 32)
		if _, err := rand.Read(id); err != nil {
			return nil, err
		}
		return &credentialStore{path: path, user: credentialUser{ID: id, Name: "owner", DisplayName: "Ariel Owner", Credentials: []lib.Credential{}}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open credentials file: %w", err)
	}
	defer fileHandle.Close()
	info, err := fileHandle.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect credentials file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("credentials file must be a private regular file without group or other access")
	}
	raw, err := io.ReadAll(fileHandle)
	if err != nil {
		return nil, fmt.Errorf("read credentials file: %w", err)
	}
	var file credentialFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("decode credentials file: %w", err)
	}
	if file.Version != credentialFileVersion || len(file.User.ID) == 0 || len(file.User.ID) > 64 || file.User.Name == "" || file.User.DisplayName == "" {
		return nil, errors.New("unsupported or invalid credentials file")
	}
	if file.User.Credentials == nil {
		file.User.Credentials = []lib.Credential{}
	}
	return &credentialStore{path: path, user: file.User}, nil
}

func (s *credentialStore) snapshot() credentialUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneUser(s.user)
}

func cloneUser(user credentialUser) credentialUser {
	raw, err := json.Marshal(user)
	if err != nil {
		panic("webauthn credential cannot be encoded: " + err.Error())
	}
	var cloned credentialUser
	if err := json.Unmarshal(raw, &cloned); err != nil {
		panic("webauthn credential cannot be decoded: " + err.Error())
	}
	return cloned
}

func (s *credentialStore) add(credential lib.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneUser(s.user)
	for _, existing := range next.Credentials {
		if bytes.Equal(existing.ID, credential.ID) {
			return errors.New("credential already exists")
		}
	}
	next.Credentials = append(next.Credentials, credential)
	if err := s.persist(next); err != nil {
		return err
	}
	s.user = next
	return nil
}

func (s *credentialStore) addBootstrap(credential lib.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.user.Credentials) != 0 {
		return errors.New("initial Passkey is already enrolled")
	}
	next := cloneUser(s.user)
	next.Credentials = append(next.Credentials, credential)
	if err := s.persist(next); err != nil {
		return err
	}
	s.user = next
	return nil
}

func (s *credentialStore) update(credential lib.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneUser(s.user)
	found := false
	for i := range next.Credentials {
		if bytes.Equal(next.Credentials[i].ID, credential.ID) {
			existing := next.Credentials[i]
			if credential.Authenticator.SignCount <= existing.Authenticator.SignCount && (credential.Authenticator.SignCount != 0 || existing.Authenticator.SignCount != 0) {
				credential.Authenticator.CloneWarning = true
			}
			if credential.Authenticator.SignCount < existing.Authenticator.SignCount {
				credential.Authenticator.SignCount = existing.Authenticator.SignCount
			}
			credential.Authenticator.CloneWarning = credential.Authenticator.CloneWarning || existing.Authenticator.CloneWarning
			credential.Flags.UserVerified = credential.Flags.UserVerified || existing.Flags.UserVerified
			credential.Flags.BackupEligible = existing.Flags.BackupEligible
			next.Credentials[i] = credential
			found = true
			break
		}
	}
	if !found {
		return errors.New("credential does not exist")
	}
	if err := s.persist(next); err != nil {
		return err
	}
	s.user = next
	return nil
}

func (s *credentialStore) persist(user credentialUser) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".ariel-auth-*")
	if err != nil {
		return fmt.Errorf("create credentials temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(credentialFile{Version: credentialFileVersion, User: user}); err != nil {
		tmp.Close()
		return fmt.Errorf("encode credentials file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync credentials file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace credentials file: %w", err)
	}
	return os.Chmod(s.path, 0o600)
}
