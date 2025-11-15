package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

type App struct {
	app                fyne.App
	window             fyne.Window
	db                 *Database
	leftBrowser        *FileBrowser
	rightBrowser       *FileBrowser
	leftConn           ConnectionManager
	rightConn          ConnectionManager
	connectionList     *widget.List
	connections        []*Connection
	selectedConnection int
	leftSelectedFile   int
	rightSelectedFile  int
	helpWindow         fyne.Window
	updateWindow       fyne.Window
	openDialogs        map[string]fyne.Window
	childWindows       []fyne.Window
	toolbar            fyne.CanvasObject
	sidebar            fyne.CanvasObject
}

const (
	// appName    = "KrankyBear FileMover"
	appVersion = "0.1.0" // see FyneApp.toml
	appAuthor  = "Allan Marillier"
)

var appName = "KrankyBear FileMover"
var appCopyright = "Copyright (c) Allan Marillier, 2025-" + strconv.Itoa(time.Now().Year())

func NewApp() (*App, error) {
	myApp := app.NewWithID("com.github.amarillier.FileMover")
	myApp.Settings().SetTheme(newAppTheme())

	window := myApp.NewWindow("FileMover - Dual Pane File Transfer")
	window.Resize(fyne.NewSize(1200, 800))

	app := &App{
		app:                myApp,
		window:             window,
		selectedConnection: -1,
		leftSelectedFile:   -1,
		rightSelectedFile:  -1,
		openDialogs:        make(map[string]fyne.Window),
		childWindows:       []fyne.Window{},
	}

	return app, nil
}

func (a *App) showPasswordDialog(parent fyne.Window) {
	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Enter master password")
	passwordEntry.Resize(fyne.NewSize(300, passwordEntry.MinSize().Height))

	// Create password dialog first so we can reference it in callbacks
	var passwordDialog dialog.Dialog

	submitPassword := func() {
		masterPassword := passwordEntry.Text
		if masterPassword == "" {
			dialog.ShowError(fmt.Errorf("master password cannot be empty"), parent)
			return
		}

		db, err := NewDatabase(masterPassword)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to initialize database: %w", err), parent)
			return
		}

		// Verify the password is correct
		if err := db.VerifyPassword(); err != nil {
			db.Close()
			dialog.ShowError(fmt.Errorf("invalid master password"), parent)
			return
		}

		a.db = db
		fyne.Do(func() {
			passwordDialog.Hide()
			a.setupUI()
		})
	}

	// Allow Enter key to submit
	passwordEntry.OnSubmitted = func(_ string) {
		submitPassword()
	}

	// Create custom content with buttons
	content := container.NewVBox(
		widget.NewLabel("Master Password:"),
		passwordEntry,
		container.NewHBox(
			widget.NewButton("Submit", submitPassword),
			widget.NewButton("Forgot Password", func() {
				a.showForgotPasswordDialog(parent, passwordDialog)
			}),
			widget.NewButton("Cancel", func() {
				os.Exit(0)
			}),
		),
	)

	passwordDialog = dialog.NewCustom("Enter Master Password", "", content, parent)
	passwordDialog.Resize(fyne.NewSize(450, 200))
	passwordDialog.Show()
}

func (a *App) showForgotPasswordDialog(parent fyne.Window, passwordDialog dialog.Dialog) {
	warningText := "WARNING: This will permanently delete your encrypted database and all saved connections.\n\n"
	warningText += "This action cannot be undone. All your saved connection credentials will be lost.\n\n"
	warningText += "Do you want to continue?"

	label := widget.NewLabel(warningText)
	label.Wrapping = fyne.TextWrapWord

	// Create forgot password dialog first so we can reference it in callbacks
	var forgotDialog dialog.Dialog

	content := container.NewVBox(
		label,
		container.NewHBox(
			widget.NewButton("Yes, Delete Database", func() {
				// Get database path
				homeDir, err := os.UserHomeDir()
				if err != nil {
					dialog.ShowError(fmt.Errorf("failed to get home directory: %w", err), parent)
					return
				}

				dbPath := filepath.Join(homeDir, ".filemover", "connections.db")

				// Close any open database connection
				if a.db != nil {
					a.db.Close()
					a.db = nil
				}

				// Delete the database file
				if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
					dialog.ShowError(fmt.Errorf("failed to delete database: %w", err), parent)
					return
				}

				// Close the forgot password dialog
				fyne.Do(func() {
					forgotDialog.Hide()
					// Close the password dialog
					passwordDialog.Hide()

					// Show password dialog again to create new database
					a.showPasswordDialog(parent)
				})
			}),
			widget.NewButton("Cancel", func() {
				forgotDialog.Hide()
			}),
		),
	)

	forgotDialog = dialog.NewCustom("Forgot Password - Reset Database", "", content, parent)
	forgotDialog.Resize(fyne.NewSize(500, 200))
	forgotDialog.Show()
}

