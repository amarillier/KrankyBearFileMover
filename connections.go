package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	debugLog("[SFTP] Connecting: User=%s, Host=%s, Port=%d\n", s.conn.Username, s.conn.Host, s.conn.Port)

	config := &ssh.ClientConfig{
		User:            s.conn.Username,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // In production, use proper host key verification
		Timeout:         10 * time.Second,
	}

	// Try SSH key authentication first if provided
	if s.conn.SSHKeyPath != "" {
		// Expand ~ to home directory
		keyPath := s.conn.SSHKeyPath
		if strings.HasPrefix(keyPath, "~") {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				debugLog("[SFTP] ERROR: Failed to get home directory: %v\n", err)
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			keyPath = filepath.Join(homeDir, strings.TrimPrefix(keyPath, "~"))
		}
		debugLog("[SFTP] Attempting SSH key authentication from: %s (expanded from %s)\n", keyPath, s.conn.SSHKeyPath)
		key, err := os.ReadFile(keyPath)
		if err != nil {
			debugLog("[SFTP] ERROR: Failed to read SSH key file: %v\n", err)
			return fmt.Errorf("failed to read SSH key: %w", err)
		}

		var signer ssh.Signer
		if s.conn.KeyPassphrase != "" {
			debugLog("[SFTP] Parsing SSH key with passphrase\n")
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(s.conn.KeyPassphrase))
		} else {
			debugLog("[SFTP] Parsing SSH key without passphrase\n")
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			debugLog("[SFTP] ERROR: Failed to parse SSH key: %v\n", err)
			return fmt.Errorf("failed to parse SSH key: %w", err)
		}

		config.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
		debugLog("[SFTP] SSH key authentication configured\n")
	}

	// Add password authentication if provided
	if s.conn.Password != "" {
		debugLog("[SFTP] Adding password authentication\n")
		config.Auth = append(config.Auth, ssh.Password(s.conn.Password))
	}

	if len(config.Auth) == 0 {
		debugLog("[SFTP] ERROR: No authentication method provided (no key, no password)\n")
		return fmt.Errorf("no authentication method provided")
	}

	addr := fmt.Sprintf("%s:%d", s.conn.Host, s.conn.Port)
	if s.conn.Port == 0 {
		addr = fmt.Sprintf("%s:22", s.conn.Host)
	}
	debugLog("[SFTP] Connecting to SSH server: %s\n", addr)

	sshConn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		debugLog("[SFTP] ERROR: SSH dial failed: %v\n", err)
		return fmt.Errorf("failed to connect to SSH server at %s: %w", addr, err)
	}
	debugLog("[SFTP] SSH connection established\n")

	sftpConn, err := sftp.NewClient(sshConn)
	if err != nil {
		sshConn.Close()
		debugLog("[SFTP] ERROR: Failed to create SFTP client: %v\n", err)
		return fmt.Errorf("failed to create SFTP client: %w", err)
	}
	debugLog("[SFTP] SFTP client created successfully\n")

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
		conn: conn,
	}
}

