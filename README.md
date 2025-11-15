# KrankyBear FileMover

A cross-platform dual-pane file transfer application built with Go and the Fyne GUI library. Similar to WinSCP or FileZilla, this application provides an intuitive interface for transferring files between local and remote systems using SSH (SFTP/SCP) and SMB/CIFS protocols.

## Features

### Core Functionality

- **Dual-Pane Interface**: Side-by-side file browsers for easy file transfer between local and remote locations
- **Multiple Protocol Support**:
  - **SFTP/SCP**: Secure file transfer over SSH
  - **SMB/CIFS**: Windows file sharing and network drives
  - **Local File System**: Browse and manage local files
- **Secure Credential Storage**: 
  - Password-protected encrypted database (AES-256)
  - Master password protection
  - Encrypted storage of passwords and SSH key passphrases
- **Connection Management**:
  - Save multiple connection profiles
  - Support for password authentication
  - SSH key authentication with optional passphrase
  - Easy connection switching between left and right panes

### User Interface

- **Modern Fyne GUI**: Cross-platform native look and feel
- **File Browser Features**:
  - Directory navigation with up button and path entry
  - File listing with size, date, and directory indicators
  - Click directories to navigate, select files to transfer
- **Transfer Operations**:
  - One-click file transfer between panes
  - Visual transfer progress (basic implementation)
  - Automatic refresh after transfers

### Cross-Platform Support

- **macOS**: macOS 10.13 (High Sierra) or later
- **Windows**: Windows 10 or later
- **Linux**: Works on all desktop environments (GNOME, KDE, XFCE, Cinnamon, MATE, etc.) with X11 or Wayland

## Installation

### Prerequisites

- Go 1.21 or later
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

1. **Set Master Password**: When you first launch the application, you'll be prompted to enter a master password. This password encrypts your stored connection credentials. **Remember this password** - you'll need it every time you launch the application.

2. **Create a Connection**:
   - Click "New Connection" button
   - Fill in the connection details:
     - **Name**: A friendly name for this connection
     - **Type**: Choose SFTP, SCP, or SMB
     - **Host**: IP address or hostname
     - **Port**: Default ports (22 for SSH, 445 for SMB)
     - **Username**: Your username
     - **Password**: Your password (optional if using SSH key)
     - **SSH Key Path**: Path to your SSH private key (optional)
     - **Key Passphrase**: Passphrase for your SSH key (if encrypted)
     - **Remote Path**: Initial directory to connect to
   - Click "Save"

3. **Connect**:
   - Select a connection from the list
   - Click "Connect Left" or "Connect Right" to connect to that pane
   - The file browser will update to show the remote directory

4. **Transfer Files**:
   - Navigate to the file you want to transfer
   - Select the file in the source pane
   - Click "→ Transfer" to copy from left to right, or "← Transfer" to copy from right to left
   - The file will be transferred to the current directory in the destination pane

### Managing Connections

- **Edit Connection**: Select a connection and click "Edit Connection"
- **Delete Connection**: Select a connection and click "Delete Connection"
- **Refresh List**: Click "Refresh" to reload connections from the database

### Disconnecting

- Click "Disconnect Left" or "Disconnect Right" to disconnect from a remote server
- The pane will revert to showing the local file system

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
- Encryption uses AES-256 with PBKDF2 key derivation
- The master password is never stored - it's used only to derive the encryption key
- Passwords and SSH key passphrases are encrypted before storage

### Best Practices

- Use a strong master password
- Keep your SSH private keys secure
- Use SSH key authentication when possible instead of passwords
- Regularly back up your connection database (located at `~/.filemover/connections.db`)

## Architecture

### Components

- **database.go**: Encrypted credential storage using SQLite
- **connections.go**: Protocol implementations (SFTP, SCP, SMB, Local)
- **ui.go**: File browser widgets and UI components
- **main.go**: Main application entry point and UI orchestration

### Protocol Support

- **SFTP/SCP**: Implemented using `golang.org/x/crypto/ssh` and `github.com/pkg/sftp`
- **SMB/CIFS**: Implemented using `github.com/hirochachacha/go-smb2`
- **Local**: Native Go `os` package for local file operations

## Dependencies

- [Fyne](https://fyne.io/) v2.6.3 - Cross-platform GUI toolkit
- [github.com/pkg/sftp](https://github.com/pkg/sftp) - SFTP client library
- [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh) - SSH client implementation
- [github.com/hirochachacha/go-smb2](https://github.com/hirochachacha/go-smb2) - SMB2 client library
- [github.com/mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) - SQLite3 driver

## Known Limitations

- Transfer progress tracking is basic (no detailed progress bar)
- Directory transfers not yet implemented (single files only)
- File permissions may not be preserved during transfer
- SSH host key verification uses insecure mode (should be configured for production)
- SMB share discovery not implemented (must specify share name)

## Future Enhancements

- [ ] Detailed transfer progress with speed and ETA
- [ ] Directory transfer support
- [ ] File permissions preservation
- [ ] SSH host key management and verification
- [ ] SMB share discovery
- [ ] Drag-and-drop file transfer
- [ ] File synchronization features
- [ ] Transfer queue management
- [ ] Connection favorites/quick connect

## Troubleshooting

### Connection Issues

- **SFTP/SCP**: Ensure SSH is enabled on the remote server and the port is correct (default 22)
- **SMB**: Ensure SMB is enabled and the share name is correct (common shares: C$, D$, Public)
- **Firewall**: Check that the required ports are open (22 for SSH, 445 for SMB)

### Database Issues

- If you forget your master password, you'll need to delete `~/.filemover/connections.db` and recreate your connections
- Back up the database file regularly to avoid data loss

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
