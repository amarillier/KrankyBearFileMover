package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// useTempHome points os.UserHomeDir at a throwaway directory so tests never
// touch the user's real ~/.filemover/connections.db.
func useTempHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // Windows
	return tmp
}

func sampleConnection() *Connection {
	return &Connection{
		Name:            "prod box",
		Type:            "sftp",
		Host:            "files.example.com",
		Port:            22,
		Username:        "deploy",
		Password:        "s3cr3t",
		SSHKeyPath:      "~/.ssh/id_ed25519",
		KeyPassphrase:   "phrase",
		RemotePath:      "/srv/incoming",
		RemoteRsyncPath: "/usr/bin/rsync",
		SSHExtraArgs:    "-J jump@bastion.example.com",
		FileAgentTLSPin: "abc123",
	}
}

func assertConnEqual(t *testing.T, got, want *Connection) {
	t.Helper()
	if got.Name != want.Name || got.Host != want.Host || got.Username != want.Username ||
		got.Password != want.Password || got.SSHKeyPath != want.SSHKeyPath ||
		got.KeyPassphrase != want.KeyPassphrase || got.RemotePath != want.RemotePath ||
		got.RemoteRsyncPath != want.RemoteRsyncPath || got.SSHExtraArgs != want.SSHExtraArgs ||
		got.FileAgentTLSPin != want.FileAgentTLSPin || got.Port != want.Port || got.Type != want.Type {
		t.Fatalf("connection mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestNewDatabaseRoundTrip(t *testing.T) {
	useTempHome(t)
	const pw = "correct horse"

	db, err := NewDatabase(pw)
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	if err := db.VerifyPassword(); err != nil {
		t.Fatalf("VerifyPassword (new db): %v", err)
	}
	conn := sampleConnection()
	if err := db.SaveConnection(conn); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}
	db.Close()

	// Reopen with the correct password and confirm everything decrypts.
	db2, err := NewDatabase(pw)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	if err := db2.VerifyPassword(); err != nil {
		t.Fatalf("VerifyPassword (reopen): %v", err)
	}
	got, err := db2.GetConnection(conn.ID)
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	assertConnEqual(t, got, sampleConnection())
}

func TestWrongPasswordRejected(t *testing.T) {
	useTempHome(t)
	db, err := NewDatabase("right")
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	db.Close()

	bad, err := NewDatabase("wrong")
	if err != nil {
		t.Fatalf("NewDatabase wrong: %v", err)
	}
	defer bad.Close()
	if err := bad.VerifyPassword(); err == nil {
		t.Fatal("expected VerifyPassword to reject the wrong password")
	}
}

func TestFieldsEncryptedAtRest(t *testing.T) {
	home := useTempHome(t)
	db, err := NewDatabase("pw")
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	if err := db.VerifyPassword(); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	conn := sampleConnection()
	if err := db.SaveConnection(conn); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}
	db.Close()

	// Inspect the raw row: sensitive columns must not contain the plaintext.
	raw, err := sql.Open("sqlite3", filepath.Join(home, ".filemover", dbFileName))
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	defer raw.Close()

	var name, host, username, sshArgs string
	if err := raw.QueryRow("SELECT name, host, username, ssh_extra_args FROM connections WHERE id = ?", conn.ID).
		Scan(&name, &host, &username, &sshArgs); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if host == "files.example.com" {
		t.Error("host stored in plaintext")
	}
	if name == "prod box" {
		t.Error("name stored in plaintext")
	}
	if username == "deploy" {
		t.Error("username stored in plaintext")
	}
	if sshArgs == "-J jump@bastion.example.com" {
		t.Error("ssh_extra_args stored in plaintext")
	}
}