func (a *App) setupUI() {
	// Initialize with local connections
	a.leftConn = NewLocalConnection()
	a.rightConn = NewLocalConnection()

	// Create file browsers
	a.leftBrowser = NewFileBrowser(a.window, a.leftConn, func(path string) {
		// Path change callback
	}, func(id int) {
		a.leftSelectedFile = id
	})
	a.rightBrowser = NewFileBrowser(a.window, a.rightConn, func(path string) {
		// Path change callback
	}, func(id int) {
		a.rightSelectedFile = id
	})

	// Connection list
	a.connectionList = widget.NewList(
		func() int {
			return len(a.connections)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < len(a.connections) {
				label := obj.(*widget.Label)
				conn := a.connections[id]
				label.SetText(fmt.Sprintf("%s (%s@%s)", conn.Name, conn.Username, conn.Host))
			}
		},
	)

	a.connectionList.OnSelected = func(id widget.ListItemID) {
		if id < len(a.connections) {
			a.selectedConnection = int(id)
		}
	}

	// Toolbar
	toolbar := container.NewHBox(
		widget.NewButton("New Connection", func() {
			a.showConnectionDialog(nil)
		}),
		widget.NewButton("Edit Connection", func() {
			if a.selectedConnection >= 0 && a.selectedConnection < len(a.connections) {
				a.showConnectionDialog(a.connections[a.selectedConnection])
			}
		}),
		widget.NewButton("Delete Connection", func() {
			if a.selectedConnection >= 0 && a.selectedConnection < len(a.connections) {
				conn := a.connections[a.selectedConnection]
				dialog.ShowConfirm("Delete Connection",
					fmt.Sprintf("Delete connection '%s'?", conn.Name),
					func(ok bool) {
						if ok {
							if err := a.db.DeleteConnection(conn.ID); err != nil {
								dialog.ShowError(err, a.window)
							} else {
								a.loadConnections()
								a.selectedConnection = -1
							}
						}
					}, a.window)
			}
		}),
		widget.NewSeparator(),
		widget.NewButton("Connect Left", func() {
			if a.selectedConnection >= 0 && a.selectedConnection < len(a.connections) {
				a.connectTo(a.connections[a.selectedConnection], false)
			}
		}),
		widget.NewButton("Connect Right", func() {
			if a.selectedConnection >= 0 && a.selectedConnection < len(a.connections) {
				a.connectTo(a.connections[a.selectedConnection], true)
			}
		}),
		widget.NewButton("Disconnect Left", func() {
			if a.leftConn != nil {
				a.leftConn.Disconnect()
				a.leftConn = NewLocalConnection()
				a.leftBrowser = NewFileBrowser(a.window, a.leftConn, nil, func(id int) {
					a.leftSelectedFile = id
				})
				a.updateLayout()
			}
		}),
		widget.NewButton("Disconnect Right", func() {
			if a.rightConn != nil {
				a.rightConn.Disconnect()
				a.rightConn = NewLocalConnection()
				a.rightBrowser = NewFileBrowser(a.window, a.rightConn, nil, func(id int) {
					a.rightSelectedFile = id
				})
				a.updateLayout()
			}
		}),
		widget.NewSeparator(),
		widget.NewButton("→ Transfer", func() {
			if a.leftSelectedFile >= 0 && a.leftSelectedFile < len(a.leftBrowser.files) {
				file := a.leftBrowser.files[a.leftSelectedFile]
				if !file.IsDir {
					srcPath := filepath.Join(a.leftBrowser.currentPath, file.Name)
					dstPath := filepath.Join(a.rightBrowser.currentPath, file.Name)
					TransferFile(a.leftConn, srcPath, a.rightConn, dstPath, a.window)
					go func() {
						time.Sleep(500 * time.Millisecond)
						fyne.Do(func() {
							a.rightBrowser.Refresh()
						})
					}()
				}
			}
		}),
		widget.NewButton("← Transfer", func() {
			if a.rightSelectedFile >= 0 && a.rightSelectedFile < len(a.rightBrowser.files) {
				file := a.rightBrowser.files[a.rightSelectedFile]
				if !file.IsDir {
					srcPath := filepath.Join(a.rightBrowser.currentPath, file.Name)
					dstPath := filepath.Join(a.leftBrowser.currentPath, file.Name)
					TransferFile(a.rightConn, srcPath, a.leftConn, dstPath, a.window)
					go func() {
						time.Sleep(500 * time.Millisecond)
						fyne.Do(func() {
							a.leftBrowser.Refresh()
						})
					}()
				}
			}
		}),
	)

	// Left side: connection list
	leftSidebar := container.NewBorder(
		widget.NewLabel("Connections"),
		widget.NewButton("Refresh", func() {
			a.loadConnections()
		}),
		nil, nil,
		a.connectionList,
	)

	// Main content: dual panes
	mainContent := container.NewHSplit(
		a.leftBrowser.GetContainer(),
		a.rightBrowser.GetContainer(),
	)
	mainContent.SetOffset(0.5)

	// Store toolbar and sidebar for later updates
	a.toolbar = toolbar
	a.sidebar = leftSidebar

	// Overall layout
	content := container.NewBorder(
		toolbar,
		nil,
		leftSidebar,
		nil,
		mainContent,
	)

	a.window.SetContent(content)
	a.loadConnections()
	a.setupMenu()
}

