package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"runtime"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// runFileAgent starts a foreground TLS listener for LAN file access. It exits when the context is cancelled or after ttl (if ttl > 0).
func runFileAgent(ctx context.Context, addr, root, psk string, lanOnly bool, ttl time.Duration) error {
	return runFileAgentLog(ctx, addr, root, psk, lanOnly, ttl, os.Stderr)
}

// runFileAgentLog is like runFileAgent but writes status lines to logw (e.g. os.Stderr or a GUI buffer).
func runFileAgentLog(ctx context.Context, addr, root, psk string, lanOnly bool, ttl time.Duration, logw io.Writer) error {
	if logw == nil {
		logw = os.Stderr
	}
	if ttl > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ttl)
		defer cancel()
		fmt.Fprintf(logw, "Agent will exit after %s unless stopped earlier (Ctrl+C).\n", ttl.String())
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("file-agent root: %w", err)
	}
	st, err := os.Stat(rootAbs)
	if err != nil {
		return fmt.Errorf("file-agent root: %w", err)
	}
	if !st.IsDir() {
		return fmt.Errorf("file-agent root is not a directory: %s", rootAbs)
	}

	if strings.TrimSpace(psk) == "" {
		raw := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, raw); err != nil {
			return err
		}
		psk = base64.RawURLEncoding.EncodeToString(raw)
		fmt.Fprintf(logw, "\n=== Generated pre-shared key (copy to client profile) ===\n%s\n\n", psk)
	} else {
		fmt.Fprintf(logw, "\n=== Using provided pre-shared key ===\n(verify it matches the client profile)\n\n")
	}

	tlsCert, certDER, err := generateEphemeralServerCert()
	if err != nil {
		return err
	}
	fp := sha256.Sum256(certDER)
	fpHex := hex.EncodeToString(fp[:])
	fmt.Fprintf(logw, "=== TLS certificate SHA-256 fingerprint (paste into client \"TLS certificate pin\") ===\n%s\n\n", fpHex)

	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{tlsCert},
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	defer ln.Close()

	fmt.Fprintf(logw, "FileMover LAN file agent — root %s\nListening on %s (effective %s, TLS 1.3, PSK auth)\nLAN-only peers: %v\nEach run uses a new TLS certificate; update the saved TLS pin in the client profile after restarting the agent.\nPress Ctrl+C or send SIGINT to stop.\n", rootAbs, addr, ln.Addr().String(), lanOnly)
	fileAgentDebugf("agent listener %v", ln.Addr())

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func(c net.Conn) {
			defer c.Close()
			fileAgentDebugf("accepted TCP from %s", c.RemoteAddr())
			tcpAddr, _ := c.RemoteAddr().(*net.TCPAddr)
			if tcpAddr == nil || !allowPeerIPForLANAgent(tcpAddr.IP, lanOnly) {
				if fileAgentDebug() && tcpAddr != nil {
					fileAgentDebugf("rejecting peer %s (lanOnly=%v)", tcpAddr.IP, lanOnly)
				}
				return
			}
			tlsConn := tls.Server(c, cfg)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			s := &fileAgentSession{root: rootAbs, psk: psk, conn: tlsConn}
			s.serve()
		}(conn)
	}
}

func generateEphemeralServerCert() (tls.Certificate, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"KrankyBear FileMover LAN agent"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(48 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	return cert, der, nil
}

type fileAgentSession struct {
	root string
	psk  string
	conn net.Conn
}

func (s *fileAgentSession) serve() {
	buf, err := readFrame(s.conn)
	if err != nil {
		return
	}
	var auth fileAgentMsg
	if err := json.Unmarshal(buf, &auth); err != nil || auth.Op != fileAgentOpAuth {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpAuthFail, Msg: "invalid auth"})
		return
	}
	if auth.V != 0 && auth.V != fileAgentProtocolVer {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpAuthFail, Msg: "protocol version mismatch"})
		return
	}
	if !fileAgentPSKEqual(auth.PSK, s.psk) {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpAuthFail, Msg: "pre-shared key does not match receiver (-file-agent-psk / generated key)"})
		return
	}
	if err := writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpAuthOK, V: fileAgentProtocolVer}); err != nil {
		return
	}

	for {
		frame, err := readFrame(s.conn)
		if err != nil {
			return
		}
		var m fileAgentMsg
		if err := json.Unmarshal(frame, &m); err != nil {
			_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: "invalid message"})
			continue
		}
		switch m.Op {
		case fileAgentOpList:
			s.handleList(m.Path)
		case fileAgentOpRead:
			s.handleRead(m.Path)
		case fileAgentOpWriteBeg:
			s.handleWriteStream(m.Path)
		case fileAgentOpMkdir:
			s.handleMkdir(m.Path)
		case fileAgentOpRemove:
			s.handleRemove(m.Path)
		case fileAgentOpRename:
			s.handleRename(m.Path, m.NewPath)
		default:
			_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: "unknown op"})
		}
	}
}

func fileAgentPSKEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

func fileAgentWindowsRelUnderRoot(root, rel string) string {
	if runtime.GOOS != "windows" {
		return rel
	}
	rootVol := filepath.VolumeName(root)
	relLocal := filepath.FromSlash(rel)
	vol := filepath.VolumeName(relLocal)
	if vol == "" {
		return rel
	}
	after := strings.TrimPrefix(relLocal, vol)
	after = strings.TrimPrefix(after, `\`)
	after = strings.TrimPrefix(after, `/`)
	if !strings.EqualFold(vol, rootVol) {
		return rel
	}
	if after == "" {
		return ""
	}
	return filepath.ToSlash(after)
}

func (s *fileAgentSession) resolveAgentPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		p = "/"
	}
	p = strings.ReplaceAll(p, `\`, `/`)
	p = path.Clean("/" + strings.TrimPrefix(p, "/"))
	rel := strings.TrimPrefix(p, "/")
	rel = fileAgentWindowsRelUnderRoot(s.root, rel)
	if rel == "" || rel == "." {
		abs, err := filepath.Abs(s.root)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	full := filepath.Join(s.root, filepath.FromSlash(rel))
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	relPath, err := filepath.Rel(rootAbs, abs)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path outside shared root")
	}
	return abs, nil
}

func (s *fileAgentSession) handleList(agentPath string) {
	abs, err := s.resolveAgentPath(agentPath)
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{
			Op: fileAgentOpError,
			Msg: fmt.Sprintf("cannot list %q: %v — paths are relative to -file-agent-root on this host, not full system paths unless root is /", agentPath, err),
		})
		return
	}
	out := make([]fileAgentEntry, 0, len(ents))
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, fileAgentEntry{
			Name:    e.Name(),
			Size:    info.Size(),
			ModUnix: info.ModTime().Unix(),
			IsDir:   e.IsDir(),
			Mode:    uint32(info.Mode()),
		})
	}
	_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpListResp, Entries: out})
}

func (s *fileAgentSession) handleRead(agentPath string) {
	abs, err := s.resolveAgentPath(agentPath)
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	if st.IsDir() {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: "is a directory"})
		return
	}
	sz := st.Size()
	if err := writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpReadHdr, Size: sz}); err != nil {
		return
	}
	if sz > 0 {
		if _, err := io.CopyN(s.conn, f, sz); err != nil {
			return
		}
	}
	_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpReadDone})
}

func (s *fileAgentSession) handleWriteStream(agentPath string) {
	abs, err := s.resolveAgentPath(agentPath)
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	defer f.Close()
	if err := writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpWriteReady}); err != nil {
		return
	}

	for {
		frame, err := readFrame(s.conn)
		if err != nil {
			return
		}
		var m fileAgentMsg
		if err := json.Unmarshal(frame, &m); err != nil {
			_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: "invalid chunk"})
			return
		}
		switch m.Op {
		case fileAgentOpWriteChk:
			if m.N <= 0 || m.N > fileAgentMaxChunk {
				_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: "invalid chunk size"})
				return
			}
			if _, err := io.CopyN(f, s.conn, int64(m.N)); err != nil {
				return
			}
		case fileAgentOpWriteEnd:
			_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpWriteOK})
			return
		default:
			_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: "expected write chunk"})
			return
		}
	}
}

func (s *fileAgentSession) handleMkdir(agentPath string) {
	abs, err := s.resolveAgentPath(agentPath)
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpOK})
}

func (s *fileAgentSession) handleRemove(agentPath string) {
	abs, err := s.resolveAgentPath(agentPath)
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	st, err := os.Stat(abs)
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	if st.IsDir() {
		err = os.RemoveAll(abs)
	} else {
		err = os.Remove(abs)
	}
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpOK})
}

func (s *fileAgentSession) handleRename(oldAgentPath, newAgentPath string) {
	oldAbs, err := s.resolveAgentPath(oldAgentPath)
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	newAbs, err := s.resolveAgentPath(newAgentPath)
	if err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpError, Msg: err.Error()})
		return
	}
	_ = writeJSONFrame(s.conn, fileAgentMsg{Op: fileAgentOpOK})
}
