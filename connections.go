package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/hirochachacha/go-smb2"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type FileInfo struct {
	Name    string
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
	IsDir   bool
}

type ConnectionManager interface {
	Connect() error
	Disconnect() error
	List(path string) ([]FileInfo, error)
	ReadFile(path string) (io.ReadCloser, error)
	WriteFile(path string, data io.Reader) error
	Mkdir(path string) error
	Remove(path string) error
	Rename(oldpath, newpath string) error
	GetCurrentPath() string
	SetCurrentPath(path string)
}

// SFTPConnection handles SFTP/SCP connections
type SFTPConnection struct {
	conn     *Connection
	sshConn  *ssh.Client
	sftpConn *sftp.Client
	scpConn  *ssh.Client
	currentPath string
}

func NewSFTPConnection(conn *Connection) *SFTPConnection {
	return &SFTPConnection{
		conn:        conn,
		currentPath: conn.RemotePath,
	}
}

func (s *SFTPConnection) Connect() error {
	config := &ssh.ClientConfig{
		User:            s.conn.Username,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // In production, use proper host key verification
		Timeout:         10 * time.Second,
	}

	// Try SSH key authentication first if provided
	if s.conn.SSHKeyPath != "" {
		key, err := os.ReadFile(s.conn.SSHKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read SSH key: %w", err)
		}

		var signer ssh.Signer
		if s.conn.KeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(s.conn.KeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return fmt.Errorf("failed to parse SSH key: %w", err)
		}

		config.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	}

	// Add password authentication if provided
	if s.conn.Password != "" {
		config.Auth = append(config.Auth, ssh.Password(s.conn.Password))
	}

	if len(config.Auth) == 0 {
		return fmt.Errorf("no authentication method provided")
	}

	addr := fmt.Sprintf("%s:%d", s.conn.Host, s.conn.Port)
	if s.conn.Port == 0 {
		addr = fmt.Sprintf("%s:22", s.conn.Host)
	}

	sshConn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	sftpConn, err := sftp.NewClient(sshConn)
	if err != nil {
		sshConn.Close()
		return fmt.Errorf("failed to create SFTP client: %w", err)
	}

	s.sshConn = sshConn
	s.sftpConn = sftpConn
	s.scpConn = sshConn // Reuse SSH connection for SCP

	return nil
}

func (s *SFTPConnection) Disconnect() error {
	var errs []error
	if s.sftpConn != nil {
		if err := s.sftpConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.sshConn != nil {
		if err := s.sshConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors disconnecting: %v", errs)
	}
	return nil
}

func (s *SFTPConnection) List(path string) ([]FileInfo, error) {
	if s.sftpConn == nil {
		return nil, fmt.Errorf("not connected")
	}

	if path == "" {
		path = s.currentPath
	}

	files, err := s.sftpConn.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var fileInfos []FileInfo
	for _, file := range files {
		fileInfos = append(fileInfos, FileInfo{
			Name:    file.Name(),
			Size:    file.Size(),
			Mode:    file.Mode(),
			ModTime: file.ModTime(),
			IsDir:   file.IsDir(),
		})
	}

	return fileInfos, nil
}

func (s *SFTPConnection) ReadFile(path string) (io.ReadCloser, error) {
	if s.sftpConn == nil {
		return nil, fmt.Errorf("not connected")
	}

	return s.sftpConn.Open(path)
}

func (s *SFTPConnection) WriteFile(path string, data io.Reader) error {
	if s.sftpConn == nil {
		return fmt.Errorf("not connected")
	}

	file, err := s.sftpConn.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, data)
	return err
}

func (s *SFTPConnection) Mkdir(path string) error {
	if s.sftpConn == nil {
		return fmt.Errorf("not connected")
	}

	return s.sftpConn.MkdirAll(path)
}

func (s *SFTPConnection) Remove(path string) error {
	if s.sftpConn == nil {
		return fmt.Errorf("not connected")
	}

	info, err := s.sftpConn.Stat(path)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return s.sftpConn.RemoveDirectory(path)
	}
	return s.sftpConn.Remove(path)
}

func (s *SFTPConnection) Rename(oldpath, newpath string) error {
	if s.sftpConn == nil {
		return fmt.Errorf("not connected")
	}

	return s.sftpConn.Rename(oldpath, newpath)
}

func (s *SFTPConnection) GetCurrentPath() string {
	return s.currentPath
}

func (s *SFTPConnection) SetCurrentPath(path string) {
	s.currentPath = path
}

// SMBConnection handles SMB/CIFS connections
type SMBConnection struct {
	conn        *Connection
	smbConn     *smb2.Share
	session     *smb2.Session
	netConn     net.Conn
	currentPath string
}