func (a *App) loadConnections() {
	connections, err := a.db.GetConnections()
	if err != nil {
		dialog.ShowError(err, a.window)
		return
	}
	a.connections = connections
	a.connectionList.Refresh()
}

func (a *App) showConnectionDialog(conn *Connection) {
	// First, verify master password
	a.verifyPasswordForSettings(func() {
		connDialog := NewConnectionDialog(a.window, conn, func(savedConn *Connection) {
			if err := a.db.SaveConnection(savedConn); err != nil {
				dialog.ShowError(err, a.window)
			} else {
				a.loadConnections()
			}
		})
		connDialog.Show()
	})
}

func (a *App) verifyPasswordForSettings(onSuccess func()) {
	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Enter master password")
	passwordEntry.Resize(fyne.NewSize(300, passwordEntry.MinSize().Height))

	var verifyDialog dialog.Dialog

	verifyPassword := func() {
		masterPassword := passwordEntry.Text
		if masterPassword == "" {
			dialog.ShowError(fmt.Errorf("master password cannot be empty"), a.window)
			return
		}

		// Create a temporary database connection to verify password
		db, err := NewDatabase(masterPassword)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to initialize database: %w", err), a.window)
			return
		}

		// Verify the password is correct
		if err := db.VerifyPassword(); err != nil {
			db.Close()
			dialog.ShowError(fmt.Errorf("invalid master password"), a.window)
			return
		}

		db.Close()
		fyne.Do(func() {
			verifyDialog.Hide()
			onSuccess()
		})
	}

	// Allow Enter key to submit
	passwordEntry.OnSubmitted = func(_ string) {
		verifyPassword()
	}

	// Create custom content with buttons
	content := container.NewVBox(
		widget.NewLabel("Enter Master Password to Access Settings:"),
		passwordEntry,
		container.NewHBox(
			widget.NewButton("OK", verifyPassword),
			widget.NewButton("Cancel", func() {
				verifyDialog.Hide()
			}),
		),
	)

	verifyDialog = dialog.NewCustom("Verify Master Password", "", content, a.window)
	verifyDialog.Resize(fyne.NewSize(400, 180))
	verifyDialog.Show()
}

