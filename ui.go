package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type FileBrowser struct {
	conn          ConnectionManager
	fileList      *widget.List
	pathEntry     *widget.Entry
	parent        fyne.Window
	files         []FileInfo
	currentPath   string
	onPathChange  func(string)
	onFileSelect  func(int)
	selectedIndex int
}

func NewFileBrowser(parent fyne.Window, conn ConnectionManager, onPathChange func(string), onFileSelect func(int)) *FileBrowser {
	fb := &FileBrowser{
		conn:          conn,
		parent:        parent,
		onPathChange:  onPathChange,
		onFileSelect:  onFileSelect,
		currentPath:   conn.GetCurrentPath(),
		selectedIndex: -1,
	}

	fb.pathEntry = widget.NewEntry()
	fb.pathEntry.SetText(fb.currentPath)
	if _, ok := conn.(*FileAgentConnection); ok {
		fb.pathEntry.SetPlaceHolder(`Share paths use / (root of -file-agent-root). C:\ or C: means root when sharing that Windows drive`)
	}
	fb.pathEntry.OnSubmitted = func(path string) {
		fb.NavigateTo(path)
	}

	fb.fileList = widget.NewList(
		func() int {
			return len(fb.files)
		},
		func() fyne.CanvasObject {
			icon := widget.NewIcon(nil)
			name := widget.NewLabel("")
			name.Wrapping = fyne.TextTruncate
			size := widget.NewLabel("")
			size.Alignment = fyne.TextAlignTrailing
			date := widget.NewLabel("")
			date.Alignment = fyne.TextAlignTrailing
			// Use Border: icon on left, name in center (expands), size+date on right
			rightInfo := container.NewHBox(size, widget.NewLabel("  "), date)
			return container.NewBorder(nil, nil, icon, rightInfo, name)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(fb.files) {
				return
			}
			file := fb.files[id]
			box := obj.(*fyne.Container)

			// Border container stores objects, but order may vary
			// Find objects by type to be safe
			var icon *widget.Icon
			var name *widget.Label
			var rightInfo *fyne.Container

			for _, child := range box.Objects {
				switch v := child.(type) {
				case *widget.Icon:
					icon = v
				case *widget.Label:
					// The center name label - check if it's not part of rightInfo
					if name == nil {
						name = v
					}
				case *fyne.Container:
					rightInfo = v
				}
			}

			if icon == nil || name == nil || rightInfo == nil {
				return // Safety check
			}

			size := rightInfo.Objects[0].(*widget.Label)
			date := rightInfo.Objects[2].(*widget.Label)

			if file.IsDir {
				icon.SetResource(theme.FolderIcon())
			} else {
				icon.SetResource(theme.FileIcon())
			}

			name.SetText(file.Name)
			if file.IsDir {
				size.SetText("<DIR>")
			} else {
				size.SetText(formatSize(file.Size))
			}
			date.SetText(file.ModTime.Format("2006-01-02 15:04"))
		},
	)

	fb.fileList.OnSelected = func(id widget.ListItemID) {
		if id >= len(fb.files) {
			return
		}
		fb.selectedIndex = int(id)
		if fb.onFileSelect != nil {
			fb.onFileSelect(int(id))
		}
		file := fb.files[id]
		if file.IsDir {
			var next string
			if _, ok := fb.conn.(*RsyncRemoteConnection); ok {
				next = rsyncJoinPath(fb.currentPath, file.Name)
			} else if _, ok := fb.conn.(*FileAgentConnection); ok {
				next = fileAgentJoinPath(fb.currentPath, file.Name)
			} else {
				next = filepath.Join(fb.currentPath, file.Name)
			}
			fb.NavigateTo(next)
		}
	}

	fb.Refresh()

	return fb
}

func (fb *FileBrowser) Refresh() {
	if fb.conn == nil {
		return
	}

	files, err := fb.conn.List(fb.currentPath)
	if err != nil {
		dialog.ShowError(err, fb.parent)
		return
	}

	// Sort: directories first, then files
	var dirs, filesList []FileInfo
	for _, f := range files {
		if f.IsDir {
			dirs = append(dirs, f)
		} else {
			filesList = append(filesList, f)
		}
	}

	fb.files = append(dirs, filesList...)
	fb.fileList.Refresh()
	fb.pathEntry.SetText(fb.currentPath)
	fb.conn.SetCurrentPath(fb.currentPath)

	if fb.onPathChange != nil {
		fb.onPathChange(fb.currentPath)
	}
}