func NewSMBConnection(conn *Connection) *SMBConnection {
	return &SMBConnection{
		conn:        conn,
		currentPath: conn.RemotePath,
	}
}

func (s *SMBConnection) Connect() error {
	addr := fmt.Sprintf("%s:%d", s.conn.Host, s.conn.Port)
	if s.conn.Port == 0 {
		addr = fmt.Sprintf("%s:445", s.conn.Host) // Default SMB port
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to connect to SMB server: %w", err)
	}

	dialer := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     s.conn.Username,
			Password: s.conn.Password,
		},
	}

	session, err := dialer.Dial(conn)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to dial SMB: %w", err)
	}

	// Try to mount the share (default to first share or use remote_path)
	shareName := "C$" // Default Windows share
	if s.conn.RemotePath != "" {
		shareName = s.conn.RemotePath
	}

	share, err := session.Mount(shareName)
	if err != nil {
		session.Logoff()
		conn.Close()
		return fmt.Errorf("failed to mount share: %w", err)
	}

	s.smbConn = share
	s.session = session
	s.netConn = conn

	return nil
}

func (s *SMBConnection) Disconnect() error {
	var errs []error
	if s.smbConn != nil {
		if err := s.smbConn.Umount(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.session != nil {
		if err := s.session.Logoff(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.netConn != nil {
		if err := s.netConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors disconnecting: %v", errs)
	}
	return nil
}

func (s *SMBConnection) List(path string) ([]FileInfo, error) {
	if s.smbConn == nil {
		return nil, fmt.Errorf("not connected")
	}

	if path == "" {
		path = s.currentPath
	}

	files, err := s.smbConn.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var fileInfos []FileInfo
	for _, file := range files {
		fileInfos = append(fileInfos, FileInfo{
			Name:    file.Name(),
			Size:    file.Size(),
			Mode:    file.Mode(),
			ModTime: file.ModTime(),
			IsDir:   file.IsDir(),
		})
	}

	return fileInfos, nil
}

func (s *SMBConnection) ReadFile(path string) (io.ReadCloser, error) {
	if s.smbConn == nil {
		return nil, fmt.Errorf("not connected")
	}

	return s.smbConn.Open(path)
}

func (s *SMBConnection) WriteFile(path string, data io.Reader) error {
	if s.smbConn == nil {
		return fmt.Errorf("not connected")
	}

	file, err := s.smbConn.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, data)
	return err
}

func (s *SMBConnection) Mkdir(path string) error {
	if s.smbConn == nil {
		return fmt.Errorf("not connected")
	}

	return s.smbConn.MkdirAll(path, 0755)
}

func (s *SMBConnection) Remove(path string) error {
	if s.smbConn == nil {
		return fmt.Errorf("not connected")
	}

	info, err := s.smbConn.Stat(path)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return s.smbConn.RemoveAll(path)
	}
	return s.smbConn.Remove(path)
}

func (s *SMBConnection) Rename(oldpath, newpath string) error {
	if s.smbConn == nil {
		return fmt.Errorf("not connected")
	}

	return s.smbConn.Rename(oldpath, newpath)
}

func (s *SMBConnection) GetCurrentPath() string {
	return s.currentPath
}

func (s *SMBConnection) SetCurrentPath(path string) {
	s.currentPath = path
}

// LocalConnection handles local file system
type LocalConnection struct {
	currentPath string
}

func NewLocalConnection() *LocalConnection {
	homeDir, _ := os.UserHomeDir()
	return &LocalConnection{
		currentPath: homeDir,
	}
}

func (l *LocalConnection) Connect() error {
	return nil
}

func (l *LocalConnection) Disconnect() error {
	return nil
}

func (l *LocalConnection) List(path string) ([]FileInfo, error) {
	if path == "" {
		path = l.currentPath
	}

	files, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var fileInfos []FileInfo
	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			continue
		}

		fileInfos = append(fileInfos, FileInfo{
			Name:    file.Name(),
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
			IsDir:   file.IsDir(),
		})
	}

	return fileInfos, nil
}

func (l *LocalConnection) ReadFile(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

func (l *LocalConnection) WriteFile(path string, data io.Reader) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, data)
	return err
}

func (l *LocalConnection) Mkdir(path string) error {
	return os.MkdirAll(path, 0755)
}

func (l *LocalConnection) Remove(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func (l *LocalConnection) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (l *LocalConnection) GetCurrentPath() string {
	return l.currentPath
}

func (l *LocalConnection) SetCurrentPath(path string) {
	l.currentPath = path
}