func (a *App) connectTo(conn *Connection, rightPane bool) {
	var newConn ConnectionManager

	switch conn.Type {
	case "sftp", "scp":
		newConn = NewSFTPConnection(conn)
	case "smb":
		newConn = NewSMBConnection(conn)
	default:
		dialog.ShowError(fmt.Errorf("unsupported connection type: %s", conn.Type), a.window)
		return
	}

	progress := dialog.NewProgressInfinite("Connecting", "Connecting to "+conn.Name, a.window)
	progress.Show()

	go func() {
		defer func() {
			fyne.Do(func() {
				progress.Hide()
			})
		}()
		if err := newConn.Connect(); err != nil {
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("failed to connect: %w", err), a.window)
			})
			return
		}

		// Update UI - must be called from main thread
		fyne.Do(func() {
			if rightPane {
				if a.rightConn != nil {
					a.rightConn.Disconnect()
				}
				a.rightConn = newConn
				a.rightBrowser = NewFileBrowser(a.window, a.rightConn, nil, func(id int) {
					a.rightSelectedFile = id
				})
			} else {
				if a.leftConn != nil {
					a.leftConn.Disconnect()
				}
				a.leftConn = newConn
				a.leftBrowser = NewFileBrowser(a.window, a.leftConn, nil, func(id int) {
					a.leftSelectedFile = id
				})
			}
			a.updateLayout()
		})
	}()
}

func (a *App) updateLayout() {
	// Update the main content area with current browsers
	mainContent := container.NewHSplit(
		a.leftBrowser.GetContainer(),
		a.rightBrowser.GetContainer(),
	)
	mainContent.SetOffset(0.5)

	// Update content with new browsers, preserving toolbar and sidebar
	content := container.NewBorder(
		a.toolbar,
		nil,
		a.sidebar,
		nil,
		mainContent,
	)

	a.window.SetContent(content)
}

func (a *App) setupMenu() {
	settingsMenu := fyne.NewMenu("Settings",
		fyne.NewMenuItem("Change Master Password", func() {
			a.showChangePasswordDialog()
		}),
	)

	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("Help", func() {
			a.showHelpDialog()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Check for Update", func() {
			a.checkForUpdate()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("About", func() {
			a.showAboutDialog()
		}),
	)

	mainMenu := fyne.NewMainMenu(settingsMenu, helpMenu)
	a.window.SetMainMenu(mainMenu)

	// Setup system tray menu
	a.setupSystemTrayMenu()
}

func (a *App) setupSystemTrayMenu() {
	if desk, ok := a.app.(desktop.App); ok {
		// Determine icon based on month
		_, month, _ := time.Now().Date()
		var trayIcon fyne.Resource
		if month == time.December {
			trayIcon = resourceKrankyBearChristmasGrinchPng
		} else {
			trayIcon = resourceKrankyBearCowboyBrownPng
		}

		trayMenu := fyne.NewMenu(appName,
			fyne.NewMenuItem("Show", func() {
				a.window.Show()
			}),
			fyne.NewMenuItem("Hide", func() {
				a.window.Hide()
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Change Master Password", func() {
				a.showChangePasswordDialog()
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Help", func() {
				a.showHelpDialog()
			}),
			fyne.NewMenuItem("Check for Update", func() {
				a.checkForUpdate()
			}),
			fyne.NewMenuItem("About", func() {
				a.showAboutDialog()
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Quit", func() {
				desk.SetSystemTrayMenu(nil)
				if a.db != nil {
					a.db.Close()
				}
				a.app.Quit()
			}),
		)
		desk.SetSystemTrayMenu(trayMenu)
		desk.SetSystemTrayIcon(trayIcon)
	}
}

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	URL     string `json:"html_url"`
}