func (fb *FileBrowser) NavigateTo(path string) {
	if _, ok := fb.conn.(*FileAgentConnection); ok {
		path = fileAgentNormalizeVirtualPath(path)
	}
	fb.currentPath = path
	fb.Refresh()
}

func (fb *FileBrowser) GetContainer() *fyne.Container {
	upBtn := widget.NewButton("↑", func() {
		var parent string
		if _, ok := fb.conn.(*RsyncRemoteConnection); ok {
			parent = rsyncParentDir(fb.currentPath)
		} else if fa, ok := fb.conn.(*FileAgentConnection); ok {
			parent = fileAgentUpToExisting(fa, fb.currentPath)
		} else {
			parent = filepath.Dir(fb.currentPath)
		}
		if parent != fb.currentPath {
			fb.NavigateTo(parent)
		}
	})

	refreshBtn := widget.NewButton("↻", func() {
		fb.Refresh()
	})

	pathBar := container.NewBorder(nil, nil, upBtn, refreshBtn, fb.pathEntry)

	return container.NewBorder(pathBar, nil, nil, nil, fb.fileList)
}

func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

type ConnectionDialog struct {
	dialogWindow    fyne.Window
	conn            *Connection
	nameEntry       *widget.Entry
	typeSelect      *widget.Select
	hostEntry       *widget.Entry
	portEntry       *widget.Entry
	userEntry       *widget.Entry
	passEntry       *widget.Entry
	keyPathEntry    *widget.Entry
	keyPassEntry    *widget.Entry
	remotePathEntry *widget.Entry
	sshKeyRow       fyne.CanvasObject
	keyPassRow      fyne.CanvasObject
	sshExtraArgsEntry *widget.Entry
	sshExtraRow       fyne.CanvasObject
	remoteRsyncPathEntry *widget.Entry
	remoteRsyncRow       fyne.CanvasObject
	fileAgentTLSPinEntry *widget.Entry
	fileAgentTLSRow      fyne.CanvasObject
	copyAgentCmdRow      fyne.CanvasObject
	userRow              fyne.CanvasObject
	parent               fyne.Window
	app                  fyne.App
	cancelButton         *widget.Button
}

func duplicateConnectionName(name string) string {
	const suffix = " - COPY"
	base := strings.TrimSpace(name)
	const maxLen = 20
	maxBase := maxLen - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	return strings.TrimSpace(base + suffix)
}

