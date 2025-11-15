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

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/pbkdf2"
)

const (
	dbFileName = "connections.db"
	keyLength  = 32 // AES-256
	saltLength = 16
	iterations = 10000
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
}

type Database struct {
	db  *sql.DB
	key []byte
}

// NewDatabase creates or opens the encrypted database
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
		db:  db,
		key: deriveKey(masterPassword),
	}

	if err := d.initTables(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}

	return d, nil
}

func deriveKey(password string) []byte {
	salt := []byte("filemover-salt-v1") // In production, store salt separately
	return pbkdf2.Key([]byte(password), salt, iterations, keyLength, sha256.New)
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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)
	`
	_, err := d.db.Exec(connectionsQuery)
	if err != nil {
		return err
	}
	
	// Create metadata table
	metadataQuery := `
	CREATE TABLE IF NOT EXISTS metadata (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)
	`
	_, err = d.db.Exec(metadataQuery)
	if err != nil {
		return err
	}
	
	// Store a verification token if it doesn't exist
	var count int
	err = d.db.QueryRow("SELECT COUNT(*) FROM metadata WHERE key = 'password_verification'").Scan(&count)
	if err == nil && count == 0 {
		verificationToken, err := d.encrypt("verified")
		if err == nil {
			_, err = d.db.Exec("INSERT OR IGNORE INTO metadata (key, value) VALUES ('password_verification', ?)", verificationToken)
			if err != nil {
				return fmt.Errorf("failed to create verification token: %w", err)
			}
		}
	}
	
	return nil
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

func (d *Database) SaveConnection(conn *Connection) error {
	encryptedPassword, err := d.encrypt(conn.Password)
	if err != nil {
		return fmt.Errorf("failed to encrypt password: %w", err)
	}

	encryptedPassphrase, err := d.encrypt(conn.KeyPassphrase)
	if err != nil {
		return fmt.Errorf("failed to encrypt passphrase: %w", err)
	}

	if conn.ID == 0 {
		// Insert new connection
		query := `
		INSERT INTO connections (name, type, host, port, username, password_encrypted, ssh_key_path, key_passphrase_encrypted, remote_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`
		result, err := d.db.Exec(query, conn.Name, conn.Type, conn.Host, conn.Port, conn.Username,
			encryptedPassword, conn.SSHKeyPath, encryptedPassphrase, conn.RemotePath)
		if err != nil {
			return err
		}
		conn.ID, _ = result.LastInsertId()
	} else {
		// Update existing connection
		query := `
		UPDATE connections 
		SET name = ?, type = ?, host = ?, port = ?, username = ?, password_encrypted = ?, 
		    ssh_key_path = ?, key_passphrase_encrypted = ?, remote_path = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
		`
		_, err := d.db.Exec(query, conn.Name, conn.Type, conn.Host, conn.Port, conn.Username,
			encryptedPassword, conn.SSHKeyPath, encryptedPassphrase, conn.RemotePath, conn.ID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Database) GetConnections() ([]*Connection, error) {
	query := `SELECT id, name, type, host, port, username, password_encrypted, ssh_key_path, key_passphrase_encrypted, remote_path FROM connections ORDER BY name`
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var connections []*Connection
	for rows.Next() {
		conn := &Connection{}
		var encryptedPassword, encryptedPassphrase string

		err := rows.Scan(&conn.ID, &conn.Name, &conn.Type, &conn.Host, &conn.Port, &conn.Username,
			&encryptedPassword, &conn.SSHKeyPath, &encryptedPassphrase, &conn.RemotePath)
		if err != nil {
			continue
		}

		conn.Password, _ = d.decrypt(encryptedPassword)
		conn.KeyPassphrase, _ = d.decrypt(encryptedPassphrase)
		connections = append(connections, conn)
	}

	return connections, rows.Err()
}

func (d *Database) GetConnection(id int64) (*Connection, error) {
	query := `SELECT id, name, type, host, port, username, password_encrypted, ssh_key_path, key_passphrase_encrypted, remote_path FROM connections WHERE id = ?`
	row := d.db.QueryRow(query, id)

	conn := &Connection{}
	var encryptedPassword, encryptedPassphrase string

	err := row.Scan(&conn.ID, &conn.Name, &conn.Type, &conn.Host, &conn.Port, &conn.Username,
		&encryptedPassword, &conn.SSHKeyPath, &encryptedPassphrase, &conn.RemotePath)
	if err != nil {
		return nil, err
	}

	conn.Password, _ = d.decrypt(encryptedPassword)
	conn.KeyPassphrase, _ = d.decrypt(encryptedPassphrase)

	return conn, nil
}

func (d *Database) DeleteConnection(id int64) error {
	query := `DELETE FROM connections WHERE id = ?`
	_, err := d.db.Exec(query, id)
	return err
}

func (d *Database) VerifyPassword() error {
	var verificationToken string
	err := d.db.QueryRow("SELECT value FROM metadata WHERE key = 'password_verification'").Scan(&verificationToken)
	if err != nil {
		// Check if it's "no rows" error (table exists but no token) vs table doesn't exist
		if err == sql.ErrNoRows {
			// No verification token exists - this is OK for new databases
			// Create one now for future verification
			verificationToken, err := d.encrypt("verified")
			if err == nil {
				d.db.Exec("INSERT OR IGNORE INTO metadata (key, value) VALUES ('password_verification', ?)", verificationToken)
			}
			return nil
		}
		// If table doesn't exist or other error, assume it's an old database without metadata table
		// This is OK - we'll create the token on next save
		return nil
	}
	
	decrypted, err := d.decrypt(verificationToken)
	if err != nil || decrypted != "verified" {
		return fmt.Errorf("invalid master password")
	}
	
	return nil
}

// UpdateVerificationToken updates the password verification token with the current database key
func (d *Database) UpdateVerificationToken() error {
	verificationToken, err := d.encrypt("verified")
	if err != nil {
		return fmt.Errorf("failed to encrypt verification token: %w", err)
	}
	_, err = d.db.Exec("UPDATE metadata SET value = ? WHERE key = 'password_verification'", verificationToken)
	if err != nil {
		// If update fails, try insert (in case token doesn't exist)
		_, err = d.db.Exec("INSERT OR REPLACE INTO metadata (key, value) VALUES ('password_verification', ?)", verificationToken)
		if err != nil {
			return fmt.Errorf("failed to update verification token: %w", err)
		}
	}
	return nil
}

func (d *Database) Close() error {
	return d.db.Close()
}