// compareVersions compares two version strings (e.g., "0.1.0", "0.1.1")
// Returns: -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func compareVersions(v1, v2 string) int {
	// Remove 'v' prefix if present
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	// Split versions into parts
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	// Get maximum length
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	// Compare each part
	for i := 0; i < maxLen; i++ {
		var part1, part2 int
		if i < len(parts1) {
			part1, _ = strconv.Atoi(parts1[i])
		}
		if i < len(parts2) {
			part2, _ = strconv.Atoi(parts2[i])
		}

		if part1 < part2 {
			return -1
		}
		if part1 > part2 {
			return 1
		}
	}

	return 0
}

func (a *App) checkForUpdate() {
	// Check if update window is already open
	updateWindowTitle := appName + ": Update Check"
	if existingWindow := a.showOrFocusDialog(updateWindowTitle); existingWindow != nil {
		a.updateWindow = existingWindow
		return
	}

	// Show checking dialog
	checkingDialog := dialog.NewInformation("Checking for Updates", "Checking for updates...", a.window)
	checkingDialog.Show()

	// Run update check in goroutine to avoid blocking UI
	go func() {
		// GitHub API URL for releases
		apiURL := "https://api.github.com/repos/amarillier/KrankyBearFileMover/releases/latest"

		client := &http.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Get(apiURL)
		if err != nil {
			fyne.Do(func() {
				checkingDialog.Hide()
				dialog.ShowError(fmt.Errorf("failed to check for updates: %v", err), a.window)
			})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fyne.Do(func() {
				checkingDialog.Hide()
				dialog.ShowError(fmt.Errorf("failed to check for updates: HTTP %d", resp.StatusCode), a.window)
			})
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fyne.Do(func() {
				checkingDialog.Hide()
				dialog.ShowError(fmt.Errorf("failed to read update information: %v", err), a.window)
			})
			return
		}

		var release GitHubRelease
		if err := json.Unmarshal(body, &release); err != nil {
			fyne.Do(func() {
				checkingDialog.Hide()
				dialog.ShowError(fmt.Errorf("failed to parse update information: %v", err), a.window)
			})
			return
		}

		// Compare versions (remove 'v' prefix if present)
		latestVersion := strings.TrimPrefix(release.TagName, "v")
		currentVersion := strings.TrimPrefix(appVersion, "v")

		// Compare versions to determine which is newer
		comparison := compareVersions(currentVersion, latestVersion)

		// Format message
		var message string
		var updateAvailable bool

		if comparison > 0 {
			// Current version is newer than released version
			message = fmt.Sprintf("You are running a newer version of %s.\n\nCurrent version: %s\nLatest released version: %s",
				appName, currentVersion, latestVersion)
			updateAvailable = false
		} else if comparison < 0 {
			// Current version is older than released version
			message = fmt.Sprintf("A newer version is available!\n\nCurrent version: %s\nLatest version: %s\n\n%s",
				currentVersion, latestVersion, release.Body)
			updateAvailable = true
		} else {
			// Versions are the same
			message = fmt.Sprintf("You are running the latest version.\n\nCurrent version: %s\nLatest version: %s",
				currentVersion, latestVersion)
			updateAvailable = false
		}

		// Hide checking dialog and show update alert window on main thread
		fyne.Do(func() {
			checkingDialog.Hide()
			a.showUpdateAlert(message, release.URL, updateAvailable)
		})
	}()
}

