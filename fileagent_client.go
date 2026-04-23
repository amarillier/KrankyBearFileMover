package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FileAgentConnection is a TLS+PSK client for the LAN file agent (ConnectionManager).
type FileAgentConnection struct {
	profile     *Connection
	conn        *tls.Conn
	addr        string
	currentPath string
	mu          sync.Mutex
}

func NewFileAgentConnection(profile *Connection) *FileAgentConnection {
	start := "/"
	if p := strings.TrimSpace(profile.RemotePath); p != "" {
		start = fileAgentNormalizeVirtualPath(p)
	}
	port := profile.Port
	if port == 0 {
		port = 9742
	}
	host := strings.TrimSpace(profile.Host)
	return &FileAgentConnection{
		profile:     profile,
		addr:        net.JoinHostPort(host, strconv.Itoa(port)),
		currentPath: start,
	}
}

func (c *FileAgentConnection) dial() error {
	pin := normalizeCertPinHex(c.profile.FileAgentTLSPin)
	if pin == "" {
		return fmt.Errorf("TLS certificate pin is required for LAN file agent connections")
	}
	expected, err := hex.DecodeString(pin)
	if err != nil || len(expected) != sha256.Size {
		return fmt.Errorf("TLS pin must be %d hex characters (SHA-256)", sha256.Size*2)
	}

	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no certificate from server")
			}
			sum := sha256.Sum256(rawCerts[0])
			if subtle.ConstantTimeCompare(sum[:], expected) != 1 {
				return fmt.Errorf("TLS certificate fingerprint does not match saved pin")
			}
			return nil
		},
	}

	fileAgentDebugf("TCP dial %s (timeout %s)", c.addr, fileAgentDefaultDialTCP)
	d := net.Dialer{Timeout: fileAgentDefaultDialTCP}
	tcpConn, err := d.Dial("tcp", c.addr)
	if err != nil {
		fileAgentDebugf("TCP dial failed: %v", err)
		return fmt.Errorf("TCP connect %s: %w", c.addr, err)
	}
	fileAgentDebugf("TCP connected, TLS handshake…")
	tlsConn := tls.Client(tcpConn, tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		tcpConn.Close()
		fileAgentDebugf("TLS handshake failed: %v", err)
		return fmt.Errorf("TLS handshake %s: %w", c.addr, err)
	}
	c.conn = tlsConn
	fileAgentDebugf("TLS ok, sending auth")

	auth := fileAgentMsg{Op: fileAgentOpAuth, V: fileAgentProtocolVer, PSK: c.profile.Password}
	if err := writeJSONFrame(c.conn, auth); err != nil {
		c.conn.Close()
		c.conn = nil
		return err
	}
	var resp fileAgentMsg
	if err := readJSONFrame(c.conn, &resp); err != nil {
		c.conn.Close()
		c.conn = nil
		return err
	}
	if resp.Op == fileAgentOpAuthFail {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("authentication failed: %s", resp.Msg)
	}
	if resp.Op != fileAgentOpAuthOK {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("unexpected auth response: %s", resp.Op)
	}
	return nil
}

func (c *FileAgentConnection) ensureConn() error {
	if c.conn != nil {
		return nil
	}
	return c.dial()
}

func (c *FileAgentConnection) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return nil
	}
	return c.dial()
}

func (c *FileAgentConnection) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

func (c *FileAgentConnection) List(req string) ([]FileInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureConn(); err != nil {
		return nil, err
	}
	p := req
	if p == "" {
		p = c.currentPath
	}
	if err := writeJSONFrame(c.conn, fileAgentMsg{Op: fileAgentOpList, Path: p}); err != nil {
		return nil, err
	}
	var resp fileAgentMsg
	if err := readJSONFrame(c.conn, &resp); err != nil {
		return nil, err
	}
	if resp.Op == fileAgentOpError {
		return nil, errors.New(resp.Msg)
	}
	if resp.Op != fileAgentOpListResp {
		return nil, fmt.Errorf("unexpected list response: %s", resp.Op)
	}
	out := make([]FileInfo, 0, len(resp.Entries))
	for _, e := range resp.Entries {
		out = append(out, FileInfo{
			Name:    e.Name,
			Size:    e.Size,
			IsDir:   e.IsDir,
			ModTime: time.Unix(e.ModUnix, 0),
			Mode:    os.FileMode(e.Mode),
		})
	}
	return out, nil
}