// TestLegacyMigration builds a v1-style database (PBKDF2 key, only the three
// secret fields encrypted, plaintext metadata, no crypto_version) and confirms
// opening it migrates to v2 transparently while preserving all data.
func TestLegacyMigration(t *testing.T) {
	home := useTempHome(t)
	const pw = "legacy-pw"
	if err := os.MkdirAll(filepath.Join(home, ".filemover"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dbPath := filepath.Join(home, ".filemover", dbFileName)

	// Construct the legacy database by hand.
	if err := func() error {
		raw, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			return err
		}
		defer raw.Close()
		legacy := &Database{db: raw, key: deriveKeyLegacy(pw)}
		if err := legacy.initTables(); err != nil {
			return err
		}
		// v1 verification token + legacy-encrypted secret fields, plaintext rest.
		token, _ := legacy.encrypt("verified")
		if _, err := raw.Exec("INSERT INTO metadata (key, value) VALUES (?, ?)", metaPasswordVerification, token); err != nil {
			return err
		}
		c := sampleConnection()
		encPass, _ := legacy.encrypt(c.Password)
		encPhrase, _ := legacy.encrypt(c.KeyPassphrase)
		encPIN, _ := legacy.encrypt(c.FileAgentTLSPin)
		_, err = raw.Exec(`INSERT INTO connections (name, type, host, port, username, password_encrypted, ssh_key_path, key_passphrase_encrypted, remote_path, remote_rsync_path, ssh_extra_args, fileagent_tls_pin_encrypted)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.Name, c.Type, c.Host, c.Port, c.Username, encPass, c.SSHKeyPath, encPhrase, c.RemotePath, c.RemoteRsyncPath, c.SSHExtraArgs, encPIN)
		return err
	}(); err != nil {
		t.Fatalf("build legacy db: %v", err)
	}

	wantHomeMustHaveNoVersion(t, dbPath) // sanity: starts as legacy

	// Open via the normal path; VerifyPassword should migrate to v2.
	db, err := NewDatabase(pw)
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	if !db.legacy {
		t.Fatal("expected database to be flagged legacy before verify")
	}
	if err := db.VerifyPassword(); err != nil {
		t.Fatalf("VerifyPassword/migrate: %v", err)
	}
	if db.legacy {
		t.Fatal("database still flagged legacy after migration")
	}
	conns, err := db.GetConnections()
	if err != nil {
		t.Fatalf("GetConnections: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	assertConnEqual(t, conns[0], sampleConnection())
	db.Close()

	// crypto_version must now be v2 and host must be encrypted at rest.
	raw, _ := sql.Open("sqlite3", dbPath)
	defer raw.Close()
	var version, host string
	if err := raw.QueryRow("SELECT value FROM metadata WHERE key = ?", metaCryptoVersion).Scan(&version); err != nil {
		t.Fatalf("read crypto_version: %v", err)
	}
	if version != cryptoVersionV2 {
		t.Fatalf("crypto_version = %q, want %q", version, cryptoVersionV2)
	}
	if err := raw.QueryRow("SELECT host FROM connections LIMIT 1").Scan(&host); err != nil {
		t.Fatalf("read host: %v", err)
	}
	if host == "files.example.com" {
		t.Error("host still plaintext after migration")
	}

	// Reopen with the same password (now via the v2 path) and confirm it works.
	db2, err := NewDatabase(pw)
	if err != nil {
		t.Fatalf("reopen v2: %v", err)
	}
	defer db2.Close()
	if err := db2.VerifyPassword(); err != nil {
		t.Fatalf("VerifyPassword after migration: %v", err)
	}
}

func wantHomeMustHaveNoVersion(t *testing.T, dbPath string) {
	t.Helper()
	raw, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer raw.Close()
	var v string
	err = raw.QueryRow("SELECT value FROM metadata WHERE key = ?", metaCryptoVersion).Scan(&v)
	if err == nil {
		t.Fatalf("legacy db unexpectedly has crypto_version=%q", v)
	}
}

func TestPasswordHint(t *testing.T) {
	useTempHome(t)
	db, err := NewDatabase("pw")
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	if err := db.VerifyPassword(); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if err := db.SetPasswordHint("favourite city"); err != nil {
		t.Fatalf("SetPasswordHint: %v", err)
	}
	db.Close()

	// ReadPasswordHint must work without the master password.
	if got := ReadPasswordHint(); got != "favourite city" {
		t.Fatalf("ReadPasswordHint = %q, want %q", got, "favourite city")
	}
}