func NewConnectionDialog(parent fyne.Window, app fyne.App, conn *Connection, onSave func(*Connection), duplicatePick func(onChosen func(*Connection))) *ConnectionDialog {
	cd := &ConnectionDialog{
		parent: parent,
		app:    app,
	}

	if conn == nil {
		cd.conn = &Connection{
			Type: "sftp",
			Port: 22,
		}
	} else {
		cd.conn = conn
	}

	cd.nameEntry = widget.NewEntry()
	// Trim spaces and limit to 20 characters for connection names
	nameText := strings.TrimSpace(cd.conn.Name)
	if len(nameText) > 20 {
		nameText = nameText[:20]
	}
	cd.nameEntry.SetText(nameText)
	// Add validation to limit name to 20 characters and trim spaces
	cd.nameEntry.OnChanged = func(text string) {
		// Trim spaces
		trimmed := strings.TrimSpace(text)
		// Limit to 20 characters
		if len(trimmed) > 20 {
			trimmed = trimmed[:20]
			cd.nameEntry.SetText(trimmed)
		} else if trimmed != text {
			cd.nameEntry.SetText(trimmed)
		}
	}

	cd.keyPathEntry = widget.NewEntry()
	cd.keyPathEntry.SetText(cd.conn.SSHKeyPath)
	keyBrowseBtn := widget.NewButton("Browse...", func() {
		dialog.ShowFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err == nil && reader != nil {
				cd.keyPathEntry.SetText(reader.URI().Path())
				reader.Close()
			}
		}, parent)
	})

	cd.keyPassEntry = widget.NewPasswordEntry()
	cd.keyPassEntry.SetText(cd.conn.KeyPassphrase)

	// Create containers for SSH key fields that can be shown/hidden
	sshKeyContainer := container.NewBorder(nil, nil, widget.NewLabel("SSH Key Path:"), nil, container.NewBorder(nil, nil, nil, keyBrowseBtn, cd.keyPathEntry))
	keyPassContainer := container.NewBorder(nil, nil, widget.NewLabel("Key Passphrase:"), nil, cd.keyPassEntry)
	cd.sshKeyRow = sshKeyContainer
	cd.keyPassRow = keyPassContainer

	cd.typeSelect = widget.NewSelect([]string{"sftp", "scp", "smb", "rsync", "fileagent"}, func(value string) {
		cd.conn.Type = value
		cd.applyConnectionTypeSideEffects(value)
	})

	cd.hostEntry = widget.NewEntry()
	cd.hostEntry.SetText(cd.conn.Host)

	cd.portEntry = widget.NewEntry()
	cd.portEntry.SetText(fmt.Sprintf("%d", cd.conn.Port))

	cd.userEntry = widget.NewEntry()
	cd.userEntry.SetText(cd.conn.Username)

	cd.passEntry = widget.NewPasswordEntry()
	cd.passEntry.SetText(cd.conn.Password)

	cd.remotePathEntry = widget.NewEntry()
	cd.remotePathEntry.SetText(cd.conn.RemotePath)

	cd.sshExtraArgsEntry = widget.NewEntry()
	cd.sshExtraArgsEntry.SetText(cd.conn.SSHExtraArgs)
	cd.sshExtraArgsEntry.SetPlaceHolder(`e.g. -J user@jump  or  tunnel / ProxyCommand flags`)
	cd.sshExtraRow = container.NewBorder(nil, nil, widget.NewLabel("SSH extra arguments (optional):"), nil, cd.sshExtraArgsEntry)

	cd.remoteRsyncPathEntry = widget.NewEntry()
	cd.remoteRsyncPathEntry.SetText(cd.conn.RemoteRsyncPath)
	cd.remoteRsyncPathEntry.SetPlaceHolder(`e.g. C:/cygwin64/bin/rsync.exe`)
	cd.remoteRsyncRow = container.NewBorder(nil, nil, widget.NewLabel("Remote rsync program (optional):"), nil, cd.remoteRsyncPathEntry)

	cd.fileAgentTLSPinEntry = widget.NewEntry()
	cd.fileAgentTLSPinEntry.SetText(cd.conn.FileAgentTLSPin)
	cd.fileAgentTLSPinEntry.SetPlaceHolder(`SHA-256 hex printed when agent starts (64 characters)`)
	cd.fileAgentTLSRow = container.NewBorder(nil, nil, widget.NewLabel("TLS certificate pin:"), nil, cd.fileAgentTLSPinEntry)

	copyAgentBtn := widget.NewButton("Copy receiver command…", func() {
		cd.showFileAgentCommandHelper()
	})
	cd.copyAgentCmdRow = container.NewBorder(nil, nil, nil, nil,
		container.NewVBox(
			copyAgentBtn,
			widget.NewLabel("Paste in a terminal on the PC that shares files. Each agent run prints a new TLS fingerprint—update the pin in this profile after restarting the agent."),
		),
	)

	// Create buttons
	saveButton := widget.NewButton("Save", func() {
		// Trim spaces and limit to 20 characters when saving
		nameText := strings.TrimSpace(cd.nameEntry.Text)
		if len(nameText) > 20 {
			nameText = nameText[:20]
		}
		cd.conn.Name = nameText
		cd.conn.Type = cd.typeSelect.Selected
		cd.conn.Host = cd.hostEntry.Text
		fmt.Sscanf(cd.portEntry.Text, "%d", &cd.conn.Port)
		cd.conn.Username = cd.userEntry.Text
		cd.conn.Password = cd.passEntry.Text
		cd.conn.SSHKeyPath = cd.keyPathEntry.Text
		cd.conn.KeyPassphrase = cd.keyPassEntry.Text
		cd.conn.RemotePath = cd.remotePathEntry.Text
		switch cd.conn.Type {
		case "rsync", "sftp", "scp":
			cd.conn.SSHExtraArgs = strings.TrimSpace(cd.sshExtraArgsEntry.Text)
		default:
			cd.conn.SSHExtraArgs = ""
		}
		if cd.conn.Type == "rsync" {
			cd.conn.RemoteRsyncPath = strings.TrimSpace(cd.remoteRsyncPathEntry.Text)
		} else {
			cd.conn.RemoteRsyncPath = ""
		}

		if cd.conn.Type == "fileagent" {
			if strings.TrimSpace(cd.conn.Host) == "" {
				dialog.ShowError(fmt.Errorf("host is required for LAN file agent"), cd.parent)
				return
			}
			if cd.conn.Port <= 0 || cd.conn.Port > 65535 {
				dialog.ShowError(fmt.Errorf("port must be between 1 and 65535"), cd.parent)
				return
			}
			if strings.TrimSpace(cd.conn.Password) == "" {
				dialog.ShowError(fmt.Errorf("pre-shared key is required for LAN file agent"), cd.parent)
				return
			}
			pin := normalizeCertPinHex(cd.fileAgentTLSPinEntry.Text)
			if len(pin) != 64 {
				dialog.ShowError(fmt.Errorf("TLS certificate pin must be 64 hexadecimal characters (SHA-256 fingerprint from the agent)"), cd.parent)
				return
			}
			cd.conn.FileAgentTLSPin = pin
			cd.conn.Username = ""
		} else {
			cd.conn.FileAgentTLSPin = ""
		}

		onSave(cd.conn)
		cd.dialogWindow.Close()
	})

	cancelButton := widget.NewButton("Cancel", func() {
		cd.dialogWindow.Close()
	})

	formRows := []fyne.CanvasObject{}
	if duplicatePick != nil && (conn == nil || conn.ID == 0) {
		formRows = append(formRows, widget.NewButton("Duplicate from saved…", func() {
			duplicatePick(func(src *Connection) {
				cd.ApplyDuplicateFrom(src)
			})
		}))
	}
	cd.userRow = container.NewBorder(nil, nil, widget.NewLabel("Username:"), nil, cd.userEntry)
	formRows = append(formRows,
		container.NewBorder(nil, nil, widget.NewLabel("Name:"), nil, cd.nameEntry),
		container.NewBorder(nil, nil, widget.NewLabel("Type:"), nil, cd.typeSelect),
		container.NewBorder(nil, nil, widget.NewLabel("Host:"), nil, cd.hostEntry),
		container.NewBorder(nil, nil, widget.NewLabel("Port:"), nil, cd.portEntry),
		cd.userRow,
		container.NewBorder(nil, nil, widget.NewLabel("Password:"), nil, cd.passEntry),
		sshKeyContainer,
		keyPassContainer,
		container.NewBorder(nil, nil, widget.NewLabel("Remote Path:"), nil, cd.remotePathEntry),
		cd.fileAgentTLSRow,
		cd.copyAgentCmdRow,
		cd.sshExtraRow,
		cd.remoteRsyncRow,
	)
	// SetSelected runs the type callback; must run after passEntry, portEntry, userRow, etc. exist.
	cd.typeSelect.SetSelected(cd.conn.Type)

	formItems := container.NewVBox(formRows...)

	// Allow Enter key in the last field to submit
	cd.remotePathEntry.OnSubmitted = func(_ string) {
		saveButton.OnTapped()
	}

	// Create custom content with form and buttons - simple VBox layout
	buttonsContainer := container.NewHBox(saveButton, cancelButton)
	innerContent := container.NewVBox(
		container.NewPadded(formItems),
		container.NewPadded(buttonsContainer),
	)

	// Create a focusable wrapper widget that handles Esc key
	escWrapper := &escKeyWrapper{
		content: container.NewPadded(innerContent),
		onEsc:   func() { cancelButton.OnTapped() },
	}
	escWrapper.ExtendBaseWidget(escWrapper)

	// Create custom window instead of dialog to avoid default close button
	cd.dialogWindow = cd.app.NewWindow("Connection Settings")
	cd.dialogWindow.Resize(fyne.NewSize(500, 500))
	cd.dialogWindow.SetContent(escWrapper)

	// Store cancel button reference for Esc key handler
	cd.cancelButton = cancelButton

	return cd
}

