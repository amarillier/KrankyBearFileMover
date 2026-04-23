package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	fileAgentMaxFrame       = 64 << 20 // 64 MiB (JSON list / control)
	fileAgentMaxChunk       = 256 * 1024
	fileAgentProtocolVer    = 1
	fileAgentDefaultListen  = ":9742"
	fileAgentDefaultDialTCP = 45 * time.Second
)

const (
	fileAgentOpAuth      = "auth"
	fileAgentOpAuthOK    = "authOK"
	fileAgentOpAuthFail  = "authFail"
	fileAgentOpError     = "error"
	fileAgentOpList      = "list"
	fileAgentOpListResp  = "listResp"
	fileAgentOpRead      = "read"
	fileAgentOpReadHdr   = "readHdr"
	fileAgentOpReadDone  = "readDone"
	fileAgentOpWriteBeg  = "writeBeg"
	fileAgentOpWriteReady = "writeReady"
	fileAgentOpWriteChk  = "writeChk"
	fileAgentOpWriteEnd  = "writeEnd"
	fileAgentOpWriteOK   = "writeOK"
	fileAgentOpMkdir     = "mkdir"
	fileAgentOpOK        = "ok"
	fileAgentOpRemove    = "remove"
	fileAgentOpRename    = "rename"
)

type fileAgentMsg struct {
	Op   string `json:"op"`
	V    int    `json:"v,omitempty"`
	PSK  string `json:"psk,omitempty"`
	Msg  string `json:"msg,omitempty"`
	Path string `json:"path,omitempty"`
	// listResp
	Entries []fileAgentEntry `json:"entries,omitempty"`
	// readHdr
	Size int64 `json:"size,omitempty"`
	// writeChk
	N int `json:"n,omitempty"`
	// rename
	NewPath string `json:"newPath,omitempty"`
}

type fileAgentEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModUnix int64  `json:"mod"`
	IsDir   bool   `json:"isDir"`
	Mode    uint32 `json:"mode"`
}

func readFrame(r io.Reader) ([]byte, error) {
	var nb [4]byte
	if _, err := io.ReadFull(r, nb[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(nb[:])
	if n == 0 || n > fileAgentMaxFrame {
		return nil, fmt.Errorf("invalid frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > fileAgentMaxFrame {
		return fmt.Errorf("invalid payload length %d", len(payload))
	}
	var nb [4]byte
	binary.BigEndian.PutUint32(nb[:], uint32(len(payload)))
	if _, err := w.Write(nb[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func writeJSONFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFrame(w, b)
}

func readJSONFrame(r io.Reader, v any) error {
	b, err := readFrame(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// fileAgentIsShareRootAlias reports paths users type thinking of a Windows drive root (C:\)
// when the agent share is that drive — they mean virtual /, not a subfolder literally named C:.
func fileAgentIsShareRootAlias(p string) bool {
	s := strings.TrimSpace(strings.ReplaceAll(p, `\`, `/`))
	s = strings.TrimPrefix(s, "/")
	if len(s) == 2 && s[1] == ':' {
		return true // C:
	}
	if len(s) == 3 && s[1] == ':' && s[2] == '/' {
		return true // C:/
	}
	return false
}

// fileAgentNormalizeVirtualPath maps user input in the path bar to a virtual agent path (/...).
func fileAgentNormalizeVirtualPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	p = strings.ReplaceAll(p, `\`, `/`)
	if fileAgentIsShareRootAlias(p) {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = path.Clean(p)
	if p == "" || p == "." {
		return "/"
	}
	return p
}

func fileAgentJoinPath(dir, elem string) string {
	d := "/" + strings.TrimPrefix(strings.TrimSpace(dir), "/")
	d = path.Clean(d)
	if d == "" || d == "." {
		d = "/"
	}
	elem = strings.TrimSpace(elem)
	if elem == "" || elem == "." {
		return d
	}
	rel := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(elem, "/")), "/")
	if rel == "." || rel == "" {
		return d
	}
	return path.Clean(path.Join(d, rel))
}

func fileAgentParentDir(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	c := path.Clean(p)
	if c == "/" {
		return "/"
	}
	parent := path.Dir(c)
	if parent == "." {
		return "/"
	}
	return parent
}

func normalizeCertPinHex(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

// BuildFileAgentReceiverCommand builds a shell-friendly command line to start the agent on the receiving PC.
// Use quoteArg-style quoting via strconv.Quote for each argument (suitable for pasting into bash/zsh; adjust for cmd.exe if needed).
func BuildFileAgentReceiverCommand(exe, listenAddr, root string, psk string, includePSK, lanOnly bool, ttl time.Duration) string {
	var b strings.Builder
	b.WriteString(strconv.Quote(exe))
	b.WriteString(" -file-agent")
	b.WriteString(" -file-agent-listen ")
	b.WriteString(strconv.Quote(listenAddr))
	b.WriteString(" -file-agent-root ")
	b.WriteString(strconv.Quote(root))
	if ttl > 0 {
		b.WriteString(" -file-agent-ttl ")
		b.WriteString(strconv.Quote(ttl.String()))
	}
	if includePSK && strings.TrimSpace(psk) != "" {
		b.WriteString(" -file-agent-psk ")
		b.WriteString(strconv.Quote(strings.TrimSpace(psk)))
	}
	if !lanOnly {
		b.WriteString(" -file-agent-lan-only=false")
	}
	return b.String()
}

func allowPeerIPForLANAgent(ip net.IP, lanOnly bool) bool {
	if !lanOnly {
		return true
	}
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	if ip.IsLinkLocalUnicast() {
		return true
	}
	return false
}