func (a *App) showUpdateAlert(updtmsg string, releaseURL string, updateAvailable bool) {
	// Parse release link
	releaselink, rerr := url.Parse(releaseURL)
	if rerr != nil {
		fyne.LogError("Could not parse URL", rerr)
		releaselink, _ = url.Parse("https://github.com/amarillier/KrankyBearFileMover/releases/latest")
	}
	myreleaselink := widget.NewHyperlink(releaseURL, releaselink)
	myreleaselink.Alignment = fyne.TextAlignLeading

	// Create image - use Beret if running newer version, Christmas Grinch in December, otherwise CowboyBrown
	var kbimg *canvas.Image
	_, month, _ := time.Now().Date()
	if strings.Contains(updtmsg, "running a newer version") {
		kbimg = canvas.NewImageFromResource(resourceKrankyBearBeretPng)
	} else if month == time.December {
		kbimg = canvas.NewImageFromResource(resourceKrankyBearChristmasGrinchPng)
	} else {
		kbimg = canvas.NewImageFromResource(resourceKrankyBearCowboyBrownPng)
	}
	kbimg.FillMode = canvas.ImageFillOriginal

	// Create content
	text := widget.NewLabel(updtmsg)
	text.Wrapping = fyne.TextWrapWord

	openBtn := widget.NewButton("Open Release Page", func() {
		// Open browser to release page
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/c", "start", releaseURL)
		} else if runtime.GOOS == "linux" {
			cmd = exec.Command("xdg-open", releaseURL)
		} else {
			cmd = exec.Command("open", releaseURL) // macOS
		}
		cmd.Run()
	})

	content := container.NewVBox(
		kbimg,
		text,
		myreleaselink,
		openBtn,
	)

	// Create or update window
	updateWindowTitle := appName + ": Update Check"
	if a.updateWindow == nil {
		a.updateWindow = a.app.NewWindow(updateWindowTitle)
		// Set icon - use Beret if running newer version, Christmas Grinch in December, otherwise CowboyBrown
		if strings.Contains(updtmsg, "running a newer version") {
			a.updateWindow.SetIcon(resourceKrankyBearBeretPng)
		} else if month == time.December {
			a.updateWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
		} else {
			a.updateWindow.SetIcon(resourceKrankyBearCowboyBrownPng)
		}
		a.updateWindow.Resize(fyne.NewSize(500, 300))
		a.registerDialog(a.updateWindow)
		a.centerDialogOnMainWindow(a.updateWindow)
	} else {
		// Update existing window icon
		if strings.Contains(updtmsg, "running a newer version") {
			a.updateWindow.SetIcon(resourceKrankyBearBeretPng)
		} else if month == time.December {
			a.updateWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
		} else {
			a.updateWindow.SetIcon(resourceKrankyBearCowboyBrownPng)
		}
	}

	a.updateWindow.SetContent(content)
	a.updateWindow.Show()
}

func (a *App) showChangePasswordDialog() {
	// First verify current password
	a.verifyPasswordForSettings(func() {
		currentPasswordEntry := widget.NewPasswordEntry()
		currentPasswordEntry.SetPlaceHolder("Current master password")
		newPasswordEntry := widget.NewPasswordEntry()
		newPasswordEntry.SetPlaceHolder("New master password")
		confirmPasswordEntry := widget.NewPasswordEntry()
		confirmPasswordEntry.SetPlaceHolder("Confirm new password")

		var changeDialog dialog.Dialog

		changePassword := func() {
			currentPassword := currentPasswordEntry.Text
			newPassword := newPasswordEntry.Text
			confirmPassword := confirmPasswordEntry.Text

			if currentPassword == "" || newPassword == "" || confirmPassword == "" {
				dialog.ShowError(fmt.Errorf("all fields are required"), a.window)
				return
			}

			if newPassword != confirmPassword {
				dialog.ShowError(fmt.Errorf("new passwords do not match"), a.window)
				return
			}

			// Verify current password
			tempDb, err := NewDatabase(currentPassword)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to verify current password: %w", err), a.window)
				return
			}
			if err := tempDb.VerifyPassword(); err != nil {
				tempDb.Close()
				dialog.ShowError(fmt.Errorf("current password is incorrect"), a.window)
				return
			}
			tempDb.Close()

			// Re-encrypt all connections with new password
			if err := a.reencryptDatabase(currentPassword, newPassword); err != nil {
				dialog.ShowError(fmt.Errorf("failed to change password: %w", err), a.window)
				return
			}

			// Update the current database connection
			a.db.Close()
			newDb, err := NewDatabase(newPassword)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to open database with new password: %w", err), a.window)
				return
			}
			a.db = newDb

			changeDialog.Hide()
			dialog.ShowInformation("Success", "Master password changed successfully", a.window)
		}

		content := container.NewVBox(
			widget.NewLabel("Change Master Password"),
			widget.NewLabel("Current Password:"),
			currentPasswordEntry,
			widget.NewLabel("New Password:"),
			newPasswordEntry,
			widget.NewLabel("Confirm New Password:"),
			confirmPasswordEntry,
			container.NewHBox(
				widget.NewButton("Change Password", changePassword),
				widget.NewButton("Cancel", func() {
					changeDialog.Hide()
				}),
			),
		)

		changeDialog = dialog.NewCustom("Change Master Password", "", content, a.window)
		changeDialog.Resize(fyne.NewSize(400, 300))
		changeDialog.Show()
	})
}

