package main

// fileMoverHelpBody returns the main Help tab text (shared by help dialogs).
func fileMoverHelpBody() string {
	return `KrankyBear FileMover is a cross-platform dual-pane file transfer application.

FEATURES:

- Cross-platform: macOS, Linux, and Windows
- Dual pane: browse two locations and copy files between them
- Connection types:
  • Local disk
  • SFTP / SCP over SSH
  • SMB / CIFS (including already-mounted shares on macOS/Linux)
  • Rsync (local or remote user@host:/path, with optional SSH jump/extra args)
  • LAN file agent: TLS + pre-shared key, no SSH or SMB required (optional foreground mode)
- Secure storage: encrypted connection database (master password)
- Rsync sync toolbar when an rsync profile is active (Left→Right, Right→Left, both)
- Themes, system tray, debug logging

USAGE:

- Connections → pick a profile → Connect Left / Connect Right
- Click folders to open; select a file → → Transfer or ← Transfer
- Disconnect returns a pane to local filesystem

LAN FILE AGENT (optional):

The receiving machine runs the app from a terminal in “agent” mode (not a background service).
The sending machine uses a connection of type “fileagent” with Host, Port, Pre-shared key,
and TLS certificate pin (SHA-256 hex printed when the agent starts).

“Remote path” in the profile is relative to -file-agent-root on the receiver (/ = share root).
It is not the host’s /home/... unless you set -file-agent-root to / or to that directory.

1) On the PC that SHARES files, run for example:
     filemover -file-agent -file-agent-listen :9742 -file-agent-root .
   (Any -file-agent-* flag on the command line starts headless agent mode; -file-agent is optional.)
   Add -file-agent-psk "your-key" if you want a fixed key (otherwise one is generated and printed).
   Add -file-agent-ttl 30m to stop automatically after thirty minutes.
   Default -file-agent-lan-only=true accepts only loopback/private/link-local clients.

   Scope: use the agent when both machines are on the same typical “home LAN” (one flat segment).
   It is not designed for crossing routed VLANs, NAT/port-forwards, or lab topologies where you would
   normally use SSH/RDP port mapping — for those cases use SFTP, SCP, or SMB in FileMover instead.

   Windows/PowerShell note: launching with plain "& filemover.exe -file-agent" returns to the prompt
   immediately (it's a GUI-subsystem exe) and Ctrl+C can be swallowed by PowerShell's own line editor
   instead of stopping the agent. Simplest fix: run "filemover -file-agent-window" instead — it opens
   just the agent window (PSK/pin + Start/Stop button, no console dependency at all). Otherwise use
   cmd.exe, or from PowerShell run it with:
     Start-Process -FilePath filemover.exe -ArgumentList '-file-agent' -NoNewWindow -Wait
   so PowerShell actually waits for it and Ctrl+C works normally. Or just set -file-agent-ttl, or
   close the console window / use the Tools menu GUI agent (Stop button) instead.

2) Copy the printed TLS fingerprint (64 hex chars) into the connection profile’s “TLS certificate pin”.
   Each new agent run gets a new certificate—update the pin after restarting the agent.

3) In Connection Settings for type “fileagent”, use “Copy receiver command…” to build a paste-ready
   command using this app’s path and your port (optional TTL, LAN-only, and PSK options).

Firewall: the receiver must allow inbound TCP on the agent port (default 9742). Linux ufw often
denies everything except SSH until you run e.g. sudo ufw allow 9742/tcp. Windows: inbound rule for
TCP 9742 (or the app) on Private networks. macOS: allow incoming for the app if the system firewall
is on. Use -file-agent-listen :9742 (all interfaces), not 127.0.0.1:9742, for remote machines.

Check connectivity from the sender with: nc -vz <host> 9742 — ping may fail while TCP works.

Tools menu (menu bar on macOS, Linux, and Windows): Run LAN file agent on this computer runs the TLS listener in a window (log, PSK, pin).

If a connection fails, run from a terminal with FILEMOVER_FILEAGENT_DEBUG=1 to print TCP/TLS steps
on stderr; on the receiver agent, the same variable logs accepts and LAN-only rejects.

TOTP as a second factor for the agent may be added in a future version.

CONFIGURATION:

Each saved profile has a Name, Type, Host, Port, paths, and type-specific fields (SSH keys,
rsync remote program, SSH extra arguments, LAN agent TLS pin, etc.). Passwords and pins are
stored encrypted in ~/.filemover/ (see README for details).
`
}