func (c *FileAgentConnection) ReadFile(remotePath string) (io.ReadCloser, error) {
	c.mu.Lock()
	if err := c.ensureConn(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if err := writeJSONFrame(c.conn, fileAgentMsg{Op: fileAgentOpRead, Path: remotePath}); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	b, err := readFrame(c.conn)
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	var head fileAgentMsg
	if err := json.Unmarshal(b, &head); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if head.Op == fileAgentOpError {
		c.mu.Unlock()
		return nil, errors.New(head.Msg)
	}
	if head.Op != fileAgentOpReadHdr {
		c.mu.Unlock()
		return nil, fmt.Errorf("unexpected read response: %s", head.Op)
	}
	return &fileAgentReadCloser{fa: c, remain: head.Size}, nil
}

type fileAgentReadCloser struct {
	fa     *FileAgentConnection
	remain int64
}

func (r *fileAgentReadCloser) Read(p []byte) (int, error) {
	if r.remain <= 0 {
		return 0, io.EOF
	}
	nwant := int64(len(p))
	if nwant > r.remain {
		nwant = r.remain
	}
	if nwant == 0 {
		return 0, io.EOF
	}
	n, err := io.ReadFull(r.fa.conn, p[:nwant])
	r.remain -= int64(n)
	return n, err
}

func (r *fileAgentReadCloser) Close() error {
	if r.remain > 0 {
		_, _ = io.CopyN(io.Discard, r.fa.conn, r.remain)
		r.remain = 0
	}
	var done fileAgentMsg
	if err := readJSONFrame(r.fa.conn, &done); err != nil {
		r.fa.mu.Unlock()
		return err
	}
	if done.Op != fileAgentOpReadDone && done.Op != fileAgentOpError {
		r.fa.mu.Unlock()
		return fmt.Errorf("unexpected read trailer: %s", done.Op)
	}
	r.fa.mu.Unlock()
	if done.Op == fileAgentOpError {
		return errors.New(done.Msg)
	}
	return nil
}

func (c *FileAgentConnection) WriteFile(remotePath string, data io.Reader) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureConn(); err != nil {
		return err
	}
	if err := writeJSONFrame(c.conn, fileAgentMsg{Op: fileAgentOpWriteBeg, Path: remotePath}); err != nil {
		return err
	}
	var wack fileAgentMsg
	if err := readJSONFrame(c.conn, &wack); err != nil {
		return err
	}
	if wack.Op == fileAgentOpError {
		return errors.New(wack.Msg)
	}
	if wack.Op != fileAgentOpWriteReady {
		return fmt.Errorf("unexpected write ack: %s", wack.Op)
	}
	buf := make([]byte, fileAgentMaxChunk)
	for {
		n, err := data.Read(buf)
		if n > 0 {
			if errW := writeJSONFrame(c.conn, fileAgentMsg{Op: fileAgentOpWriteChk, N: n}); errW != nil {
				return errW
			}
			if _, errW := c.conn.Write(buf[:n]); errW != nil {
				return errW
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if err := writeJSONFrame(c.conn, fileAgentMsg{Op: fileAgentOpWriteEnd}); err != nil {
		return err
	}
	var resp fileAgentMsg
	if err := readJSONFrame(c.conn, &resp); err != nil {
		return err
	}
	if resp.Op == fileAgentOpError {
		return errors.New(resp.Msg)
	}
	if resp.Op != fileAgentOpWriteOK {
		return fmt.Errorf("unexpected write response: %s", resp.Op)
	}
	return nil
}

func (c *FileAgentConnection) Mkdir(remotePath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureConn(); err != nil {
		return err
	}
	if err := writeJSONFrame(c.conn, fileAgentMsg{Op: fileAgentOpMkdir, Path: remotePath}); err != nil {
		return err
	}
	return c.readSimpleOK()
}

func (c *FileAgentConnection) Remove(remotePath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureConn(); err != nil {
		return err
	}
	if err := writeJSONFrame(c.conn, fileAgentMsg{Op: fileAgentOpRemove, Path: remotePath}); err != nil {
		return err
	}
	return c.readSimpleOK()
}

func (c *FileAgentConnection) Rename(oldpath, newpath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureConn(); err != nil {
		return err
	}
	if err := writeJSONFrame(c.conn, fileAgentMsg{Op: fileAgentOpRename, Path: oldpath, NewPath: newpath}); err != nil {
		return err
	}
	return c.readSimpleOK()
}

func (c *FileAgentConnection) readSimpleOK() error {
	var resp fileAgentMsg
	if err := readJSONFrame(c.conn, &resp); err != nil {
		return err
	}
	if resp.Op == fileAgentOpError {
		return errors.New(resp.Msg)
	}
	if resp.Op != fileAgentOpOK {
		return fmt.Errorf("unexpected response: %s", resp.Op)
	}
	return nil
}

func (c *FileAgentConnection) GetCurrentPath() string {
	return c.currentPath
}

func (c *FileAgentConnection) SetCurrentPath(p string) {
	c.currentPath = fileAgentNormalizeVirtualPath(p)
}

// fileAgentUpToExisting returns the nearest virtual path at or above fileAgentParentDir(from)
// that lists successfully. Use for ↑ when the current path is missing on the share (e.g. user
// entered a host-absolute path by mistake).
func fileAgentUpToExisting(fa *FileAgentConnection, from string) string {
	if fa == nil || from == "" || from == "/" {
		return "/"
	}
	p := fileAgentParentDir(from)
	for {
		if p == from {
			return "/"
		}
		_, err := fa.List(p)
		if err == nil {
			return p
		}
		if p == "/" {
			return "/"
		}
		next := fileAgentParentDir(p)
		if next == p {
			return "/"
		}
		p = next
	}
}
