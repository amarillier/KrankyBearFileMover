package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

const (
	dbFileName = "connections.db"
	keyLength  = 32 // AES-256
	saltLength = 16
	iterations = 10000 // legacy PBKDF2 iteration count (v1 databases only)

	// argon2id parameters for v2 key derivation. 64 MB / 3 passes / 4 lanes is a
	// solid desktop default that comfortably exceeds OWASP minimums.
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB => 64 MB
	argonThreads = 4

	metaPasswordVerification = "password_verification"
	metaCryptoVersion        = "crypto_version"
	metaKDFSalt              = "kdf_salt"
	metaPasswordHint         = "password_hint"

	cryptoVersionV2 = "2"
)

type Connection struct {
	ID            int64
	Name          string
	Type          string // "sftp", "scp", "smb"
	Host          string
	Port          int
	Username      string
	Password      string // encrypted
	SSHKeyPath    string
	KeyPassphrase string // encrypted
	RemotePath    string
	// RemoteRsyncPath is passed to rsync as --rsync-path when non-empty (e.g. Cygwin: C:/cygwin64/bin/rsync.exe).
	RemoteRsyncPath string
	// SSHExtraArgs are appended to the ssh invoked by rsync (-e), e.g. -J user@jump or -o ProxyCommand=...
	SSHExtraArgs string
	// FileAgentTLSPin is the expected SHA-256 hex fingerprint of the agent's TLS certificate (type fileagent).
	FileAgentTLSPin string
}

type Database struct {
	db       *sql.DB
	key      []byte
	password string // master password held in memory for re-keying / keychain caching
	legacy   bool   // true when opened from a v1 database awaiting migration
}

// NewDatabase creates or opens the encrypted database.
//
// Key derivation depends on the crypto version recorded in the database:
//   - brand-new databases are initialised at v2 (argon2id + random per-install salt)
//   - existing v2 databases derive the key from the stored salt
//   - legacy v1 databases derive a temporary PBKDF2 key; the one-time upgrade to v2
//     (random salt, argon2id, full-field encryption) runs inside VerifyPassword once
//     the password is confirmed correct.
func NewDatabase(masterPassword string) (*Database, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	dbPath := filepath.Join(homeDir, ".filemover", dbFileName)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	d := &Database{
		db:       db,
		password: masterPassword,
	}

	if err := d.initTables(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}

	if err := d.setupCrypto(masterPassword); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize encryption: %w", err)
	}

	return d, nil
}

// deriveKeyLegacy reproduces the original v1 key derivation. It is only used to
// read a legacy database long enough to migrate it to v2.
func deriveKeyLegacy(password string) []byte {
	salt := []byte("filemover-salt-v1")
	return pbkdf2.Key([]byte(password), salt, iterations, keyLength, sha256.New)
}

// deriveKeyV2 derives the AES-256 key using argon2id and the per-install salt
// stored alongside the database (so the database stays portable).
func deriveKeyV2(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, keyLength)
}

func (d *Database) initTables() error {
	// Create connections table
	connectionsQuery := `
	CREATE TABLE IF NOT EXISTS connections (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		host TEXT NOT NULL,
		port INTEGER NOT NULL,
		username TEXT NOT NULL,
		password_encrypted TEXT,
		ssh_key_path TEXT,
		key_passphrase_encrypted TEXT,
		remote_path TEXT,
		remote_rsync_path TEXT,
		ssh_extra_args TEXT,
		fileagent_tls_pin_encrypted TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)
	`
	if _, err := d.db.Exec(connectionsQuery); err != nil {
		return err
	}

	// Create metadata table
	metadataQuery := `
	CREATE TABLE IF NOT EXISTS metadata (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)
	`
	if _, err := d.db.Exec(metadataQuery); err != nil {
		return err
	}

	return d.migrateConnectionsSchema()
}

func (d *Database) migrateConnectionsSchema() error {
	for _, stmt := range []string{
		`ALTER TABLE connections ADD COLUMN remote_rsync_path TEXT`,
		`ALTER TABLE connections ADD COLUMN ssh_extra_args TEXT`,
		`ALTER TABLE connections ADD COLUMN fileagent_tls_pin_encrypted TEXT`,
	} {
		if _, err := d.db.Exec(stmt); err != nil {
			low := strings.ToLower(err.Error())
			if !strings.Contains(low, "duplicate column") {
				return err
			}
		}
	}
	return nil
}