func (a *App) reencryptDatabase(oldPassword, newPassword string) error {
	// Close current database
	a.db.Close()

	// Create temporary database with old password to decrypt
	oldDb, err := NewDatabase(oldPassword)
	if err != nil {
		return fmt.Errorf("failed to open database with old password: %w", err)
	}
	defer oldDb.Close()

	// Get connections from old database (decrypted)
	oldConnections, err := oldDb.GetConnections()
	if err != nil {
		return fmt.Errorf("failed to get connections from old database: %w", err)
	}

	// Create new database with new password
	newDb, err := NewDatabase(newPassword)
	if err != nil {
		return fmt.Errorf("failed to create database with new password: %w", err)
	}
	defer newDb.Close()

	// Re-encrypt and save all connections with new password
	for _, conn := range oldConnections {
		if err := newDb.SaveConnection(conn); err != nil {
			return fmt.Errorf("failed to save connection %s: %w", conn.Name, err)
		}
	}

	// Update the verification token with the new password
	if err := newDb.UpdateVerificationToken(); err != nil {
		return fmt.Errorf("failed to update verification token: %w", err)
	}

	return nil
}

// centerDialogOnMainWindow positions a dialog window relative to the main window
func (a *App) centerDialogOnMainWindow(dialogWindow fyne.Window) {
	if a.window == nil {
		dialogWindow.CenterOnScreen()
		return
	}
	dialogWindow.Show()
	dialogWindow.CenterOnScreen()
}

// showOrFocusDialog checks if a dialog with the given title is already open.
// If it exists, brings it to front. Otherwise, returns nil.
func (a *App) showOrFocusDialog(title string) fyne.Window {
	if existingWindow, exists := a.openDialogs[title]; exists {
		if existingWindow != nil {
			if existingWindow.Content() != nil && existingWindow.Content().Visible() {
				existingWindow.Show()
				existingWindow.RequestFocus()
				return existingWindow
			} else {
				delete(a.openDialogs, title)
			}
		} else {
			delete(a.openDialogs, title)
		}
	}
	return nil
}

// registerDialog registers a dialog window by its title to prevent duplicates
func (a *App) registerDialog(window fyne.Window) {
	title := window.Title()
	a.openDialogs[title] = window
	a.registerChildWindow(window)
}

// registerChildWindow adds a window to the tracking list
func (a *App) registerChildWindow(window fyne.Window) {
	a.childWindows = append(a.childWindows, window)
	title := window.Title()

	window.SetCloseIntercept(func() {
		for i, w := range a.childWindows {
			if w == window {
				a.childWindows = append(a.childWindows[:i], a.childWindows[i+1:]...)
				break
			}
		}
		if _, exists := a.openDialogs[title]; exists {
			delete(a.openDialogs, title)
		}
		window.Close()
	})
}

