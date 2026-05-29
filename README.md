# KrankyBear FileMover

A cross-platform dual-pane file transfer application built with Go and the Fyne GUI library. Similar to WinSCP or FileZilla, this application provides an intuitive interface for transferring files between local and remote systems using SSH (SFTP/SCP), SMB/CIFS, **rsync**, and an optional **LAN file agent** (TLS + pre-shared key, no SSH or share required).

## Features

### Core Functionality

- **Dual-Pane Interface**: Side-by-side file browsers for easy file transfer between local and remote locations
- **Multiple Protocol Support**:
  - **SFTP/SCP**: Secure file transfer over SSH
  - **SMB/CIFS**: Windows file sharing and network drives (with automatic detection of already-mounted shares)
  - **Rsync**: File synchronization with support for local and remote paths (optional SSH extra arguments, e.g. `ProxyJump`)
  - **LAN file agent**: Foreground CLI mode on the “receiver” machine; the GUI connects with TLS, a **pre-shared key**, and a **certificate pin**—useful when SSH or SMB is impractical (still requires a network path, e.g. LAN)
  - **Local File System**: Browse and manage local files
- **Secure Credential Storage**:
  - Password-protected encrypted database (AES-256-GCM)
  - Master password protection using **argon2id** key derivation with a random per-install salt
  - **Full-field encryption at rest**: every sensitive field is encrypted (name, host, username, SSH key path, passphrase, remote/rsync paths, SSH extra args, password, and LAN agent TLS pin)
  - **Optional OS keychain storage** of the master password (macOS Keychain, Windows Credential Manager, Linux Secret Service) so you are not prompted every launch — opt-in, and the database stays portable
  - **Optional master password hint**, shown after two failed login attempts
  - Databases from earlier versions are upgraded automatically on first unlock
- **Connection Management**:
  - Save multiple connection profiles
  - Support for password authentication
  - SSH key authentication with optional passphrase
  - Easy connection switching between left and right panes

### User Interface

- **Modern Fyne GUI**: Cross-platform native look and feel
- **System Tray Integration**: 
  - App icon in system tray (macOS/Linux/Windows)
  - Quick access menu from system tray
  - Show/Hide window functionality
  - Access to all menu options from system tray
- **File Browser Features**:
  - Directory navigation with up button and path entry
  - File listing with size, date, and directory indicators
  - Click directories to navigate, select files to transfer
- **Theme Support**: 
  - Light, Dark, and Default themes
  - Theme preference persists across sessions
  - Accessible from Settings menu
- **Transfer Operations**:
  - One-click file transfer between panes
  - Visual transfer progress (basic implementation)
  - Automatic refresh after transfers
  - **Rsync Synchronization**: Three sync modes available when rsync connections are active:
    - Sync Left→Right: Synchronize from left panel to right panel
    - Sync Right→Left: Synchronize from right panel to left panel
    - Bidirectional Sync: Synchronize changes from both panels

### Cross-Platform Support

- **macOS**: macOS 10.13 (High Sierra) or later
- **Windows**: Windows 10 or later
- **Linux**: Works on all desktop environments (GNOME, KDE, XFCE, Cinnamon, MATE, etc.) with X11 or Wayland

## Installation

### Prerequisites

- Go 1.25 or later
- CGO enabled (required for SQLite and SMB support)

### Building from Source

```bash
# Clone the repository
git clone <repository-url>
cd KrankyBearFileMover

# Install dependencies
go mod download

# Build for your platform
go build -o filemover

# Or build for specific platforms:
# Linux
GOOS=linux GOARCH=amd64 go build -o filemover-linux

# macOS
GOOS=darwin GOARCH=amd64 go build -o filemover-macos

# Windows
GOOS=windows GOARCH=amd64 go build -o filemover-windows.exe
```

### Linux Dependencies

On Linux, you may need to install additional libraries:

```bash
# Ubuntu/Debian
sudo apt-get install -y libasound2 libgl1-mesa-glx libx11-6

# Fedora/RHEL
sudo dnf install -y alsa-lib mesa-libGL libX11
```

## Usage

### First Launch

1. **Set Master Password**: When you first launch the application, you'll be prompted to create a master password. This password encrypts your stored connection credentials. **Remember this password** — by default you'll need it every time you launch the application, and **there is no recovery if you forget it**. Optionally, you can have this computer's OS keychain remember it for you (Settings → Application Settings) so you're not prompted at launch, and set a **password hint** that appears after two failed attempts.

2. **Create a Connection**:
   - Click "New Connection" button
   - Fill in the connection details:
     - **Name**: A friendly name for this connection
     - **Type**: Choose SFTP, SCP, SMB, Rsync, or **fileagent** (LAN agent client)
     - **Host**: IP address or hostname (required for SFTP/SCP/SMB/**fileagent**, optional for some rsync local paths)
     - **Port**: Default ports (22 for SSH, 445 for SMB, **9742** suggested for file agent)
     - **Username**: Your username (not used for **fileagent**)
     - **Password**: Your password, or for **fileagent** the **pre-shared key** matching the receiver’s agent
     - **SSH Key Path**: Path to your SSH private key (optional, hidden for SMB/rsync/**fileagent**)
     - **Key Passphrase**: Passphrase for your SSH key (if encrypted, hidden for SMB/rsync/**fileagent**)
     - **Remote Path**: Initial directory to connect to
       - For rsync: Can be a local path (e.g., `/path/to/dir` or `~/Documents`) or remote path format (`user@host:/path`)
       - For **fileagent**: Path is **relative to `-file-agent-root` on the receiver**, not the host’s absolute filesystem unless the root is `/`. Use **`/`** for the share root, or **`/subfolder`** for a folder inside the share (see LAN file agent section below)
     - **TLS certificate pin** (**fileagent** only): 64-character SHA-256 hex fingerprint printed when the receiver starts `filemover -file-agent ...` (new fingerprint every agent run)
     - **Copy receiver command…** (**fileagent** only): Builds a paste-ready CLI for the **other** machine, with optional TTL and whether to embed the PSK in the command
   - Press **Esc** or click "Cancel" to close without saving
   - Click "Save" to store the connection

### LAN file agent (receiver + sender)

This is **not** a background service: someone runs the agent in a terminal on the machine that **exposes** files, and stops it with **Ctrl+C** (or when **`-file-agent-ttl`** expires).

**Scope:** The agent is aimed at machines on the **same typical home or office LAN** (one local segment). It is **not** designed for crossing **routed VLANs**, **NAT/port-forward** lab setups, or paths where you would usually rely on **SSH port mapping**—use **SFTP**, **SCP**, or **SMB** in FileMover for those instead.

**Headless mode:** Pass **`-file-agent`**, or any other **`-file-agent-*`** flag (e.g. **`-file-agent-listen`**, **`-file-agent-root`**) on the command line—that alone selects the TLS listener without the GUI. With no **`-file-agent*`** arguments, the graphical app starts and needs **`DISPLAY`** (over SSH without X11 you will see GLFW errors).

**1. Receiver (share files)—examples:**

```bash
# Share current directory on port 9742; generate and print a random pre-shared key + TLS fingerprint
filemover -file-agent -file-agent-listen :9742 -file-agent-root .

# Same with a fixed key you will type into the GUI connection profile (Password field)
filemover -file-agent -file-agent-listen :9742 -file-agent-root "$HOME/Public" \
  -file-agent-psk 'use-a-long-random-secret'

# Bind to one interface IP; stop automatically after 45 minutes
filemover -file-agent -file-agent-listen 192.168.1.10:9742 -file-agent-root . \
  -file-agent-ttl 45m

# Allow non–private-IP clients (use with care; default is LAN-only)
filemover -file-agent -file-agent-listen :9742 -file-agent-root . -file-agent-lan-only=false
```

Copy the **TLS fingerprint** into the connection’s **TLS certificate pin** field. If you omit **`-file-agent-psk`**, copy the **generated** key into the profile’s **Password** (pre-shared key) field.

**2. Sender:** Create a connection of type **fileagent** with the receiver’s **Host**, **Port**, **Password** (PSK), **pin**, and **Remote path**.

**Remote path (important):** The GUI path is **virtual** and is always resolved **under** the receiver’s **`-file-agent-root`**. **`/`** is the shared directory. A value like **`/home/allan`** means the path `home/allan` *inside* that share (e.g. `…/KrankyBearFileMover/home/allan` if the agent was started in the project tree)—**not** the Linux home directory. To expose `/home/allan` on the receiver, run the agent with **`-file-agent-root /home/allan`** and set Remote path to **`/`** (or a subfolder only). No need to run as **root**; pick a root directory you can read.

On **Windows**, if the agent’s root is a whole drive (e.g. **`-file-agent-root C:\`**), use virtual **`/`** in the profile and path bar—not **`C:\`**, which would incorrectly mean a `C:` folder inside the share. **`C:\`** / **`C:`** typed in the path bar are treated as the share root for convenience.

**3. In-app helper:** With a **fileagent** profile open, use **Copy receiver command…** to generate a command using this app’s executable path and your port (edit share directory, optional **TTL**, LAN-only, and whether to include the PSK in the copied string).

**4. Firewall and network path:** The sender must reach the receiver’s TCP port (default **9742**) like any other service. **Host firewalls often allow SSH (22) or web ports but block everything else** until you add a rule—so “SSH works, file agent times out” usually means **9742/tcp is not allowed yet**.

- **Ubuntu/Debian (`ufw`)**: e.g. `sudo ufw allow 9742/tcp` or, tighter, `sudo ufw allow from 192.168.0.0/16 to any port 9742 proto tcp` (adjust the subnet). Check `sudo ufw status verbose`.
- **Windows**: Create an **inbound** rule allowing **TCP 9742** (or the specific executable) for **Private** networks while the agent runs; Windows Firewall blocks unexpected listeners by default. Release **`.exe`** builds use **`-H windowsgui`**, so there is no console unless you launch from **cmd/PowerShell** with **`-h`**, **`--help`**, **`/?`**, or any **`-file-agent*`** flag—the binary then attaches to that terminal for output. Alternatively use **Tools → Run LAN file agent on this computer…** in the GUI (log, PSK, and TLS pin appear in that window).
- **macOS**: If **Firewall** is enabled in System Settings, allow incoming connections for the app (or Terminal, if you start `-file-agent` from there).
- **Listen address**: Use **`-file-agent-listen :9742`** so the process listens on all interfaces. **`127.0.0.1:9742`** only accepts local connections; another machine will see **connection timed out**.
- **VMs (e.g. Proxmox)**: Bridged networking is usually enough once the **guest OS** allows the port; the hypervisor VLAN is separate from **ufw/iptables** inside the VM.

**5. Troubleshooting “connection timed out” (TCP, before TLS):** The certificate pin and PSK are checked only **after** TCP connects. If the GUI reports a TCP timeout, fix reachability first.

1. On the **receiver**: `ss -tlnp | grep 9742` — you want `0.0.0.0:9742` or `*:9742`, not only `127.0.0.1:9742`.
2. On the **receiver**, from itself: `nc -vz 127.0.0.1 9742` and `nc -vz $(hostname -I | awk '{print $1}') 9742` should succeed.
3. From the **sender**: `nc -vz <receiver-ip> 9742` — this must succeed before FileMover can connect.
4. **Ping** may fail while **TCP** works (ICMP blocked by the same firewall policy); use **`nc`** as the truth test for file agent.
5. If **`nc`** still times out but you see inbound SYNs on the receiver (`sudo tcpdump -n -i any 'tcp port 9742'`), repeated **SYN with no SYN-ACK** usually means the **receiver’s firewall** is dropping the connection—add the allow rule for **9742/tcp**.
6. **Application debug:** From a terminal, run the GUI with **`FILEMOVER_FILEAGENT_DEBUG=1`** to print TCP/TLS steps on stderr; use the same variable on the receiver agent to log accepts and LAN-only rejects.

**Typical use case:** The LAN file agent helps when **SSH or SMB is unavailable**, **removable media is restricted**, or **approved cloud sync is impractical**—for example moving data between two Macs on the same network. You still need a permitted network path and compliant use under your organization’s policies.

**Future:** TOTP / authenticator support on top of the same protocol may be added later.

3. **Connect**:
   - Select a connection from the list
   - Click "Connect Left" or "Connect Right" to connect to that pane
   - Connection attempts have a configurable timeout (default 10 seconds)
   - If connection fails or times out, an error dialog will appear and the panel will reset to local filesystem
   - The file browser will update to show the remote directory upon successful connection
   - **SMB Note**: If an SMB share is already mounted on your system, it will be automatically detected and used

4. **Transfer Files**:
   - Navigate to the file you want to transfer
   - Select the file in the source pane
   - Click "→ Transfer" to copy from left to right, or "← Transfer" to copy from right to left
   - The file will be transferred to the current directory in the destination pane

5. **Synchronize with Rsync** (when rsync connection is active):
   - Sync buttons appear in the second toolbar row when an rsync connection is established
   - **Sync Left→Right**: Synchronizes all files from left panel to right panel
   - **Sync Right→Left**: Synchronizes all files from right panel to left panel
   - **Bidirectional Sync**: Synchronizes changes from both panels (left→right, then right→left)
   - Uses rsync with archive mode (`-av`) and delete option (`--delete`) to keep directories in sync

### Managing Connections

- **Edit Connection**: Select a connection and click "Edit Connection" (requires master password)
- **Delete Connection**: Select a connection and click "Delete Connection"
- **Refresh List**: Click "Refresh" to reload connections from the database

### Disconnecting

- Click "Disconnect Left" or "Disconnect Right" to disconnect from a remote server
- The pane will revert to showing the local file system

### Settings Menu

- **Theme Settings**: Change between Light, Dark, and Default themes
- **Application Settings**: Connection timeout, master-password re-prompt timeout, panel colors, and **Remember master password in this computer's keychain** (toggle on/off — turning it off removes the stored copy)
- **Change Master Password**: Update your master password (re-encrypts all stored credentials) and optionally set or clear a **password hint**
- **Remove ALL Settings**: Delete all stored connections and reset the application (requires confirmation; also clears any keychain-stored master password)
- **Debug Mode**: Toggle verbose debug logging to file (useful for troubleshooting connection issues)
- **View Debug Log**: View the debug log file in a scrollable window with option to open in system default editor

### Debug Mode

When troubleshooting connection issues (especially SSH key or password problems), you can enable debug mode:

1. Go to **Settings → Debug Mode** to enable/disable debug logging
2. When enabled, all connection attempts, authentication steps, and errors are logged to `~/.filemover/debug.log`
3. Use **Settings → View Debug Log** to view the log file within the application
4. Click "Open Log File" to open it in your system's default text editor
5. Debug mode persists across application restarts

**Note**: Debug logs may contain sensitive information. Keep the log file secure and delete it when no longer needed.

## Server Configuration

### Setting Up OpenSSH Server on Windows

To use SFTP/SCP with Windows servers, you need to install and configure the OpenSSH Server. Here's how to set it up:

#### Installation

**Windows 10 (version 1809 or later) and Windows 11:**

1. Open PowerShell as Administrator
2. Check if OpenSSH Server is available:
   ```powershell
   Get-WindowsCapability -Online | Where-Object Name -like 'OpenSSH.Server*'
   ```
3. Install OpenSSH Server:
   ```powershell
   Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
   ```
4. Start and configure the service:
   ```powershell
   Start-Service sshd
   Set-Service -Name sshd -StartupType 'Automatic'
   ```

**Windows Server 2019 and later:**

1. Open PowerShell as Administrator
2. Install OpenSSH Server:
   ```powershell
   Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
   ```
3. Start the service:
   ```powershell
   Start-Service sshd
   Set-Service -Name sshd -StartupType 'Automatic'
   ```

#### Configuring SSH Server (sshd_config)

1. Open PowerShell as Administrator
2. Navigate to the SSH configuration directory:
   ```powershell
   cd C:\ProgramData\ssh
   ```
3. Edit the `sshd_config` file (backup first):
   ```powershell
   Copy-Item sshd_config sshd_config.backup
   notepad sshd_config
   ```
   
   **Important**: When editing SSH configuration files on Windows:
   - **File encoding**: Must be UTF-8 (without BOM)
   - **Line endings**: Must use Unix format (LF only, not Windows CRLF)
   - Notepad may convert line endings - consider using VS Code, Notepad++, or PowerShell to ensure correct format
   - To convert line endings in PowerShell: `(Get-Content sshd_config -Raw) -replace "`r`n", "`n" | Set-Content sshd_config -NoNewline`

4. Ensure the following settings are configured:

   ```config
   # Enable SFTP subsystem
   Subsystem sftp sftp-server.exe
   
   # Enable public key authentication
   PubkeyAuthentication yes
   
   # Specify the authorized_keys file location
   # For regular users: .ssh/authorized_keys (in their home directory)
   # For administrators: C:\ProgramData\ssh\administrators_authorized_keys
   AuthorizedKeysFile .ssh/authorized_keys
   
   # Allow password authentication (set to 'no' for key-only access)
   PasswordAuthentication yes
   
   # Enable SCP
   # Note: SCP is enabled by default when SFTP subsystem is configured
   
   # Optional: Change default port (default is 22)
   # Port 22
   
   # Optional: Restrict which users can log in
   # AllowUsers username1 username2
   ```

5. Save the file and restart the SSH service:
   ```powershell
   Restart-Service sshd
   ```

#### Setting Up Key-Based Authentication

**Important**: The location of the authorized_keys file depends on whether you're using an administrator account or a regular user account.

##### For Regular (Non-Administrator) Users

1. **Create the .ssh directory** (if it doesn't exist):
   ```powershell
   # For the current user
   New-Item -ItemType Directory -Path "$env:USERPROFILE\.ssh" -Force
   
   # Or for a specific user (replace 'username' with actual username)
   # New-Item -ItemType Directory -Path "C:\Users\username\.ssh" -Force
   ```

2. **Create or edit the authorized_keys file**:
   ```powershell
   # Navigate to the .ssh directory
   cd $env:USERPROFILE\.ssh
   
   # Create the file if it doesn't exist
   New-Item -ItemType File -Path authorized_keys -Force
   
   # Open it for editing
   notepad authorized_keys
   ```
   
   **Important**: The `authorized_keys` file must:
   - **File encoding**: UTF-8 (without BOM)
   - **Line endings**: Unix format (LF only, not Windows CRLF)
   - Each key must be on a single line (no line breaks within a key)
   - If using Notepad, it may add CRLF line endings - use VS Code, Notepad++, or PowerShell to ensure correct format

3. **Add your public key**:
   - Copy your public SSH key (usually from `~/.ssh/id_rsa.pub` or `~/.ssh/id_ed25519.pub` on your client machine)
   - Paste it into the `authorized_keys` file (one key per line)
   - Ensure the key is on a single line with no line breaks
   - Save the file with UTF-8 encoding and Unix line endings

4. **Set proper permissions** (important for security):
   ```powershell
   # Set permissions on .ssh directory
   icacls "$env:USERPROFILE\.ssh" /inheritance:r
   icacls "$env:USERPROFILE\.ssh" /grant:r "$env:USERNAME:(OI)(CI)F"
   
   # Set permissions on authorized_keys file
   icacls "$env:USERPROFILE\.ssh\authorized_keys" /inheritance:r
   icacls "$env:USERPROFILE\.ssh\authorized_keys" /grant:r "$env:USERNAME:(F)"
   ```

##### For Administrator Accounts (Windows 11 and Windows Server)

**Note**: On Windows 11 and newer Windows Server versions, administrator accounts may need to use the `administrators_authorized_keys` file located in `C:\ProgramData\ssh\` instead of the user's `.ssh\authorized_keys` file.

1. **Create or edit the administrators_authorized_keys file**:
   ```powershell
   # Navigate to the SSH configuration directory
   cd C:\ProgramData\ssh
   
   # Create the file if it doesn't exist
   New-Item -ItemType File -Path administrators_authorized_keys -Force
   
   # Open it for editing
   notepad administrators_authorized_keys
   ```
   
   **Important**: The `administrators_authorized_keys` file must:
   - **File encoding**: UTF-8 (without BOM)
   - **Line endings**: Unix format (LF only, not Windows CRLF)
   - Each key must be on a single line (no line breaks within a key)
   - If using Notepad, it may add CRLF line endings - use VS Code, Notepad++, or PowerShell to ensure correct format

2. **Add your public key**:
   - Copy your public SSH key (usually from `~/.ssh/id_rsa.pub` or `~/.ssh/id_ed25519.pub` on your client machine)
   - Paste it into the `administrators_authorized_keys` file (one key per line)
   - Ensure the key is on a single line with no line breaks
   - Save the file with UTF-8 encoding and Unix line endings

3. **Set proper permissions** (critical for administrators_authorized_keys):
   
   **Option 1: Using Repair-AuthorizedKeyPermission cmdlet (Recommended)**:
   ```powershell
   # This cmdlet automatically sets the correct permissions
   Repair-AuthorizedKeyPermission -FilePath C:\ProgramData\ssh\administrators_authorized_keys
   ```
   
   **Option 2: Manual permission setting**:
   ```powershell
   # Set permissions: Full control to SYSTEM and BUILTIN\Administrators
   icacls "C:\ProgramData\ssh\administrators_authorized_keys" /inheritance:r
   icacls "C:\ProgramData\ssh\administrators_authorized_keys" /grant:r "SYSTEM:(F)"
   icacls "C:\ProgramData\ssh\administrators_authorized_keys" /grant:r "BUILTIN\Administrators:(F)"
   ```

4. **Restart the SSH service**:
   ```powershell
   Restart-Service sshd
   ```

**Which file to use?**
- **Regular users**: Use `C:\Users\username\.ssh\authorized_keys`
- **Administrator accounts on Windows 11/Server**: Use `C:\ProgramData\ssh\administrators_authorized_keys`
- If key-based authentication doesn't work with the user's authorized_keys file, try the administrators_authorized_keys file

#### Generating SSH Keys (if you don't have one)

**On Windows (PowerShell):**
```powershell
# Generate a new SSH key pair
ssh-keygen -t ed25519 -C "your_email@example.com"

# Or use RSA (if ed25519 is not available)
ssh-keygen -t rsa -b 4096 -C "your_email@example.com"
```

**On macOS/Linux:**
```bash
# Generate a new SSH key pair
ssh-keygen -t ed25519 -C "your_email@example.com"

# Or use RSA
ssh-keygen -t rsa -b 4096 -C "your_email@example.com"
```

The public key will be saved to `~/.ssh/id_ed25519.pub` (or `id_rsa.pub`). Copy this to the Windows server's `authorized_keys` file.

#### Testing the Connection

1. **Test SSH connection**:
   ```powershell
   ssh username@windows-server-ip
   ```

2. **Test SFTP**:
   ```powershell
   sftp username@windows-server-ip
   ```

3. **Test SCP**:
   ```powershell
   scp testfile.txt username@windows-server-ip:C:\Users\username\
   ```

#### Firewall Configuration

Ensure Windows Firewall allows SSH connections:

```powershell
# Allow SSH through Windows Firewall
New-NetFirewallRule -Name sshd -DisplayName 'OpenSSH Server (sshd)' -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22
```

Or use the GUI:
1. Open Windows Defender Firewall
2. Click "Advanced settings"
3. Click "Inbound Rules"
4. Find "OpenSSH SSH Server (sshd)" and ensure it's enabled

#### Troubleshooting Windows SSH Server

- **Service not starting**: Check Event Viewer → Windows Logs → Application for errors
- **Permission denied**: 
  - Verify `authorized_keys` file permissions are correct
  - For administrator accounts, ensure you're using `C:\ProgramData\ssh\administrators_authorized_keys` with proper permissions (SYSTEM and BUILTIN\Administrators)
  - Run `Repair-AuthorizedKeyPermission` cmdlet if using administrators_authorized_keys
- **Connection refused**: Ensure firewall allows port 22 and SSH service is running
- **SFTP not working**: Verify `Subsystem sftp sftp-server.exe` is in `sshd_config`
- **Keys not working**: 
  - Ensure the public key in `authorized_keys` matches your private key exactly (no extra spaces or line breaks)
  - **For administrator accounts**: If key authentication fails with user's `.ssh\authorized_keys`, try using `C:\ProgramData\ssh\administrators_authorized_keys` instead
  - Verify the correct file is being used based on your account type (regular user vs administrator)
  - **Check file encoding and line endings**: Files must be UTF-8 encoded with Unix (LF) line endings, not Windows (CRLF)
  - To fix line endings in PowerShell: `(Get-Content authorized_keys -Raw) -replace "`r`n", "`n" | Set-Content authorized_keys -NoNewline -Encoding UTF8`

## Security

### Credential Storage

- All credentials are stored in an encrypted SQLite database located at `~/.filemover/connections.db`
- Encryption uses **AES-256-GCM**; the key is derived from your master password using **argon2id** with a **random per-install salt** stored in the database (so the database stays portable between computers)
- **Every sensitive connection field is encrypted at rest** — name, host, username, SSH key path, key passphrase, remote and rsync paths, SSH extra arguments, password, and the LAN file-agent TLS pin. Only non-sensitive fields (type, port, timestamps) are kept in clear text
- By default the master password is **never stored** — it is used only to derive the encryption key. Optionally, this computer's **OS keychain** (macOS Keychain, Windows Credential Manager, Linux Secret Service) can remember it to skip the launch prompt; the keychain only ever holds the master password, and the database remains portable
- Databases created by earlier versions are **upgraded automatically** (stronger key derivation and full-field encryption) the first time you unlock them with the correct password
- An optional, **unencrypted** master password hint can be shown after two failed login attempts — never put the password itself in the hint

### Best Practices

- Use a strong master password
- If you enable OS keychain storage of the master password, your OS account login becomes the protection for that cached copy — keep your account secured and lock it when away
- Keep your SSH private keys secure
- Use SSH key authentication when possible instead of passwords
- Regularly back up your connection database (located at `~/.filemover/connections.db`)

## Architecture

### Components

- **database.go**: Encrypted credential storage using SQLite (AES-256-GCM, argon2id key derivation, automatic legacy upgrade)
- **keychain.go**: Optional OS keychain storage of the master password
- **connections.go**: Protocol implementations (SFTP, SCP, SMB, Local)
- **ui.go**: File browser widgets and UI components
- **main.go**: Main application entry point and UI orchestration

### Protocol Support

- **SFTP/SCP**: Implemented using `golang.org/x/crypto/ssh` and `github.com/pkg/sftp`
- **SMB/CIFS**: Implemented using `github.com/hirochachacha/go-smb2` with automatic detection of mounted shares
- **Rsync**: Uses system `rsync` command-line tool with SSH support for remote paths
- **Local**: Native Go `os` package for local file operations

## Dependencies

- [Fyne](https://fyne.io/) v2.7.3 - Cross-platform GUI toolkit
- [github.com/pkg/sftp](https://github.com/pkg/sftp) - SFTP client library
- [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) - SSH client, AES-256-GCM, argon2id, PBKDF2
- [github.com/hirochachacha/go-smb2](https://github.com/hirochachacha/go-smb2) - SMB2 client library
- [github.com/mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) - SQLite3 driver
- [github.com/zalando/go-keyring](https://github.com/zalando/go-keyring) - OS keychain access for optional master-password storage

## Known Limitations

- Transfer progress tracking is basic (no detailed progress bar)
- Directory transfers not yet implemented (single files only)
- File permissions may not be preserved during transfer
- SSH host key verification uses insecure mode (should be configured for production)
- SMB share discovery not implemented (must specify share name)
- Rsync requires the `rsync` command-line tool to be installed and available in PATH
- Connection timeout is configurable via preferences but defaults to 10 seconds

## Possible Future Enhancements

- [ ] Detailed transfer progress with speed and ETA
- [ ] Directory transfer support
- [ ] File permissions preservation
- [ ] SSH host key management and verification
- [ ] SMB share discovery
- [ ] Drag-and-drop file transfer
- [ ] Transfer queue management
- [ ] Connection favorites/quick connect
- [x] Configurable connection timeout via UI
- [x] File synchronization features (rsync support implemented)
- [x] Configurable panel colors

## Troubleshooting

### Connection Issues

- **SFTP/SCP**: Ensure SSH is enabled on the remote server and the port is correct (default 22)
- **SMB**: Ensure SMB is enabled and the share name is correct (common shares: C$, D$, Public)
- **Rsync**: Ensure `rsync` is installed and available in your PATH. For remote paths, ensure SSH access is configured.
- **Firewall**: Check that the required ports are open (22 for SSH, 445 for SMB)
- **Connection Timeout**: If connections timeout, check network connectivity and firewall settings. Enable debug mode to see detailed connection logs.
- **SSH Key Authentication**: If SSH key authentication fails, enable debug mode to see detailed error messages. Common issues:
  - Incorrect key file path (use `~` for home directory, e.g., `~/.ssh/id_rsa`)
  - Wrong passphrase for encrypted keys
  - Key file permissions too open (should be 600)
  - Key format not supported
- **Password Authentication**: If password authentication fails, verify the password is correct. Enable debug mode to see if the connection is reaching the authentication step.

### Database Issues

- If you forget your master password there is **no recovery**: you'll need to delete `~/.filemover/connections.db` and recreate your connections. (If you enabled OS keychain storage, the password is still retrievable on that computer via your system's keychain/credential manager.)
- Back up the database file regularly to avoid data loss. The backup is self-contained and portable — restoring it on another computer will prompt for the same master password
- **First unlock after upgrading** re-encrypts the database to the new format; if you roll back to an older version afterward, it will not be able to read the upgraded database

### Build Issues

- Ensure CGO is enabled: `export CGO_ENABLED=1`
- On Linux, install required development libraries
- On macOS, Xcode command line tools may be required

## License

This project is provided as-is, free for personal, educational and commercial use, under GNU GPL-3.0

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

## Author

Allan Marillier

## Acknowledgments

- Built with [Fyne](https://fyne.io/) - An easy-to-use GUI toolkit for Go
- Inspired by WinSCP and FileZilla
- Uses excellent open-source libraries for SSH, SFTP, and SMB support