// setupCrypto establishes d.key based on the database's crypto version. For a
// brand-new database it initialises v2 state; for a legacy database it derives
// the legacy key and flags the database for migration on VerifyPassword.
func (d *Database) setupCrypto(password string) error {
	version, _ := d.getMeta(metaCryptoVersion)

	if version == cryptoVersionV2 {
		saltB64, ok := d.getMeta(metaKDFSalt)
		if !ok {
			return errors.New("crypto v2 database is missing its key salt")
		}
		salt, err := base64.StdEncoding.DecodeString(saltB64)
		if err != nil {
			return fmt.Errorf("invalid stored key salt: %w", err)
		}
		d.key = deriveKeyV2(password, salt)
		return nil
	}

	// No v2 marker: either a legacy v1 database or a brand-new one.
	if d.metaExists(metaPasswordVerification) {
		// Legacy database. Derive the old key so VerifyPassword can confirm the
		// password and then migrate everything to v2.
		d.key = deriveKeyLegacy(password)
		d.legacy = true
		return nil
	}

	// Brand-new database: initialise v2 crypto state.
	salt := make([]byte, saltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	d.key = deriveKeyV2(password, salt)
	if err := d.setMeta(metaKDFSalt, base64.StdEncoding.EncodeToString(salt)); err != nil {
		return err
	}
	if err := d.setMeta(metaCryptoVersion, cryptoVersionV2); err != nil {
		return err
	}
	token, err := d.encrypt("verified")
	if err != nil {
		return err
	}
	return d.setMeta(metaPasswordVerification, token)
}

func (d *Database) getMeta(key string) (string, bool) {
	var v string
	if err := d.db.QueryRow("SELECT value FROM metadata WHERE key = ?", key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func (d *Database) metaExists(key string) bool {
	_, ok := d.getMeta(key)
	return ok
}

func (d *Database) setMeta(key, value string) error {
	_, err := d.db.Exec("INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)", key, value)
	return err
}

func (d *Database) encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(d.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (d *Database) decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(d.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// decryptOrRaw decrypts a stored value, falling back to the raw value if it does
// not decrypt. This keeps the app resilient to any field that predates full-field
// encryption (e.g. mid-migration), without ever crashing on read.
func (d *Database) decryptOrRaw(value string) string {
	if value == "" {
		return ""
	}
	if v, err := d.decrypt(value); err == nil {
		return v
	}
	return value
}

func (d *Database) SaveConnection(conn *Connection) error {
	encName, err := d.encrypt(conn.Name)
	if err != nil {
		return fmt.Errorf("failed to encrypt name: %w", err)
	}
	encHost, err := d.encrypt(conn.Host)
	if err != nil {
		return fmt.Errorf("failed to encrypt host: %w", err)
	}
	encUsername, err := d.encrypt(conn.Username)
	if err != nil {
		return fmt.Errorf("failed to encrypt username: %w", err)
	}
	encPassword, err := d.encrypt(conn.Password)
	if err != nil {
		return fmt.Errorf("failed to encrypt password: %w", err)
	}
	encKeyPath, err := d.encrypt(conn.SSHKeyPath)
	if err != nil {
		return fmt.Errorf("failed to encrypt ssh key path: %w", err)
	}
	encPassphrase, err := d.encrypt(conn.KeyPassphrase)
	if err != nil {
		return fmt.Errorf("failed to encrypt passphrase: %w", err)
	}
	encRemotePath, err := d.encrypt(conn.RemotePath)
	if err != nil {
		return fmt.Errorf("failed to encrypt remote path: %w", err)
	}
	encRsyncPath, err := d.encrypt(conn.RemoteRsyncPath)
	if err != nil {
		return fmt.Errorf("failed to encrypt remote rsync path: %w", err)
	}
	encExtraArgs, err := d.encrypt(conn.SSHExtraArgs)
	if err != nil {
		return fmt.Errorf("failed to encrypt ssh extra args: %w", err)
	}
	encPIN, err := d.encrypt(conn.FileAgentTLSPin)
	if err != nil {
		return fmt.Errorf("failed to encrypt file agent TLS pin: %w", err)
	}

	if conn.ID == 0 {
		// Insert new connection
		query := `
		INSERT INTO connections (name, type, host, port, username, password_encrypted, ssh_key_path, key_passphrase_encrypted, remote_path, remote_rsync_path, ssh_extra_args, fileagent_tls_pin_encrypted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`
		result, err := d.db.Exec(query, encName, conn.Type, encHost, conn.Port, encUsername,
			encPassword, encKeyPath, encPassphrase, encRemotePath, encRsyncPath, encExtraArgs, encPIN)
		if err != nil {
			return err
		}
		conn.ID, _ = result.LastInsertId()
	} else {
		// Update existing connection
		query := `
		UPDATE connections
		SET name = ?, type = ?, host = ?, port = ?, username = ?, password_encrypted = ?,
		    ssh_key_path = ?, key_passphrase_encrypted = ?, remote_path = ?, remote_rsync_path = ?, ssh_extra_args = ?, fileagent_tls_pin_encrypted = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
		`
		_, err := d.db.Exec(query, encName, conn.Type, encHost, conn.Port, encUsername,
			encPassword, encKeyPath, encPassphrase, encRemotePath, encRsyncPath, encExtraArgs, encPIN, conn.ID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Database) GetConnections() ([]*Connection, error) {
	query := `SELECT id, name, type, host, port, username, password_encrypted, ssh_key_path, key_passphrase_encrypted, remote_path, IFNULL(remote_rsync_path, ''), IFNULL(ssh_extra_args, ''), IFNULL(fileagent_tls_pin_encrypted, '') FROM connections`
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var connections []*Connection
	for rows.Next() {
		conn, err := d.scanConnection(rows.Scan)
		if err != nil {
			continue
		}
		connections = append(connections, conn)
	}

	// Sort by (decrypted) name in memory; name is encrypted at rest so SQL cannot order it.
	sortConnectionsByName(connections)

	return connections, rows.Err()
}

func (d *Database) GetConnection(id int64) (*Connection, error) {
	query := `SELECT id, name, type, host, port, username, password_encrypted, ssh_key_path, key_passphrase_encrypted, remote_path, IFNULL(remote_rsync_path, ''), IFNULL(ssh_extra_args, ''), IFNULL(fileagent_tls_pin_encrypted, '') FROM connections WHERE id = ?`
	row := d.db.QueryRow(query, id)
	return d.scanConnection(row.Scan)
}

// scanConnection decrypts a connection row read by either Query or QueryRow.
func (d *Database) scanConnection(scan func(dest ...any) error) (*Connection, error) {
	conn := &Connection{}
	var encName, encHost, encUsername, encPassword, encKeyPath, encPassphrase, encRemotePath, encRsyncPath, encExtraArgs, encPIN string
	if err := scan(&conn.ID, &encName, &conn.Type, &encHost, &conn.Port, &encUsername,
		&encPassword, &encKeyPath, &encPassphrase, &encRemotePath, &encRsyncPath, &encExtraArgs, &encPIN); err != nil {
		return nil, err
	}

	conn.Name = d.decryptOrRaw(encName)
	conn.Host = d.decryptOrRaw(encHost)
	conn.Username = d.decryptOrRaw(encUsername)
	conn.Password = d.decryptOrRaw(encPassword)
	conn.SSHKeyPath = d.decryptOrRaw(encKeyPath)
	conn.KeyPassphrase = d.decryptOrRaw(encPassphrase)
	conn.RemotePath = d.decryptOrRaw(encRemotePath)
	conn.RemoteRsyncPath = d.decryptOrRaw(encRsyncPath)
	conn.SSHExtraArgs = d.decryptOrRaw(encExtraArgs)
	conn.FileAgentTLSPin = d.decryptOrRaw(encPIN)
	return conn, nil
}

func sortConnectionsByName(connections []*Connection) {
	for i := 1; i < len(connections); i++ {
		for j := i; j > 0 && strings.ToLower(connections[j-1].Name) > strings.ToLower(connections[j].Name); j-- {
			connections[j-1], connections[j] = connections[j], connections[j-1]
		}
	}
}

func (d *Database) DeleteConnection(id int64) error {
	query := `DELETE FROM connections WHERE id = ?`
	_, err := d.db.Exec(query, id)
	return err
}

// VerifyPassword confirms the master password decrypts the verification token.
// When opened from a legacy (v1) database, a correct password additionally
// triggers the one-time migration to v2 (random salt, argon2id, full-field
// encryption).
func (d *Database) VerifyPassword() error {
	verificationToken, ok := d.getMeta(metaPasswordVerification)
	if !ok {
		// No verification token (e.g. an old database without metadata). Create
		// one now using the current key so subsequent opens can verify.
		if token, err := d.encrypt("verified"); err == nil {
			d.setMeta(metaPasswordVerification, token)
		}
		return nil
	}

	decrypted, err := d.decrypt(verificationToken)
	if err != nil || decrypted != "verified" {
		return fmt.Errorf("invalid master password")
	}

	if d.legacy {
		if err := d.migrateToV2(); err != nil {
			return fmt.Errorf("failed to upgrade database encryption: %w", err)
		}
	}

	return nil
}

// migrateToV2 rewrites a legacy database in place: every sensitive field is
// re-encrypted under a freshly derived argon2id key with a new random salt. The
// whole rewrite runs in a single transaction so an interrupted migration leaves
// the original v1 data intact.
func (d *Database) migrateToV2() error {
	// Read existing rows, decrypting only the fields that were encrypted in v1.
	conns, err := d.readConnectionsRawLegacy()
	if err != nil {
		return err
	}

	salt := make([]byte, saltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}

	legacyKey := d.key
	newKey := deriveKeyV2(d.password, salt)

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
			d.key = legacyKey // restore so the handle stays usable on failure
		}
	}()

	// Switch the active key so d.encrypt produces v2 ciphertext.
	d.key = newKey

	for _, c := range conns {
		encName, _ := d.encrypt(c.Name)
		encHost, _ := d.encrypt(c.Host)
		encUser, _ := d.encrypt(c.Username)
		encPass, _ := d.encrypt(c.Password)
		encKeyPath, _ := d.encrypt(c.SSHKeyPath)
		encPhrase, _ := d.encrypt(c.KeyPassphrase)
		encRemote, _ := d.encrypt(c.RemotePath)
		encRsync, _ := d.encrypt(c.RemoteRsyncPath)
		encArgs, _ := d.encrypt(c.SSHExtraArgs)
		encPIN, _ := d.encrypt(c.FileAgentTLSPin)

		if _, err := tx.Exec(`UPDATE connections SET name=?, host=?, username=?, password_encrypted=?, ssh_key_path=?, key_passphrase_encrypted=?, remote_path=?, remote_rsync_path=?, ssh_extra_args=?, fileagent_tls_pin_encrypted=? WHERE id=?`,
			encName, encHost, encUser, encPass, encKeyPath, encPhrase, encRemote, encRsync, encArgs, encPIN, c.ID); err != nil {
			return err
		}
	}

	token, err := d.encrypt("verified")
	if err != nil {
		return err
	}
	for _, kv := range [][2]string{
		{metaPasswordVerification, token},
		{metaKDFSalt, base64.StdEncoding.EncodeToString(salt)},
		{metaCryptoVersion, cryptoVersionV2},
	} {
		if _, err := tx.Exec("INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)", kv[0], kv[1]); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	d.legacy = false
	return nil
}

// readConnectionsRawLegacy reads connection rows from a v1 database, where only
// password, key passphrase, and TLS pin were encrypted; the remaining fields are
// plaintext and copied as-is.
func (d *Database) readConnectionsRawLegacy() ([]*Connection, error) {
	query := `SELECT id, name, type, host, port, username, password_encrypted, ssh_key_path, key_passphrase_encrypted, remote_path, IFNULL(remote_rsync_path, ''), IFNULL(ssh_extra_args, ''), IFNULL(fileagent_tls_pin_encrypted, '') FROM connections`
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var connections []*Connection
	for rows.Next() {
		conn := &Connection{}
		var encPassword, encPassphrase, encPIN string
		if err := rows.Scan(&conn.ID, &conn.Name, &conn.Type, &conn.Host, &conn.Port, &conn.Username,
			&encPassword, &conn.SSHKeyPath, &encPassphrase, &conn.RemotePath, &conn.RemoteRsyncPath, &conn.SSHExtraArgs, &encPIN); err != nil {
			continue
		}
		conn.Password, _ = d.decrypt(encPassword)
		conn.KeyPassphrase, _ = d.decrypt(encPassphrase)
		conn.FileAgentTLSPin, _ = d.decrypt(encPIN)
		connections = append(connections, conn)
	}

	return connections, rows.Err()
}

// UpdateVerificationToken updates the password verification token with the current database key
func (d *Database) UpdateVerificationToken() error {
	verificationToken, err := d.encrypt("verified")
	if err != nil {
		return fmt.Errorf("failed to encrypt verification token: %w", err)
	}
	if err := d.setMeta(metaPasswordVerification, verificationToken); err != nil {
		return fmt.Errorf("failed to update verification token: %w", err)
	}
	return nil
}

// MasterPassword returns the master password held in memory. Used to refresh the
// OS keychain entry when the "remember password" preference is enabled.
func (d *Database) MasterPassword() string {
	return d.password
}

// GetPasswordHint returns the user-authored, plaintext master password hint (or "").
func (d *Database) GetPasswordHint() string {
	v, _ := d.getMeta(metaPasswordHint)
	return v
}

// SetPasswordHint stores (or clears, when blank) the plaintext master password hint.
func (d *Database) SetPasswordHint(hint string) error {
	if strings.TrimSpace(hint) == "" {
		_, err := d.db.Exec("DELETE FROM metadata WHERE key = ?", metaPasswordHint)
		return err
	}
	return d.setMeta(metaPasswordHint, hint)
}

// ReadPasswordHint reads the master password hint directly from the database file
// without needing the master password. Used by the login dialog to surface the
// hint after repeated failed attempts.
func ReadPasswordHint() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dbPath := filepath.Join(homeDir, ".filemover", dbFileName)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return ""
	}
	defer db.Close()

	var v string
	if err := db.QueryRow("SELECT value FROM metadata WHERE key = ?", metaPasswordHint).Scan(&v); err != nil {
		return ""
	}
	return v
}

func (d *Database) Close() error {
	return d.db.Close()
}
