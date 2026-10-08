package webauth

import (
	"os"
	"path/filepath"
	"testing"

	lib "github.com/go-webauthn/webauthn/webauthn"
)

func TestCredentialStoreCreatesAndAtomicallyPersistsPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	store, err := openCredentialStore(path)
	if err != nil {
		t.Fatal(err)
	}
	user := store.snapshot()
	if len(user.ID) != 32 || len(user.Credentials) != 0 {
		t.Fatalf("new user = %+v", user)
	}
	credential := lib.Credential{ID: []byte("credential-id"), PublicKey: []byte("public-key")}
	if err := store.add(credential); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode = %o", info.Mode().Perm())
	}
	reloaded, err := openCredentialStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.snapshot()
	if len(got.Credentials) != 1 || string(got.Credentials[0].ID) != "credential-id" || string(got.ID) != string(user.ID) {
		t.Fatalf("reloaded user = %+v", got)
	}
}

func TestCredentialStoreRejectsCorruptOrUnknownFiles(t *testing.T) {
	for name, body := range map[string]string{
		"corrupt": `{`,
		"version": `{"version":99,"user":{"id":"YWJj","name":"owner","displayName":"Ariel Owner"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "auth.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := openCredentialStore(path); err == nil {
				t.Fatal("invalid credential file accepted")
			}
		})
	}
}

func TestCredentialStoreRejectsExistingFileReadableByOtherUsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	body := `{"version":1,"user":{"id":"YWJj","name":"owner","displayName":"Ariel Owner","credentials":[]}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := openCredentialStore(path); err == nil {
		t.Fatal("credentials file with permissive mode was accepted")
	}
}

func TestCredentialStoreAllowsOnlyOneBootstrapCredential(t *testing.T) {
	store, err := openCredentialStore(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.addBootstrap(lib.Credential{ID: []byte("first"), PublicKey: []byte("key")}); err != nil {
		t.Fatal(err)
	}
	if err := store.addBootstrap(lib.Credential{ID: []byte("second"), PublicKey: []byte("key")}); err == nil {
		t.Fatal("second bootstrap credential was accepted")
	}
}

func TestCredentialStoreUpdateDoesNotRegressConcurrentCredentialState(t *testing.T) {
	store, err := openCredentialStore(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	original := lib.Credential{
		ID:        []byte("credential-id"),
		PublicKey: []byte("public-key"),
		Authenticator: lib.Authenticator{
			SignCount:    10,
			CloneWarning: true,
		},
		Flags: lib.CredentialFlags{
			UserVerified:   true,
			BackupEligible: true,
			BackupState:    false,
		},
	}
	if err := store.add(original); err != nil {
		t.Fatal(err)
	}
	staleUpdate := original
	staleUpdate.Authenticator.SignCount = 5
	staleUpdate.Authenticator.CloneWarning = false
	staleUpdate.Flags.UserVerified = false
	staleUpdate.Flags.BackupState = true
	if err := store.update(staleUpdate); err != nil {
		t.Fatal(err)
	}

	got := store.snapshot().Credentials[0]
	if got.Authenticator.SignCount != 10 {
		t.Fatalf("sign count regressed to %d", got.Authenticator.SignCount)
	}
	if !got.Authenticator.CloneWarning {
		t.Fatal("clone warning was cleared by a stale update")
	}
	if !got.Flags.UserVerified {
		t.Fatal("latched user verification flag was cleared")
	}
	if !got.Flags.BackupState {
		t.Fatal("mutable backup state was not updated")
	}
}
