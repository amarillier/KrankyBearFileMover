package main

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// The master password may optionally be cached in the OS keychain (macOS
// Keychain, Windows Credential Manager, Linux Secret Service) so the user is not
// prompted on every launch. The keychain only ever holds the master password;
// the encrypted database remains the single source of truth and stays portable.
const (
	keychainService = "KrankyBear FileMover"
	keychainAccount = "master-password"
)

// keychainGetMasterPassword returns the cached master password and whether one
// was found. A missing entry (or any keychain error, e.g. no Secret Service
// daemon on a headless Linux box) is reported as "not found" so callers cleanly
// fall back to prompting.
func keychainGetMasterPassword() (string, bool) {
	pw, err := keyring.Get(keychainService, keychainAccount)
	if err != nil {
		return "", false
	}
	return pw, true
}

// keychainSetMasterPassword stores (or refreshes) the cached master password.
func keychainSetMasterPassword(password string) error {
	return keyring.Set(keychainService, keychainAccount, password)
}

// keychainDeleteMasterPassword removes the cached master password. A missing
// entry is treated as success.
func keychainDeleteMasterPassword() error {
	if err := keyring.Delete(keychainService, keychainAccount); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}