func (cd *ConnectionDialog) applyConnectionTypeSideEffects(value string) {
	if cd.portEntry != nil {
		if value == "fileagent" {
			if cd.conn.Port == 0 || cd.conn.Port == 22 || cd.conn.Port == 445 {
				cd.conn.Port = 9742
				cd.portEntry.SetText("9742")
			}
		} else if value == "smb" && cd.conn.Port == 22 {
			cd.conn.Port = 445
			cd.portEntry.SetText("445")
		} else if value != "smb" && cd.conn.Port == 445 {
			cd.conn.Port = 22
			cd.portEntry.SetText("22")
		}
	}
	if cd.userRow != nil {
		if value == "fileagent" {
			cd.userRow.Hide()
		} else {
			cd.userRow.Show()
		}
	}
	if cd.passEntry != nil {
		switch value {
		case "fileagent":
			cd.passEntry.SetPlaceHolder("Pre-shared key (must match -file-agent-psk on receiver)")
		default:
			cd.passEntry.SetPlaceHolder("")
		}
	}
	if cd.sshKeyRow != nil && cd.keyPassRow != nil {
		if value == "smb" || value == "rsync" || value == "fileagent" {
			cd.sshKeyRow.Hide()
			cd.keyPassRow.Hide()
		} else {
			cd.sshKeyRow.Show()
			cd.keyPassRow.Show()
		}
	}
	if cd.sshExtraRow != nil {
		switch value {
		case "rsync", "sftp", "scp":
			cd.sshExtraRow.Show()
		default:
			cd.sshExtraRow.Hide()
		}
	}
	if cd.remoteRsyncRow != nil {
		if value == "rsync" {
			cd.remoteRsyncRow.Show()
		} else {
			cd.remoteRsyncRow.Hide()
		}
	}
	if cd.fileAgentTLSRow != nil {
		if value == "fileagent" {
			cd.fileAgentTLSRow.Show()
		} else {
			cd.fileAgentTLSRow.Hide()
		}
	}
	if cd.copyAgentCmdRow != nil {
		if value == "fileagent" {
			cd.copyAgentCmdRow.Show()
		} else {
			cd.copyAgentCmdRow.Hide()
		}
	}
	if cd.remotePathEntry != nil {
		switch value {
		case "fileagent":
			cd.remotePathEntry.SetPlaceHolder(`Virtual path under agent root: usually /. Windows: use / for drive root when the agent shares C:\ (typing C:\ here means a subfolder, not the drive).`)
		default:
			cd.remotePathEntry.SetPlaceHolder("")
		}
	}
}

