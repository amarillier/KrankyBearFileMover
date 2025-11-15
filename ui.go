package main

import (
	"fmt"
	"path/filepath"

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
			size := widget.NewLabel("")
			date := widget.NewLabel("")
			return container.NewHBox(icon, name, size, date)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(fb.files) {
				return
			}
			file := fb.files[id]
			box := obj.(*fyne.Container)
			children := box.Objects

			icon := children[0].(*widget.Icon)
			name := children[1].(*widget.Label)
			size := children[2].(*widget.Label)
			date := children[3].(*widget.Label)

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
			fb.NavigateTo(filepath.Join(fb.currentPath, file.Name))
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
	fb.currentPath = path
	fb.Refresh()
}

func (fb *FileBrowser) GetContainer() *fyne.Container {
	upBtn := widget.NewButton("↑", func() {
		parent := filepath.Dir(fb.currentPath)
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
	dialog    dialog.Dialog
	conn      *Connection
	nameEntry *widget.Entry
	typeSelect *widget.Select
	hostEntry *widget.Entry
	portEntry *widget.Entry
	userEntry *widget.Entry
	passEntry *widget.Entry
	keyPathEntry *widget.Entry
	keyPassEntry *widget.Entry
	remotePathEntry *widget.Entry
}

func NewConnectionDialog(parent fyne.Window, conn *Connection, onSave func(*Connection)) *ConnectionDialog {
	cd := &ConnectionDialog{}

	if conn == nil {
		cd.conn = &Connection{
			Type: "sftp",
			Port: 22,
		}
	} else {
		cd.conn = conn
	}

	cd.nameEntry = widget.NewEntry()
	cd.nameEntry.SetText(cd.conn.Name)

	cd.typeSelect = widget.NewSelect([]string{"sftp", "scp", "smb"}, func(value string) {
		cd.conn.Type = value
		if value == "smb" && cd.conn.Port == 22 {
			cd.conn.Port = 445
			cd.portEntry.SetText("445")
		} else if value != "smb" && cd.conn.Port == 445 {
			cd.conn.Port = 22
			cd.portEntry.SetText("22")
		}
	})
	cd.typeSelect.SetSelected(cd.conn.Type)

	cd.hostEntry = widget.NewEntry()
	cd.hostEntry.SetText(cd.conn.Host)

	cd.portEntry = widget.NewEntry()
	cd.portEntry.SetText(fmt.Sprintf("%d", cd.conn.Port))

	cd.userEntry = widget.NewEntry()
	cd.userEntry.SetText(cd.conn.Username)

	cd.passEntry = widget.NewPasswordEntry()
	cd.passEntry.SetText(cd.conn.Password)

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

	cd.remotePathEntry = widget.NewEntry()
	cd.remotePathEntry.SetText(cd.conn.RemotePath)

	// Create buttons
	saveButton := widget.NewButton("Save", func() {
		cd.conn.Name = cd.nameEntry.Text
		cd.conn.Type = cd.typeSelect.Selected
		cd.conn.Host = cd.hostEntry.Text
		fmt.Sscanf(cd.portEntry.Text, "%d", &cd.conn.Port)
		cd.conn.Username = cd.userEntry.Text
		cd.conn.Password = cd.passEntry.Text
		cd.conn.SSHKeyPath = cd.keyPathEntry.Text
		cd.conn.KeyPassphrase = cd.keyPassEntry.Text
		cd.conn.RemotePath = cd.remotePathEntry.Text

		onSave(cd.conn)
		cd.dialog.Hide()
	})
	
	cancelButton := widget.NewButton("Cancel", func() {
		cd.dialog.Hide()
	})

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Name", Widget: cd.nameEntry},
			{Text: "Type", Widget: cd.typeSelect},
			{Text: "Host", Widget: cd.hostEntry},
			{Text: "Port", Widget: cd.portEntry},
			{Text: "Username", Widget: cd.userEntry},
			{Text: "Password", Widget: cd.passEntry},
			{Text: "SSH Key Path", Widget: container.NewBorder(nil, nil, nil, keyBrowseBtn, cd.keyPathEntry)},
			{Text: "Key Passphrase", Widget: cd.keyPassEntry},
			{Text: "Remote Path", Widget: cd.remotePathEntry},
		},
		OnSubmit: func() {
			saveButton.OnTapped()
		},
	}

	// Create custom content with form and buttons
	content := container.NewVBox(
		form,
		container.NewHBox(saveButton, cancelButton),
	)

	cd.dialog = dialog.NewCustom("Connection Settings", "", content, parent)
	cd.dialog.Resize(fyne.NewSize(500, 400))
	return cd
}

func (cd *ConnectionDialog) Show() {
	cd.dialog.Show()
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