func (a *App) showHelpDialog() {
	if existingWindow := a.showOrFocusDialog(appName + ": Help"); existingWindow != nil {
		a.helpWindow = existingWindow
		return
	}

	a.helpWindow = a.app.NewWindow(appName + ": Help")

	// Set icon based on month - Christmas Grinch in December, otherwise CowboyBrown
	_, month, _ := time.Now().Date()
	if month == time.December {
		a.helpWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
	} else {
		a.helpWindow.SetIcon(resourceKrankyBearCowboyBrownPng)
	}

	hlpText := `KrankyBear FileMover is a cross-platform dual-pane file transfer application.

FEATURES:

- Cross-platform support: Works on macOS, Linux, and Windows
- Dual-pane interface: Transfer files between two locations side-by-side
- Multiple connection types:
  • Local file system
  • SFTP/SCP (SSH File Transfer Protocol)
  • SMB (Windows file sharing)
- Secure credential storage: Encrypted database for connection credentials
- Connection management: Save, edit, and delete connection profiles
- Easy file transfer: Simple drag-and-drop or button-based transfers

USAGE:

- Select a connection from the left sidebar
- Click "Connect Left" or "Connect Right" to connect to a remote server
- Navigate directories in either pane
- Select a file and click "→ Transfer" or "← Transfer" to move files
- Use "Disconnect Left" or "Disconnect Right" to return to local file system

CONFIGURATION:

Connection profiles are stored securely in an encrypted database.
Each connection requires:
- Name: A friendly name for the connection
- Type: SFTP/SCP or SMB
- Host: Server address or IP
- Username: Login username
- Password: Login password (stored encrypted)
- Port: Connection port (defaults provided)
- Path: Initial directory path (optional)
`

	hlpText += "\n" + appName + " v " + appVersion
	hlpText += "\n" + appCopyright
	hlpText += "\n\n" + appAuthor + ", using Go and fyne GUI"

	helpLabel := widget.NewLabel(hlpText)
	helpLabel.Wrapping = fyne.TextWrapWord

	tabs := container.NewDocTabs(
		container.NewTabItem("Help", container.NewScroll(helpLabel)),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	a.helpWindow.Resize(fyne.NewSize(800, 500))
	a.helpWindow.SetContent(tabs)
	a.registerDialog(a.helpWindow)

	a.helpWindow.SetCloseIntercept(func() {
		windowToClose := a.helpWindow
		title := windowToClose.Title()

		for i, w := range a.childWindows {
			if w == windowToClose {
				a.childWindows = append(a.childWindows[:i], a.childWindows[i+1:]...)
				break
			}
		}
		if _, exists := a.openDialogs[title]; exists {
			delete(a.openDialogs, title)
		}
		a.helpWindow = nil
		windowToClose.Close()
	})

	a.centerDialogOnMainWindow(a.helpWindow)
	a.helpWindow.Show()
}

func (a *App) showAboutDialog() {
	if existingWindow := a.showOrFocusDialog("About"); existingWindow != nil {
		return
	}

	aboutText := appName + " v " + appVersion
	aboutText += "\n" + appCopyright
	aboutText += "\n\nCreated by " + appAuthor + ", using Go and fyne GUI"
	aboutText += "\n\nNo obligation, it's rewarding to hear if you use this app."
	aboutText += "\n\nAnd looking about about and help or settings too much might expose an easter egg!"

	// Set icon based on month - Christmas Grinch in December, otherwise CowboyBrown
	_, month, _ := time.Now().Date()
	var kbImage *canvas.Image
	if month == time.December {
		kbImage = canvas.NewImageFromResource(resourceKrankyBearChristmasGrinchPng)
	} else {
		kbImage = canvas.NewImageFromResource(resourceKrankyBearCowboyBrownPng)
	}
	kbImage.FillMode = canvas.ImageFillOriginal

	text := widget.NewLabel(aboutText)
	content := container.NewHBox(kbImage, text)

	dialogWindow := a.app.NewWindow(appName + ": About")
	if month == time.December {
		dialogWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
	} else {
		dialogWindow.SetIcon(resourceKrankyBearCowboyBrownPng)
	}
	dialogWindow.Resize(fyne.NewSize(500, 200))
	dialogWindow.SetContent(content)

	a.registerDialog(dialogWindow)
	a.centerDialogOnMainWindow(dialogWindow)
}

func (a *App) Run() {
	// Show window first
	a.window.Show()
	// Show password dialog after window is ready
	go func() {
		time.Sleep(50 * time.Millisecond) // Small delay to ensure window is initialized
		fyne.Do(func() {
			a.showPasswordDialog(a.window)
		})
	}()
	// Run the event loop (this blocks)
	a.window.ShowAndRun()
	if a.db != nil {
		a.db.Close()
	}
}

func main() {
	app, err := NewApp()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	app.Run()
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