// showFileAgentCommandHelper opens a small window to preview and copy the CLI used on the receiving machine.
func (cd *ConnectionDialog) showFileAgentCommandHelper() {
	w := cd.app.NewWindow("LAN file agent — receiver command")
	w.Resize(fyne.NewSize(720, 280))

	rootE := widget.NewEntry()
	rootE.SetText(".")
	rootE.SetPlaceHolder("Directory to share (on the receiver)")

	ttlE := widget.NewEntry()
	ttlE.SetPlaceHolder(`optional auto-stop, e.g. 30m or 2h (empty = until Ctrl+C)`)

	preview := widget.NewMultiLineEntry()
	preview.Wrapping = fyne.TextWrapBreak

	// SetChecked can invoke OnChanged immediately; start with a no-op until the real rebuild is assigned below.
	rebuild := func() {}
	lanC := widget.NewCheck("Restrict clients to LAN/private/link-local IPs (-file-agent-lan-only)", func(bool) { rebuild() })
	lanC.SetChecked(true)
	pskC := widget.NewCheck("Include pre-shared key in command (avoid if clipboard is not private)", func(bool) { rebuild() })

	rebuild = func() {
		var port int
		fmt.Sscanf(cd.portEntry.Text, "%d", &port)
		if port <= 0 {
			port = 9742
		}
		listen := fmt.Sprintf(":%d", port)
		exe, err := os.Executable()
		if err != nil {
			exe = "filemover"
		}
		var ttl time.Duration
		if ts := strings.TrimSpace(ttlE.Text); ts != "" {
			d, err := time.ParseDuration(ts)
			if err != nil {
				preview.SetText("(Fix TTL: " + err.Error() + ")")
				return
			}
			ttl = d
		}
		psk := cd.passEntry.Text
		if !pskC.Checked {
			psk = ""
		}
		preview.SetText(BuildFileAgentReceiverCommand(exe, listen, rootE.Text, psk, pskC.Checked, lanC.Checked, ttl))
	}

	rootE.OnChanged = func(_ string) { rebuild() }
	ttlE.OnChanged = func(_ string) { rebuild() }
	rebuild()

	copyBtn := widget.NewButtonWithIcon("Copy to clipboard", theme.ContentCopyIcon(), func() {
		rebuild()
		if strings.HasPrefix(preview.Text, "(Fix TTL") {
			dialog.ShowError(fmt.Errorf("enter a valid duration or clear the TTL field"), w)
			return
		}
		w.Clipboard().SetContent(preview.Text)
		dialog.ShowInformation("Copied", "Command copied to clipboard.", w)
	})
	closeBtn := widget.NewButton("Close", func() { w.Close() })

	w.SetContent(container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Executable path comes from this app. Edit share directory and options, then Copy."),
			rootE,
			ttlE,
			lanC,
			pskC,
			preview,
			container.NewHBox(copyBtn, closeBtn),
		),
		nil, nil, nil,
	))
	w.SetCloseIntercept(func() { w.Close() })
	w.CenterOnScreen()
	w.Show()
}

