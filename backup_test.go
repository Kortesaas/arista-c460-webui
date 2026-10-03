package main

import (
	"errors"
	"testing"
)

func TestSecretsRoundTrip(t *testing.T) {
	enc, err := encryptSecrets("correct horse battery", map[string]string{"Office": "s3cret-pass"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := enc.decrypt("correct horse battery")
	if err != nil || got["Office"] != "s3cret-pass" {
		t.Fatalf("decrypt: %v %v", got, err)
	}
	if _, err := enc.decrypt("wrong passphrase"); !errors.Is(err, errWrongPassphrase) {
		t.Fatalf("wrong passphrase accepted: %v", err)
	}
	enc.Ciphertext[0] ^= 1
	if _, err := enc.decrypt("correct horse battery"); !errors.Is(err, errWrongPassphrase) {
		t.Fatalf("tampered ciphertext accepted: %v", err)
	}
}

func TestSecretsRejectsWeakParameters(t *testing.T) {
	enc, _ := encryptSecrets("correct horse battery", map[string]string{})
	enc.N = 2 // a crafted file must not make the key derivation trivial
	if _, err := enc.decrypt("correct horse battery"); err == nil {
		t.Fatal("weak scrypt parameters accepted")
	}
}

func TestPlanRestoreRejectsForeignFiles(t *testing.T) {
	a := &API{}
	if _, err := a.planRestore(restoreRequest{Backup: Backup{Format: "something-else", Version: 1}}); err == nil {
		t.Fatal("foreign file accepted")
	}
	if _, err := a.planRestore(restoreRequest{Backup: Backup{Format: backupFormat, Version: 2}}); err == nil {
		t.Fatal("future version accepted")
	}
}