// parseSMBMountAndPath splits Remote Path into the tree-connect target for session.Mount
// and the initial directory relative to the share root.
// Users may enter: "ShareName", "ShareName\folder", or "\\server\ShareName\folder".
func parseSMBMountAndPath(remotePath string) (mountArg string, withinShare string, err error) {
	rp := strings.TrimSpace(remotePath)
	rp = strings.ReplaceAll(rp, `/`, `\`)

	// UNC: \\server\share or \\server\share\sub\dir
	if strings.HasPrefix(rp, `\\`) {
		body := strings.TrimPrefix(rp, `\\`)
		parts := strings.Split(body, `\`)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid UNC path %q: need \\\\server\\share", remotePath)
		}
		server := parts[0]
		share := parts[1]
		mountArg = fmt.Sprintf(`\\%s\%s`, server, share)
		if len(parts) > 2 {
			withinShare = strings.Join(parts[2:], `\`)
		}
		return mountArg, normalizeSMBWithinSharePath(withinShare), nil
	}

	if rp == "" {
		return "C$", "", nil
	}

	// share\subdir or share only (Mount will prefix \\host\)
	parts := strings.SplitN(rp, `\`, 2)
	shareName := parts[0]
	if shareName == "" {
		return "", "", fmt.Errorf("invalid SMB remote path %q", remotePath)
	}
	if len(parts) == 1 {
		return shareName, "", nil
	}
	return shareName, normalizeSMBWithinSharePath(parts[1]), nil
}

func normalizeSMBWithinSharePath(p string) string {
	p = strings.TrimSpace(p)
	for strings.HasPrefix(p, `\`) {
		p = p[1:]
	}
	p = strings.TrimSuffix(p, `\`)
	return p
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

	mountArg, withinShare, err := parseSMBMountAndPath(s.conn.RemotePath)
	if err != nil {
		session.Logoff()
		conn.Close()
		return err
	}

	share, err := session.Mount(mountArg)
	if err != nil {
		session.Logoff()
		conn.Close()
		return fmt.Errorf("failed to mount share: %w", err)
	}

	s.smbConn = share
	s.session = session
	s.netConn = conn
	s.currentPath = withinShare

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

// splitRsyncRemoteSpec parses "user@host:/remote/path" (one colon between host and path).
// Remote path uses POSIX segments (e.g. Cygwin /cygdrive/c/Users/...).
func splitRsyncRemoteSpec(spec string) (user, host, remotePath string, err error) {
	at := strings.LastIndex(spec, "@")
	if at < 0 {
		return "", "", "", fmt.Errorf("invalid rsync remote path %q (need user@host:path)", spec)
	}
	user = spec[:at]
	rest := spec[at+1:]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return "", "", "", fmt.Errorf("invalid rsync remote path %q (need user@host:path)", spec)
	}
	host = rest[:colon]
	remotePath = rest[colon+1:]
	return user, host, remotePath, nil
}

func rsyncJoinPath(module, elem string) string {
	user, host, rp, err := splitRsyncRemoteSpec(module)
	if err != nil {
		return filepath.Join(module, elem)
	}
	elem = strings.TrimSpace(elem)
	elem = strings.TrimSuffix(elem, "/")
	elem = strings.TrimPrefix(elem, "./")
	if elem == "" || elem == "." {
		return module
	}
	if strings.Contains(elem, " -> ") {
		elem = strings.TrimSpace(strings.SplitN(elem, " -> ", 2)[0])
	}
	if strings.HasPrefix(elem, "/") {
		return fmt.Sprintf("%s@%s:%s", user, host, path.Clean(elem))
	}
	base := path.Clean("/" + strings.TrimPrefix(rp, "/"))
	joined := path.Join(base, elem)
	return fmt.Sprintf("%s@%s:%s", user, host, joined)
}

func rsyncParentDir(module string) string {
	user, host, rp, err := splitRsyncRemoteSpec(module)
	if err != nil {
		return filepath.Dir(module)
	}
	rp = path.Clean("/" + strings.TrimPrefix(rp, "/"))
	parent := path.Dir(rp)
	if parent == "/" || parent == "." {
		return fmt.Sprintf("%s@%s:/", user, host)
	}
	return fmt.Sprintf("%s@%s:%s", user, host, parent)
}

var (
	// ISO datetime then filename (common with GNU rsync).
	reRsyncListISO = regexp.MustCompile(`^([\-dcbDlsp][rwxrwxrwxSTt.\-\+#@]{9,})\s+([\d,]+)\s+(\d{4}/\d{2}/\d{2})\s+(\d{2}:\d{2}:\d{2})\s+(.+)$`)
	// Apr 10 2025 name  OR  Apr 10 12:34 name (current year implied for time form)
	reRsyncListMon = regexp.MustCompile(`^([\-dcbDlsp][rwxrwxrwxSTt.\-\+#@]{9,})\s+([\d,]+)\s+(\w{3})\s+(\d{1,2})\s+(\d{4}|\d{2}:\d{2})\s+(.+)$`)
)

func parseRsyncListOnlyLine(line string) (FileInfo, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return FileInfo{}, false
	}
	if m := reRsyncListISO.FindStringSubmatch(line); m != nil {
		return fileInfoFromRsyncListFields(m[1], m[2], m[3]+" "+m[4], m[5], time.Time{})
	}
	if m := reRsyncListMon.FindStringSubmatch(line); m != nil {
		mon, day, third, fileName := m[3], m[4], m[5], m[6]
		var modTime time.Time
		if strings.Contains(third, ":") {
			modTime, _ = time.Parse("Jan 2 15:04 2006", fmt.Sprintf("%s %s %s %d", mon, day, third, time.Now().Year()))
		} else {
			modTime, _ = time.Parse("Jan 2 2006", fmt.Sprintf("%s %s %s", mon, day, third))
		}
		return fileInfoFromRsyncListFields(m[1], m[2], "", fileName, modTime)
	}
	// Legacy fallback: fixed 5+ fields where cols 2–3 are date+time (brittle).
	parts := strings.Fields(line)
	if len(parts) < 5 {
		return FileInfo{}, false
	}
	mode := parts[0]
	if len(mode) < 1 {
		return FileInfo{}, false
	}
	switch mode[0] {
	case 'd', '-', 'l', 'b', 'c', 'p', 's', 'D', 'L':
	default:
		return FileInfo{}, false
	}
	return fileInfoFromRsyncListFields(mode, parts[1], parts[2]+" "+parts[3], strings.Join(parts[4:], " "), time.Time{})
}

func fileInfoFromRsyncListFields(mode, sizeStr, dateTime, name string, modOverride time.Time) (FileInfo, bool) {
	if len(mode) < 1 {
		return FileInfo{}, false
	}
	switch mode[0] {
	case 'd', '-', 'l', 'b', 'c', 'p', 's', 'D', 'L':
	default:
		return FileInfo{}, false
	}
	isDir := mode[0] == 'd'
	sizeStr = strings.ReplaceAll(sizeStr, ",", "")
	size, _ := strconv.ParseInt(sizeStr, 10, 64)
	name = strings.TrimSpace(name)
	if idx := strings.Index(name, " -> "); idx >= 0 {
		name = strings.TrimSpace(name[:idx])
	}
	if name == "." || name == ".." || name == "" {
		return FileInfo{}, false
	}
	modTime := modOverride
	if modTime.IsZero() && dateTime != "" {
		if t, err := time.Parse("2006/01/02 15:04:05", strings.TrimSpace(dateTime)); err == nil {
			modTime = t
		}
	}
	return FileInfo{Name: name, Size: size, IsDir: isDir, ModTime: modTime, Mode: 0644}, true
}

// rsyncNeedsExplicitSSH reports whether rsync must pass -e to ssh (non-default port or extra args).
func rsyncNeedsExplicitSSH(profile *Connection) bool {
	if profile == nil {
		return false
	}
	port := profile.Port
	if port == 0 {
		port = 22
	}
	if port != 22 {
		return true
	}
	return strings.TrimSpace(profile.SSHExtraArgs) != ""
}

// buildRsyncSSHShell builds the ssh command line passed to rsync -e.
func buildRsyncSSHShell(profile *Connection) string {
	var b strings.Builder
	b.WriteString("ssh")
	port := profile.Port
	if port == 0 {
		port = 22
	}
	if port != 22 {
		fmt.Fprintf(&b, " -p %d", port)
	}
	if ex := strings.TrimSpace(profile.SSHExtraArgs); ex != "" {
		b.WriteByte(' ')
		b.WriteString(ex)
	}
	return b.String()
}

// RsyncRemoteConnection lists and transfers via local rsync over SSH (remote uses rsync --server).
// Browsing uses "rsync --list-only"; it does not use the local os.ReadDir on the module string.
type RsyncRemoteConnection struct {
	profile     *Connection
	currentPath string
}

func NewRsyncRemoteConnection(profile *Connection, module string) *RsyncRemoteConnection {
	return &RsyncRemoteConnection{
		profile:     profile,
		currentPath: module,
	}
}

func (r *RsyncRemoteConnection) rsyncBaseArgs() []string {
	var args []string
	if p := strings.TrimSpace(r.profile.RemoteRsyncPath); p != "" {
		args = append(args, "--rsync-path="+p)
	}
	if rsyncNeedsExplicitSSH(r.profile) {
		args = append(args, "-e", buildRsyncSSHShell(r.profile))
	}
	return args
}

func (r *RsyncRemoteConnection) Connect() error {
	return nil
}

func (r *RsyncRemoteConnection) Disconnect() error {
	return nil
}

func (r *RsyncRemoteConnection) List(req string) ([]FileInfo, error) {
	spec := strings.TrimSpace(req)
	if spec == "" {
		spec = r.currentPath
	}
	rsyncBin, err := exec.LookPath("rsync")
	if err != nil {
		return nil, fmt.Errorf("rsync not found in PATH: %w", err)
	}
	args := append(r.rsyncBaseArgs(), "--list-only", spec)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rsyncBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		msg := fmt.Errorf("rsync --list-only failed for %s: %w", spec, err)
		if detail != "" {
			msg = fmt.Errorf("%w: %s", msg, detail)
		}
		return nil, msg
	}
	var files []FileInfo
	for _, line := range strings.Split(string(out), "\n") {
		if fi, ok := parseRsyncListOnlyLine(line); ok {
			files = append(files, fi)
		}
	}
	return files, nil
}

func (r *RsyncRemoteConnection) ReadFile(remotePath string) (io.ReadCloser, error) {
	rsyncBin, err := exec.LookPath("rsync")
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "filemover-rsync-in-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	_ = tmp.Close()

	args := append(r.rsyncBaseArgs(), remotePath, tmpName)
	cmd := exec.Command(rsyncBin, args...)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmpName)
		return nil, fmt.Errorf("rsync read failed: %w", err)
	}
	f, err := os.Open(tmpName)
	if err != nil {
		_ = os.Remove(tmpName)
		return nil, err
	}
	return &rsyncTempFile{File: f, path: tmpName}, nil
}

func (r *RsyncRemoteConnection) WriteFile(remotePath string, data io.Reader) error {
	rsyncBin, err := exec.LookPath("rsync")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "filemover-rsync-out-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	defer os.Remove(tmpPath)

	args := append(r.rsyncBaseArgs(), tmpPath, remotePath)
	cmd := exec.Command(rsyncBin, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync write failed: %w", err)
	}
	return nil
}

func (r *RsyncRemoteConnection) Mkdir(path string) error {
	return fmt.Errorf("mkdir not supported for rsync remote paths")
}

func (r *RsyncRemoteConnection) Remove(path string) error {
	return fmt.Errorf("delete not supported for rsync remote paths")
}

func (r *RsyncRemoteConnection) Rename(oldpath, newpath string) error {
	return fmt.Errorf("rename not supported for rsync remote paths")
}

func (r *RsyncRemoteConnection) GetCurrentPath() string {
	return r.currentPath
}

func (r *RsyncRemoteConnection) SetCurrentPath(p string) {
	r.currentPath = p
}

type rsyncTempFile struct {
	*os.File
	path string
}

func (f *rsyncTempFile) Close() error {
	err := f.File.Close()
	_ = os.Remove(f.path)
	return err
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