// ApplyDuplicateFrom copies an existing profile into this dialog as a new entry (ID cleared, name suffixed).
func (cd *ConnectionDialog) ApplyDuplicateFrom(src *Connection) {
	if src == nil {
		return
	}
	*cd.conn = *src
	cd.conn.ID = 0
	cd.conn.Name = duplicateConnectionName(src.Name)

	cd.nameEntry.SetText(strings.TrimSpace(cd.conn.Name))
	cd.typeSelect.SetSelected(cd.conn.Type)
	cd.applyConnectionTypeSideEffects(cd.conn.Type)
	cd.hostEntry.SetText(cd.conn.Host)
	cd.portEntry.SetText(fmt.Sprintf("%d", cd.conn.Port))
	cd.userEntry.SetText(cd.conn.Username)
	cd.passEntry.SetText(cd.conn.Password)
	cd.keyPathEntry.SetText(cd.conn.SSHKeyPath)
	cd.keyPassEntry.SetText(cd.conn.KeyPassphrase)
	cd.remotePathEntry.SetText(cd.conn.RemotePath)
	if cd.sshExtraArgsEntry != nil {
		cd.sshExtraArgsEntry.SetText(cd.conn.SSHExtraArgs)
	}
	if cd.remoteRsyncPathEntry != nil {
		cd.remoteRsyncPathEntry.SetText(cd.conn.RemoteRsyncPath)
	}
	if cd.fileAgentTLSPinEntry != nil {
		cd.fileAgentTLSPinEntry.SetText(cd.conn.FileAgentTLSPin)
	}
}

// DialogWindow returns the connection editor window (for single-instance tracking).
func (cd *ConnectionDialog) DialogWindow() fyne.Window {
	return cd.dialogWindow
}

func (cd *ConnectionDialog) Show() {
	cd.dialogWindow.Show()
	cd.dialogWindow.CenterOnScreen()

	// Focus the wrapper so it can receive Esc key events
	go func() {
		time.Sleep(100 * time.Millisecond)
		fyne.Do(func() {
			if cd.dialogWindow != nil && cd.dialogWindow.Canvas() != nil {
				// Focus the wrapper so Esc key works
				if escWrapper, ok := cd.dialogWindow.Content().(*escKeyWrapper); ok {
					cd.dialogWindow.Canvas().Focus(escWrapper)
				}
			}
		})
	}()
}

type TransferProgress struct {
	dialog dialog.Dialog
	bar    *widget.ProgressBar
	label  *widget.Label
}

func NewTransferProgress(parent fyne.Window) *TransferProgress {
	bar := widget.NewProgressBar()
	label := widget.NewLabel("Transferring...")

	content := container.NewVBox(label, bar)
	d := dialog.NewCustom("Transfer Progress", "Cancel", content, parent)

	return &TransferProgress{
		dialog: d,
		bar:    bar,
		label:  label,
	}
}

func (tp *TransferProgress) Show() {
	tp.dialog.Show()
}

func (tp *TransferProgress) Update(progress float64, message string) {
	fyne.Do(func() {
		tp.bar.SetValue(progress)
		tp.label.SetText(message)
	})
}

func (tp *TransferProgress) Hide() {
	fyne.Do(func() {
		tp.dialog.Hide()
	})
}

func TransferFile(src ConnectionManager, srcPath string, dst ConnectionManager, dstPath string, parent fyne.Window) error {
	progress := NewTransferProgress(parent)
	progress.Show()

	go func() {
		defer func() {
			fyne.Do(func() {
				progress.Hide()
			})
		}()

		reader, err := src.ReadFile(srcPath)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("failed to read source file: %w", err), parent)
			})
			return
		}
		defer reader.Close()

		err = dst.WriteFile(dstPath, reader)
		if err != nil {
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("failed to write destination file: %w", err), parent)
			})
		} else {
			fyne.Do(func() {
				dialog.ShowInformation("Success", "File transferred successfully", parent)
			})
		}
	}()

	return nil
}
