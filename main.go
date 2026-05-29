package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type App struct {
	app                  fyne.App
	window               fyne.Window
	db                   *Database
	leftBrowser          *FileBrowser
	rightBrowser         *FileBrowser
	leftConn             ConnectionManager
	rightConn            ConnectionManager
	leftConnInfo         *Connection // Store connection info for rsync
	rightConnInfo        *Connection // Store connection info for rsync
	connections          []*Connection
	selectedConnectionID int64 // 0 = none; SQLite IDs are always > 0
	connectionPickLabel  *widget.Label
	settingsAuthExpiry   time.Time // master-password grace for settings/connection editors
	leftSelectedFile     int
	rightSelectedFile    int
	helpWindow           fyne.Window
	updateWindow         fyne.Window
	openDialogs          map[string]fyne.Window
	childWindows         []fyne.Window
	toolbar              fyne.CanvasObject
	syncToolbar          fyne.CanvasObject // Separate toolbar for sync/transfer buttons
	systemTrayActive     bool
	leftPanelColor       color.Color // Custom color for left panel
	rightPanelColor      color.Color // Custom color for right panel
	fileAgentStopMu      sync.Mutex
	fileAgentStop        context.CancelFunc // non-nil while LAN file agent runs from Tools menu
	passwordWindow       fyne.Window        // master-password prompt, if visible
}

const (
	// appName    = "KrankyBear FileMover"
	appVersion               = "0.3.0" // keep in sync with FyneApp.toml; bump with ./setver.sh
	appAuthor                = "Allan Marillier"
	defaultConnectionTimeout = 10 * time.Second // Default connection timeout
	debugLogFileName         = "debug.log"

	dialogTitleConnectionSettings   = "Connection Settings"
	dialogTitlePickEditConnection   = "Edit connection"
	dialogTitlePickDeleteConnection = "Delete connection"
	dialogTitlePickCloneConnection  = "Duplicate from connection"
)

// DebugLogger handles debug logging to a file
type DebugLogger struct {
	logFile *os.File
	logger  *log.Logger
	enabled bool
	mu      sync.Mutex
}

var globalDebugLogger *DebugLogger

// initDebugLogger initializes the debug logger
func initDebugLogger() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	logDir := filepath.Join(homeDir, ".filemover")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logPath := filepath.Join(logDir, debugLogFileName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	globalDebugLogger = &DebugLogger{
		logFile: logFile,
		logger:  log.New(logFile, "", log.LstdFlags),
		enabled: false,
	}

	return nil
}

// SetEnabled enables or disables debug logging
func (dl *DebugLogger) SetEnabled(enabled bool) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	dl.enabled = enabled
	if enabled {
		dl.logger.Printf("=== Debug logging enabled ===\n")
	} else {
		dl.logger.Printf("=== Debug logging disabled ===\n")
	}
}

// IsEnabled returns whether debug logging is enabled
func (dl *DebugLogger) IsEnabled() bool {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	return dl.enabled
}

// Log writes a debug message if logging is enabled
func (dl *DebugLogger) Log(format string, args ...interface{}) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	if dl.enabled {
		dl.logger.Printf(format, args...)
	}
}

// GetLogFilePath returns the path to the log file
func (dl *DebugLogger) GetLogFilePath() string {
	return dl.logFile.Name()
}

// Close closes the log file
func (dl *DebugLogger) Close() error {
	if dl.logFile != nil {
		return dl.logFile.Close()
	}
	return nil
}

// debugLog is a convenience function to log debug messages
func debugLog(format string, args ...interface{}) {
	if globalDebugLogger != nil && globalDebugLogger.IsEnabled() {
		globalDebugLogger.Log(format, args...)
	}
}

var appName = "KrankyBear FileMover"
var appCopyright = "Copyright (c) Allan Marillier, 2025-" + strconv.Itoa(time.Now().Year())

func connectionDisplayLabel(c *Connection) string {
	if c == nil {
		return ""
	}
	name := strings.TrimSpace(c.Name)
	if c.Type == "fileagent" {
		p := c.Port
		if p == 0 {
			p = 9742
		}
		return fmt.Sprintf("%s (LAN agent %s:%d)", name, c.Host, p)
	}
	return fmt.Sprintf("%s (%s@%s)", name, c.Username, c.Host)
}

func (a *App) masterPasswordGraceDuration() time.Duration {
	minutes := a.app.Preferences().IntWithFallback("master_password_prompt_timeout_minutes", 5)
	if minutes <= 0 {
		return 0
	}
	return time.Duration(minutes) * time.Minute
}

func (a *App) selectedConnectionProfile() *Connection {
	if a.selectedConnectionID == 0 {
		return nil
	}
	for _, c := range a.connections {
		if c.ID == a.selectedConnectionID {
			return c
		}
	}
	return nil
}

func (a *App) connectionFromDisplayLabel(label string) *Connection {
	for _, c := range a.connections {
		if connectionDisplayLabel(c) == label {
			return c
		}
	}
	return nil
}

func (a *App) updateConnectionPickLabel() {
	if a.connectionPickLabel == nil {
		return
	}
	conn := a.selectedConnectionProfile()
	if conn == nil {
		a.connectionPickLabel.SetText("Selected: (none)")
	} else {
		a.connectionPickLabel.SetText("Selected: " + connectionDisplayLabel(conn))
	}
}

func NewApp() (*App, error) {
	myApp := app.NewWithID("com.github.amarillier.FileMover")

	// Initialize debug logger
	if err := initDebugLogger(); err != nil {
		// Log initialization error but don't fail app startup
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize debug logger: %v\n", err)
	}

	// Load saved debug mode preference
	if globalDebugLogger != nil {
		debugEnabled := myApp.Preferences().BoolWithFallback("debug_mode", false)
		globalDebugLogger.SetEnabled(debugEnabled)
	}

	// Load saved theme preference
	savedTheme := myApp.Preferences().StringWithFallback("theme", "default")
	if savedTheme == "light" {
		myApp.Settings().SetTheme(&appTheme{Theme: theme.LightTheme()})
	} else if savedTheme == "dark" {
		myApp.Settings().SetTheme(&appTheme{Theme: theme.DarkTheme()})
	} else {
		myApp.Settings().SetTheme(&appTheme{Theme: theme.DefaultTheme()})
	}

	window := myApp.NewWindow("FileMover - Dual Pane File Transfer")
	window.Resize(fyne.NewSize(1200, 800))

	app := &App{
		app:               myApp,
		window:            window,
		leftSelectedFile:  -1,
		rightSelectedFile: -1,
		openDialogs:       make(map[string]fyne.Window),
		childWindows:      []fyne.Window{},
	}

	// Set close intercept - always fully close the app when X is clicked
	window.SetCloseIntercept(func() {
		// Clean up system tray if active (but only if it was actually set up)
		// Don't call SetSystemTrayMenu(nil) as it can cause crashes if GLFW isn't initialized
		if app.systemTrayActive {
			app.systemTrayActive = false
		}
		// Close database
		if app.db != nil {
			app.db.Close()
		}
		// Close debug logger
		if globalDebugLogger != nil {
			globalDebugLogger.Close()
		}
		// Quit the app
		myApp.Quit()
	})

	return app, nil
}

func (a *App) showPasswordDialog(parent fyne.Window) {
	// Attempt a silent unlock from the OS keychain first (opt-in). If the cached
	// password is missing/stale, fall through to prompting as normal.
	if db := a.tryKeychainUnlock(); db != nil {
		a.completeLogin(db, nil)
		return
	}

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Enter master password")
	passwordEntry.Resize(fyne.NewSize(300, passwordEntry.MinSize().Height))

	// Hint label, revealed after repeated failed attempts if a hint was set.
	hintLabel := widget.NewLabel("")
	hintLabel.Wrapping = fyne.TextWrapWord
	hintLabel.Hide()
	failedAttempts := 0

	// Track the current error dialog so each new attempt replaces the previous one
	// (no stacking) and a successful login dismisses any lingering error.
	var errDialog dialog.Dialog
	dismissErrDialog := func() {
		if errDialog != nil {
			errDialog.Hide()
			errDialog = nil
		}
	}
	showErr := func(err error) {
		dismissErrDialog()
		errDialog = dialog.NewError(err, parent)
		errDialog.Show()
	}

	// Create password dialog window first so we can reference it in callbacks
	passwordDialogWindow := a.app.NewWindow("Enter Master Password")
	passwordDialogWindow.Resize(fyne.NewSize(450, 220))

	submitPassword := func() {
		masterPassword := passwordEntry.Text
		if masterPassword == "" {
			showErr(fmt.Errorf("master password cannot be empty"))
			return
		}

		db, err := NewDatabase(masterPassword)
		if err != nil {
			showErr(fmt.Errorf("failed to initialize database: %w", err))
			return
		}

		// Verify the password is correct
		if err := db.VerifyPassword(); err != nil {
			db.Close()
			failedAttempts++
			// After 2 failed attempts, surface the password hint if one was set.
			if failedAttempts >= 2 {
				if hint := ReadPasswordHint(); hint != "" {
					hintLabel.SetText("Hint: " + hint)
					hintLabel.Show()
				}
			}
			showErr(fmt.Errorf("invalid master password"))
			return
		}

		// Login succeeded: dismiss any lingering "invalid password" error.
		dismissErrDialog()

		// Refresh the keychain copy if the user has opted in to remembering it.
		if a.app.Preferences().BoolWithFallback("remember_master_password", false) {
			keychainSetMasterPassword(masterPassword)
		}

		a.completeLogin(db, passwordDialogWindow)
	}

	// Allow Enter key to submit
	passwordEntry.OnSubmitted = func(_ string) {
		submitPassword()
	}

	// Create custom content with buttons
	content := container.NewVBox(
		widget.NewLabel("Master Password:"),
		passwordEntry,
		hintLabel,
		container.NewHBox(
			widget.NewButton("Submit", submitPassword),
			widget.NewButton("Forgot Password", func() {
				a.showForgotPasswordDialog(parent, passwordDialogWindow)
			}),
			widget.NewButton("Cancel", func() {
				a.passwordWindow = nil
				passwordDialogWindow.Close()
				os.Exit(0)
			}),
		),
		container.NewHBox(
			widget.NewButton("Help", func() {
				a.showHelpDialogBeforePassword(parent)
			}),
			widget.NewButton("About", func() {
				a.showAboutDialogBeforePassword(parent)
			}),
		),
	)

	passwordDialogWindow.SetContent(container.NewPadded(content))

	a.passwordWindow = passwordDialogWindow
	// Set close intercept to exit app when X is clicked
	passwordDialogWindow.SetCloseIntercept(func() {
		a.passwordWindow = nil
		os.Exit(0)
	})

	// Center on the same display as the main window
	a.centerDialogOnMainWindow(passwordDialogWindow)

	// Focus the password entry after a small delay to ensure window is shown
	go func() {
		time.Sleep(100 * time.Millisecond)
		fyne.Do(func() {
			passwordDialogWindow.Canvas().Focus(passwordEntry)
		})
	}()
}

// tryKeychainUnlock attempts to open the database using a master password cached
// in the OS keychain. It returns nil (so the caller prompts) when the feature is
// disabled, no entry exists, or the cached password no longer works (in which
// case the stale entry is removed).
func (a *App) tryKeychainUnlock() *Database {
	if !a.app.Preferences().BoolWithFallback("remember_master_password", false) {
		return nil
	}
	pw, ok := keychainGetMasterPassword()
	if !ok || pw == "" {
		return nil
	}

	db, err := NewDatabase(pw)
	if err != nil {
		return nil
	}
	if err := db.VerifyPassword(); err != nil {
		// Cached password is stale (e.g. changed elsewhere). Discard it.
		db.Close()
		keychainDeleteMasterPassword()
		return nil
	}
	return db
}

// completeLogin installs the unlocked database and brings up the main UI. The
// passwordDialogWindow may be nil when the unlock came from the keychain.
func (a *App) completeLogin(db *Database, passwordDialogWindow fyne.Window) {
	a.db = db
	fyne.Do(func() {
		a.passwordWindow = nil
		if passwordDialogWindow != nil {
			passwordDialogWindow.Close()
		}
		a.setupUI()
		// Setup system tray menu after UI is fully initialized.
		// Use a goroutine with delay to ensure window is fully ready.
		go func() {
			time.Sleep(200 * time.Millisecond) // Delay to ensure window is fully shown and ready
			fyne.Do(func() {
				a.setupSystemTrayMenu()
			})
		}()
	})
}

func (a *App) showForgotPasswordDialog(parent fyne.Window, passwordDialogWindow fyne.Window) {
	warningText := "WARNING: This will permanently delete your encrypted database and all saved connections.\n\n"
	warningText += "This action cannot be undone. All your saved connection credentials will be lost.\n\n"
	warningText += "Do you want to continue?"

	label := widget.NewLabel(warningText)
	label.Wrapping = fyne.TextWrapWord

	// Create forgot password dialog window
	forgotDialogWindow := a.app.NewWindow("Forgot Password - Reset Database")
	forgotDialogWindow.Resize(fyne.NewSize(500, 200))

	content := container.NewVBox(
		label,
		container.NewHBox(
			widget.NewButton("Yes, Delete Database", func() {
				// Get database path
				homeDir, err := os.UserHomeDir()
				if err != nil {
					fyne.Do(func() {
						forgotDialogWindow.Close()
						dialog.ShowError(fmt.Errorf("failed to get home directory: %w", err), parent)
					})
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
					fyne.Do(func() {
						forgotDialogWindow.Close()
						dialog.ShowError(fmt.Errorf("failed to delete database: %w", err), parent)
					})
					return
				}

				// Drop any cached master password for the deleted database.
				keychainDeleteMasterPassword()
				a.app.Preferences().SetBool("remember_master_password", false)

				// Close the forgot password dialog
				fyne.Do(func() {
					forgotDialogWindow.Close()
					// Close the password dialog window
					passwordDialogWindow.Close()

					// Show password dialog again to create new database
					a.showPasswordDialog(parent)
				})
			}),
			widget.NewButton("Cancel", func() {
				forgotDialogWindow.Close()
			}),
		),
	)

	forgotDialogWindow.SetContent(container.NewPadded(content))
	forgotDialogWindow.SetOnClosed(func() {
		a.untrackDialogWindow(forgotDialogWindow.Title(), forgotDialogWindow)
	})
	a.registerDialog(forgotDialogWindow)
	a.centerDialogOnMainWindow(forgotDialogWindow)
}

func (a *App) showRemoveAllSettingsDialog() {
	warningText := "WARNING: This will permanently delete your encrypted database and all saved connections.\n\n"
	warningText += "This action cannot be undone. All your saved connection credentials will be lost.\n\n"
	warningText += "You will need to restart the application and enter a new master password to continue.\n\n"
	warningText += "Do you want to continue?"

	label := widget.NewLabel(warningText)
	label.Wrapping = fyne.TextWrapWord

	// Create dialog window first so we can reference it in callbacks
	removeDialogWindow := a.app.NewWindow("Remove ALL Settings")
	removeDialogWindow.Resize(fyne.NewSize(500, 250))

	content := container.NewVBox(
		label,
		container.NewHBox(
			widget.NewButton("Yes, Delete All Settings", func() {
				// Get database path
				homeDir, err := os.UserHomeDir()
				if err != nil {
					fyne.Do(func() {
						removeDialogWindow.Close()
						dialog.ShowError(fmt.Errorf("failed to get home directory: %w", err), a.window)
					})
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
					fyne.Do(func() {
						removeDialogWindow.Close()
						dialog.ShowError(fmt.Errorf("failed to delete database: %w", err), a.window)
					})
					return
				}

				// Drop any cached master password for the deleted database.
				keychainDeleteMasterPassword()
				a.app.Preferences().SetBool("remember_master_password", false)

				// Clear connections list and reload UI
				fyne.Do(func() {
					removeDialogWindow.Close()
					a.connections = []*Connection{}
					a.selectedConnectionID = 0
					a.leftConn = NewLocalConnection()
					a.rightConn = NewLocalConnection()
					a.updateLayout()

					// Show success message
					dialog.ShowInformation("Settings Removed", "All settings have been removed. Please restart the application and enter a new master password.", a.window)
				})
			}),
			widget.NewButton("Cancel", func() {
				removeDialogWindow.Close()
			}),
		),
	)

	removeDialogWindow.SetContent(container.NewPadded(content))
	removeDialogWindow.SetOnClosed(func() {
		a.untrackDialogWindow(removeDialogWindow.Title(), removeDialogWindow)
	})
	a.registerDialog(removeDialogWindow)
	a.centerDialogOnMainWindow(removeDialogWindow)
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

	a.connectionPickLabel = widget.NewLabel("Selected: (none)")
	a.connectionPickLabel.Wrapping = fyne.TextWrapOff

	var connectionsBtn *widget.Button
	connectionsBtn = widget.NewButton("Connections", func() {
		items := []*fyne.MenuItem{
			fyne.NewMenuItem("Refresh list", func() { a.loadConnections() }),
		}
		if len(a.connections) > 0 {
			items = append(items, fyne.NewMenuItemSeparator())
			for _, c := range a.connections {
				c := c
				items = append(items, fyne.NewMenuItem(connectionDisplayLabel(c), func() {
					a.selectedConnectionID = c.ID
					a.updateConnectionPickLabel()
				}))
			}
		} else {
			items = append(items, fyne.NewMenuItem("(No saved connections)", func() {}))
		}
		menu := fyne.NewMenu("", items...)
		pop := widget.NewPopUpMenu(menu, a.window.Canvas())
		pop.ShowAtRelativePosition(fyne.NewPos(0, connectionsBtn.Size().Height), connectionsBtn)
	})

	// Connection toolbar (top row)
	toolbar := container.NewHBox(
		connectionsBtn,
		a.connectionPickLabel,
		widget.NewSeparator(),
		widget.NewButton("New Connection", func() {
			if a.showOrFocusDialog(dialogTitleConnectionSettings) != nil {
				return
			}
			if a.showOrFocusDialog(dialogTitlePickCloneConnection) != nil {
				return
			}
			a.showConnectionDialog(nil)
		}),
		widget.NewButton("Edit Connection", func() {
			if a.showOrFocusDialog(dialogTitleConnectionSettings) != nil {
				return
			}
			if a.showOrFocusDialog(dialogTitlePickEditConnection) != nil {
				return
			}
			a.verifyPasswordForSettings(func() {
				a.showConnectionPicker(dialogTitlePickEditConnection, func(conn *Connection) {
					a.showConnectionEditor(conn)
				})
			})
		}),
		widget.NewButton("Delete Connection", func() {
			if a.showOrFocusDialog(dialogTitlePickDeleteConnection) != nil {
				return
			}
			a.verifyPasswordForSettings(func() {
				a.showConnectionPicker(dialogTitlePickDeleteConnection, func(conn *Connection) {
					dialog.ShowConfirm("Delete Connection",
						fmt.Sprintf("Delete connection '%s'?", conn.Name),
						func(ok bool) {
							if ok {
								if err := a.db.DeleteConnection(conn.ID); err != nil {
									dialog.ShowError(err, a.window)
								} else {
									if a.selectedConnectionID == conn.ID {
										a.selectedConnectionID = 0
									}
									a.loadConnections()
								}
							}
						}, a.window)
				})
			})
		}),
		widget.NewSeparator(),
		widget.NewButton("Connect Left", func() {
			conn := a.selectedConnectionProfile()
			if conn == nil {
				dialog.ShowInformation("No connection selected", "Use Connections to choose a profile, then connect.", a.window)
				return
			}
			a.connectTo(conn, false)
		}),
		widget.NewButton("Connect Right", func() {
			conn := a.selectedConnectionProfile()
			if conn == nil {
				dialog.ShowInformation("No connection selected", "Use Connections to choose a profile, then connect.", a.window)
				return
			}
			a.connectTo(conn, true)
		}),
		widget.NewButton("Disconnect Left", func() {
			if a.leftConn != nil {
				a.leftConn.Disconnect()
				a.leftConn = NewLocalConnection()
				a.leftConnInfo = nil
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
				a.rightConnInfo = nil
				a.rightBrowser = NewFileBrowser(a.window, a.rightConn, nil, func(id int) {
					a.rightSelectedFile = id
				})
				a.updateLayout()
			}
		}),
	)

	// Sync and Transfer toolbar (bottom row) - created dynamically
	a.createSyncTransferToolbar()

	// Get browser containers
	leftContainer := a.leftBrowser.GetContainer()
	rightContainer := a.rightBrowser.GetContainer()

	// Apply panel colors if set
	if a.leftPanelColor != nil {
		rect := canvas.NewRectangle(a.leftPanelColor)
		rect.SetMinSize(leftContainer.MinSize())
		leftContainer = container.NewStack(rect, leftContainer)
	}
	if a.rightPanelColor != nil {
		rect := canvas.NewRectangle(a.rightPanelColor)
		rect.SetMinSize(rightContainer.MinSize())
		rightContainer = container.NewStack(rect, rightContainer)
	}

	// Main content: dual panes
	mainContent := container.NewHSplit(
		leftContainer,
		rightContainer,
	)
	mainContent.SetOffset(0.5)

	// Store toolbar for later updates
	a.toolbar = toolbar

	// Overall layout with two toolbars stacked vertically
	toolbarContainer := container.NewVBox(
		toolbar,
		a.syncToolbar,
	)

	content := container.NewBorder(
		toolbarContainer,
		nil,
		nil,
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
	if a.selectedConnectionProfile() == nil {
		a.selectedConnectionID = 0
	}
	a.updateConnectionPickLabel()
}

func (a *App) untrackDialogWindow(title string, win fyne.Window) {
	for i, w := range a.childWindows {
		if w == win {
			a.childWindows = append(a.childWindows[:i], a.childWindows[i+1:]...)
			break
		}
	}
	delete(a.openDialogs, title)
}

func (a *App) showConnectionEditor(conn *Connection) {
	if a.showOrFocusDialog(dialogTitleConnectionSettings) != nil {
		return
	}
	var duplicatePick func(onChosen func(*Connection))
	if conn == nil || conn.ID == 0 {
		duplicatePick = func(onChosen func(*Connection)) {
			if len(a.connections) == 0 {
				dialog.ShowInformation("No saved connections", "Save at least one connection before using Duplicate.", a.window)
				return
			}
			a.showConnectionPicker(dialogTitlePickCloneConnection, onChosen)
		}
	}
	connDialog := NewConnectionDialog(a.window, a.app, conn, func(savedConn *Connection) {
		if err := a.db.SaveConnection(savedConn); err != nil {
			dialog.ShowError(err, a.window)
		} else {
			a.loadConnections()
			a.selectedConnectionID = savedConn.ID
			a.updateConnectionPickLabel()
		}
	}, duplicatePick)
	win := connDialog.DialogWindow()
	a.registerDialog(win)
	// Programmatic Close() (Save/Cancel) does not run SetCloseIntercept; cleanup must be in SetOnClosed.
	win.SetOnClosed(func() {
		a.untrackDialogWindow(dialogTitleConnectionSettings, win)
	})
	win.SetCloseIntercept(func() {
		win.Close()
	})
	connDialog.Show()
}

func (a *App) showConnectionDialog(conn *Connection) {
	a.verifyPasswordForSettings(func() {
		a.showConnectionEditor(conn)
	})
}

func (a *App) showConnectionPicker(title string, onChosen func(*Connection)) {
	if len(a.connections) == 0 {
		dialog.ShowInformation("No connections", "Add a connection first.", a.window)
		return
	}
	if a.showOrFocusDialog(title) != nil {
		return
	}
	opts := make([]string, len(a.connections))
	for i, c := range a.connections {
		opts[i] = connectionDisplayLabel(c)
	}
	sel := widget.NewSelect(opts, nil)
	sel.SetSelected(opts[0])

	pickWin := a.app.NewWindow(title)
	pickWin.Resize(fyne.NewSize(520, 160))

	okBtn := widget.NewButton("OK", func() {
		c := a.connectionFromDisplayLabel(sel.Selected)
		if c == nil {
			dialog.ShowError(fmt.Errorf("could not resolve the selected connection"), pickWin)
			return
		}
		pickWin.Close()
		onChosen(c)
	})
	cancelBtn := widget.NewButton("Cancel", func() {
		pickWin.Close()
	})

	content := container.NewVBox(
		widget.NewLabel("Choose a saved connection:"),
		sel,
		container.NewHBox(okBtn, cancelBtn),
	)
	pickWin.SetContent(container.NewPadded(content))
	a.registerDialog(pickWin)
	pickWin.SetOnClosed(func() {
		a.untrackDialogWindow(title, pickWin)
	})
	pickWin.SetCloseIntercept(func() {
		pickWin.Close()
	})
	a.centerDialogOnMainWindow(pickWin)
}

func (a *App) verifyPasswordForSettings(onSuccess func()) {
	if d := a.masterPasswordGraceDuration(); d > 0 && time.Now().Before(a.settingsAuthExpiry) {
		onSuccess()
		return
	}

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Enter master password")
	passwordEntry.Resize(fyne.NewSize(300, passwordEntry.MinSize().Height))

	var verifyDialogWindow fyne.Window
	var cancelButton *widget.Button
	var passwordWrapper *passwordEntryWithEsc

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
			// Show error dialog and focus it
			errorDialog := dialog.NewError(fmt.Errorf("invalid master password"), a.window)
			errorDialog.Show()
			// Focus the error dialog window
			go func() {
				time.Sleep(100 * time.Millisecond)
				fyne.Do(func() {
					// Try to focus the error dialog - it's a modal so it should already be focused
					// But we can also refocus the password wrapper
					if verifyDialogWindow != nil && verifyDialogWindow.Canvas() != nil && passwordWrapper != nil {
						verifyDialogWindow.Canvas().Focus(passwordWrapper)
					}
					passwordEntry.SetText("") // Clear the password field
				})
			}()
			return
		}

		db.Close()
		if d := a.masterPasswordGraceDuration(); d > 0 {
			a.settingsAuthExpiry = time.Now().Add(d)
		} else {
			a.settingsAuthExpiry = time.Time{}
		}
		fyne.Do(func() {
			verifyDialogWindow.Close()
			onSuccess()
		})
	}

	cancelButton = widget.NewButton("Cancel", func() {
		verifyDialogWindow.Close()
	})

	// Allow Enter key to submit
	passwordEntry.OnSubmitted = func(_ string) {
		verifyPassword()
	}

	// Wrap password entry to handle Esc key
	passwordWrapper = &passwordEntryWithEsc{
		entry: passwordEntry,
		onEsc: func() { cancelButton.OnTapped() },
	}
	passwordWrapper.ExtendBaseWidget(passwordWrapper)

	// Create custom content with buttons
	innerContent := container.NewVBox(
		widget.NewLabel("Enter Master Password to Access Settings:"),
		passwordWrapper,
		container.NewHBox(
			widget.NewButton("OK", verifyPassword),
			cancelButton,
		),
	)

	// Create a focusable wrapper widget that handles Esc key
	escWrapper := &escKeyWrapper{
		content: container.NewPadded(innerContent),
		onEsc:   func() { cancelButton.OnTapped() },
	}
	escWrapper.ExtendBaseWidget(escWrapper)

	// Create custom window instead of dialog to avoid default close button
	verifyDialogWindow = a.app.NewWindow("Verify Master Password")
	verifyDialogWindow.Resize(fyne.NewSize(400, 180))
	verifyDialogWindow.SetContent(escWrapper)
	verifyDialogWindow.SetCloseIntercept(func() {
		verifyDialogWindow.Close()
	})

	// Focus the wrapper so it can receive Esc key events
	go func() {
		time.Sleep(100 * time.Millisecond)
		fyne.Do(func() {
			if verifyDialogWindow != nil && verifyDialogWindow.Canvas() != nil {
				verifyDialogWindow.Canvas().Focus(escWrapper)
				// Then focus the password wrapper so user can type immediately
				time.Sleep(50 * time.Millisecond)
				fyne.Do(func() {
					verifyDialogWindow.Canvas().Focus(passwordWrapper)
				})
			}
		})
	}()

	a.centerDialogOnMainWindow(verifyDialogWindow)
}

// checkMountedShare checks if an SMB share is already mounted on the system
// Returns the mount path if found, empty string otherwise
func checkMountedShare(conn *Connection) string {
	// On macOS, SMB shares are typically mounted at /Volumes/ShareName
	// On Linux, they might be at /mnt or /media
	volumes := []string{"/Volumes", "/mnt", "/media"}

	shareName := conn.RemotePath
	if shareName == "" {
		// Try to derive share name from host
		shareName = conn.Host
	}

	for _, volumeBase := range volumes {
		mountPath := filepath.Join(volumeBase, shareName)
		if info, err := os.Stat(mountPath); err == nil && info.IsDir() {
			// Check if it's actually a mount point (not just a regular directory)
			// On macOS, we can check if it's in /Volumes
			if volumeBase == "/Volumes" {
				return mountPath
			}
			// For Linux, check if it's a mount point
			// This is a simple check - a more robust solution would check /proc/mounts
			return mountPath
		}
	}

	return ""
}

func (a *App) connectTo(conn *Connection, rightPane bool) {
	var newConn ConnectionManager

	switch conn.Type {
	case "sftp", "scp":
		newConn = NewSFTPConnection(conn)
	case "smb":
		// Check if the share is already mounted (macOS/Linux)
		mountPath := checkMountedShare(conn)
		if mountPath != "" {
			debugLog("[CONNECTION] SMB share already mounted at: %s, using local filesystem\n", mountPath)
			// Use LocalConnection with the mounted path
			localConn := NewLocalConnection()
			localConn.currentPath = mountPath
			newConn = localConn
		} else {
			newConn = NewSMBConnection(conn)
		}
	case "rsync":
		if conn.Host == "" && strings.TrimSpace(conn.RemotePath) == "" {
			newConn = NewLocalConnection()
			break
		}
		var module string
		if conn.Host != "" {
			username := conn.Username
			if username == "" {
				username = os.Getenv("USER")
				if username == "" {
					username = "root"
				}
			}
			rpath := conn.RemotePath
			if rpath == "" {
				rpath = "/"
			}
			module = fmt.Sprintf("%s@%s:%s", username, conn.Host, rpath)
		} else {
			module = conn.RemotePath
		}
		newConn = NewRsyncRemoteConnection(conn, module)
	case "fileagent":
		newConn = NewFileAgentConnection(conn)
	default:
		dialog.ShowError(fmt.Errorf("unsupported connection type: %s", conn.Type), a.window)
		return
	}

	progress := dialog.NewProgressInfinite("Connecting", "Connecting to "+conn.Name, a.window)
	progress.Show()

	// Get timeout from preferences (default 10 seconds)
	timeoutSeconds := a.app.Preferences().IntWithFallback("connection_timeout_seconds", 10)
	timeout := time.Duration(timeoutSeconds) * time.Second

	go func() {
		defer func() {
			fyne.Do(func() {
				progress.Hide()
			})
		}()

		// Create a channel for connection result
		connResult := make(chan error, 1)

		// Start connection in a goroutine
		go func() {
			var err error

			debugLog("[CONNECTION] Calling Connect() for type: %s\n", conn.Type)
			// For rsync, validate the path/connection
			if conn.Type == "rsync" {
				err = a.validateRsyncConnection(conn)
			} else {
				err = newConn.Connect()
			}

			if err != nil {
				debugLog("[CONNECTION] Connection failed: %v\n", err)
			} else {
				debugLog("[CONNECTION] Connection successful\n")
			}

			connResult <- err
		}()

		// Wait for connection or timeout
		select {
		case err := <-connResult:
			if err != nil {
				fyne.Do(func() {
					a.showConnectionErrorDialog(conn, err, rightPane)
				})
				return
			}
			// Connection successful - continue with UI update
		case <-time.After(timeout):
			debugLog("[CONNECTION] Connection timeout after %v\n", timeout)
			fyne.Do(func() {
				a.showConnectionTimeoutDialog(conn, timeout, rightPane)
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
				a.rightConnInfo = conn // Store connection info for rsync
				// Create new browser - Refresh() is called automatically in NewFileBrowser
				a.rightBrowser = NewFileBrowser(a.window, a.rightConn, nil, func(id int) {
					a.rightSelectedFile = id
				})
			} else {
				if a.leftConn != nil {
					a.leftConn.Disconnect()
				}
				a.leftConn = newConn
				a.leftConnInfo = conn // Store connection info for rsync
				// Create new browser - Refresh() is called automatically in NewFileBrowser
				a.leftBrowser = NewFileBrowser(a.window, a.leftConn, nil, func(id int) {
					a.leftSelectedFile = id
				})
			}
			// Update layout to show the new browser
			a.updateLayout()
		})
	}()
}

func (a *App) updateLayout() {
	// Get browser containers
	leftContainer := a.leftBrowser.GetContainer()
	rightContainer := a.rightBrowser.GetContainer()

	// Apply panel colors if set
	if a.leftPanelColor != nil {
		rect := canvas.NewRectangle(a.leftPanelColor)
		rect.SetMinSize(leftContainer.MinSize())
		leftContainer = container.NewStack(rect, leftContainer)
	}
	if a.rightPanelColor != nil {
		rect := canvas.NewRectangle(a.rightPanelColor)
		rect.SetMinSize(rightContainer.MinSize())
		rightContainer = container.NewStack(rect, rightContainer)
	}

	// Update the main content area with current browsers
	mainContent := container.NewHSplit(
		leftContainer,
		rightContainer,
	)
	mainContent.SetOffset(0.5)

	// Update sync/transfer toolbar based on connection types
	a.createSyncTransferToolbar()

	// Update content with new browsers, preserving toolbar
	toolbarContainer := container.NewVBox(
		a.toolbar,
		a.syncToolbar,
	)

	content := container.NewBorder(
		toolbarContainer,
		nil,
		nil,
		nil,
		mainContent,
	)

	a.window.SetContent(content)
	// Force refresh to ensure the new content is displayed
	a.window.Content().Refresh()
}

// showConnectionTimeoutDialog shows a focused error dialog for connection timeout
func (a *App) showConnectionTimeoutDialog(conn *Connection, timeout time.Duration, rightPane bool) {
	errorMsg := fmt.Sprintf("Connection to '%s' (%s@%s:%d) timed out after %v.\n\nThe connection attempt took too long to complete.",
		conn.Name, conn.Username, conn.Host, conn.Port, timeout)

	errorWindow := a.app.NewWindow("Connection Timeout")
	errorWindow.Resize(fyne.NewSize(500, 200))

	okButton := widget.NewButton("OK", func() {
		errorWindow.Close()
		// Reset the panel to local filesystem
		a.resetPanelToLocal(rightPane)
	})

	content := container.NewVBox(
		widget.NewLabel("Connection Timeout"),
		widget.NewLabel(errorMsg),
		container.NewHBox(okButton),
	)

	errorWindow.SetContent(container.NewPadded(content))
	errorWindow.SetCloseIntercept(func() {
		errorWindow.Close()
		// Reset the panel to local filesystem
		a.resetPanelToLocal(rightPane)
	})

	// Focus the error window
	go func() {
		time.Sleep(100 * time.Millisecond)
		fyne.Do(func() {
			errorWindow.Show()
			errorWindow.CenterOnScreen()
			errorWindow.RequestFocus()
			errorWindow.Canvas().Focus(okButton)
		})
	}()
}

// showConnectionErrorDialog shows a focused error dialog for connection errors
func (a *App) showConnectionErrorDialog(conn *Connection, err error, rightPane bool) {
	errorMsg := fmt.Sprintf("Failed to connect to '%s' (%s@%s:%d).\n\nError: %v\n\nConnection details:\n- Host: %s\n- Port: %d\n- Username: %s\n- Type: %s",
		conn.Name, conn.Username, conn.Host, conn.Port, err, conn.Host, conn.Port, conn.Username, conn.Type)

	errorWindow := a.app.NewWindow("Connection Failed")
	errorWindow.Resize(fyne.NewSize(500, 250))

	okButton := widget.NewButton("OK", func() {
		errorWindow.Close()
		// Reset the panel to local filesystem
		a.resetPanelToLocal(rightPane)
	})

	content := container.NewVBox(
		widget.NewLabel("Connection Failed"),
		widget.NewLabel(errorMsg),
		container.NewHBox(okButton),
	)

	errorWindow.SetContent(container.NewPadded(content))
	errorWindow.SetCloseIntercept(func() {
		errorWindow.Close()
		// Reset the panel to local filesystem
		a.resetPanelToLocal(rightPane)
	})

	// Focus the error window
	go func() {
		time.Sleep(100 * time.Millisecond)
		fyne.Do(func() {
			errorWindow.Show()
			errorWindow.CenterOnScreen()
			errorWindow.RequestFocus()
			errorWindow.Canvas().Focus(okButton)
		})
	}()
}

// validateRsyncConnection validates an rsync connection by checking if the path exists (local) or is reachable (remote)
func (a *App) validateRsyncConnection(conn *Connection) error {
	// Check if rsync is available
	rsyncPath, err := exec.LookPath("rsync")
	if err != nil {
		return fmt.Errorf("rsync not found in PATH: %w", err)
	}

	// Determine if this is a remote or local connection
	var remotePath string
	var isRemote bool

	// If Host is set, construct remote path from Host, Username, and RemotePath
	if conn.Host != "" {
		isRemote = true
		username := conn.Username
		if username == "" {
			username = os.Getenv("USER")
			if username == "" {
				username = "root"
			}
		}
		path := conn.RemotePath
		if path == "" {
			path = "/"
		}
		remotePath = fmt.Sprintf("%s@%s:%s", username, conn.Host, path)
	} else {
		// Check if RemotePath contains remote format (user@host:/path)
		path := conn.RemotePath
		if path == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			path = homeDir
		}

		if strings.Contains(path, "@") && strings.Contains(path, ":") {
			isRemote = true
			remotePath = path
		} else {
			isRemote = false
			remotePath = path
		}
	}

	if isRemote {
		// Remote path - test connection by trying to list the remote directory
		// Format: user@host:/path
		parts := strings.SplitN(remotePath, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid remote path format: %s (expected user@host:/path)", remotePath)
		}

		remoteHost := parts[0]
		pathPart := parts[1]

		// Extract user and host
		userHost := strings.SplitN(remoteHost, "@", 2)
		if len(userHost) != 2 {
			return fmt.Errorf("invalid remote host format: %s (expected user@host)", remoteHost)
		}

		// Test connection with a quick rsync dry-run
		args := []string{"--list-only"}
		if p := strings.TrimSpace(conn.RemoteRsyncPath); p != "" {
			args = append([]string{"--rsync-path=" + p}, args...)
		}
		if rsyncNeedsExplicitSSH(conn) {
			args = append([]string{"-e", buildRsyncSSHShell(conn)}, args...)
		}
		args = append(args, fmt.Sprintf("%s@%s:%s", userHost[0], userHost[1], pathPart))

		// Set a shorter timeout for validation (5 seconds)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, rsyncPath, args...)
		var stderr bytes.Buffer
		cmd.Stdout = io.Discard
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			detail := strings.TrimSpace(stderr.String())
			var exitErr *exec.ExitError
			code := -1
			if errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			}
			msg := fmt.Errorf("failed to connect to remote rsync path %s: %w", remotePath, err)
			if detail != "" {
				msg = fmt.Errorf("%w\n\nrsync/ssh output:\n%s", msg, detail)
			}
			if code == 12 {
				msg = fmt.Errorf("%w\n\nIf the remote is Windows: use Remote Path like C:/Users/yourname. If rsync is installed but not on the SSH PATH (e.g. Cygwin), set \"Remote rsync program\" in Connection Settings to the full path to rsync.exe.", msg)
			}
			return msg
		}
	} else {
		// Local path - verify it exists
		expandedPath := remotePath
		if strings.HasPrefix(remotePath, "~") {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			expandedPath = filepath.Join(homeDir, strings.TrimPrefix(remotePath, "~"))
		}

		// Check if path exists
		info, err := os.Stat(expandedPath)
		if err != nil {
			return fmt.Errorf("local path does not exist: %s: %w", expandedPath, err)
		}

		if !info.IsDir() {
			return fmt.Errorf("path is not a directory: %s", expandedPath)
		}
	}

	return nil
}

// resetPanelToLocal resets the specified panel back to local filesystem
func (a *App) resetPanelToLocal(rightPane bool) {
	if rightPane {
		if a.rightConn != nil {
			a.rightConn.Disconnect()
		}
		a.rightConn = NewLocalConnection()
		a.rightConnInfo = nil
		a.rightBrowser = NewFileBrowser(a.window, a.rightConn, nil, func(id int) {
			a.rightSelectedFile = id
		})
	} else {
		if a.leftConn != nil {
			a.leftConn.Disconnect()
		}
		a.leftConn = NewLocalConnection()
		a.leftConnInfo = nil
		a.leftBrowser = NewFileBrowser(a.window, a.leftConn, nil, func(id int) {
			a.leftSelectedFile = id
		})
	}
	a.updateLayout()
}

// joinPanelFilePath builds a full path for a selected file; rsync panels use user@host:/path semantics.
func joinPanelFilePath(fb *FileBrowser, fileName string) string {
	if _, ok := fb.conn.(*RsyncRemoteConnection); ok {
		return rsyncJoinPath(fb.currentPath, fileName)
	}
	if _, ok := fb.conn.(*FileAgentConnection); ok {
		return fileAgentJoinPath(fb.currentPath, fileName)
	}
	return filepath.Join(fb.currentPath, fileName)
}

// createSyncTransferToolbar creates the sync and transfer toolbar, showing sync buttons only when rsync is connected
func (a *App) createSyncTransferToolbar() {
	buttons := []fyne.CanvasObject{}

	// Check if either connection is rsync
	hasRsync := (a.leftConnInfo != nil && a.leftConnInfo.Type == "rsync") ||
		(a.rightConnInfo != nil && a.rightConnInfo.Type == "rsync")

	// Add sync buttons only if rsync connection is active
	if hasRsync {
		buttons = append(buttons,
			widget.NewButton("Sync Left→Right", func() {
				a.syncDirectories(false, true) // left to right
			}),
			widget.NewButton("Sync Right→Left", func() {
				a.syncDirectories(true, false) // right to left
			}),
			widget.NewButton("Bidirectional Sync", func() {
				a.syncDirectories(true, true) // bidirectional
			}),
			widget.NewSeparator(),
		)
	}

	// Add transfer buttons
	buttons = append(buttons,
		widget.NewButton("→ Transfer", func() {
			if a.leftSelectedFile >= 0 && a.leftSelectedFile < len(a.leftBrowser.files) {
				file := a.leftBrowser.files[a.leftSelectedFile]
				if !file.IsDir {
					srcPath := joinPanelFilePath(a.leftBrowser, file.Name)
					dstPath := joinPanelFilePath(a.rightBrowser, file.Name)
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
					srcPath := joinPanelFilePath(a.rightBrowser, file.Name)
					dstPath := joinPanelFilePath(a.leftBrowser, file.Name)
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

	a.syncToolbar = container.NewHBox(buttons...)
}

// syncDirectories performs rsync synchronization between left and right panels
// leftToRight: sync from left to right
// rightToLeft: sync from right to left
// If both are true, performs bidirectional sync
func (a *App) syncDirectories(leftToRight, rightToLeft bool) {
	if a.leftBrowser == nil || a.rightBrowser == nil {
		dialog.ShowError(fmt.Errorf("both panels must be connected"), a.window)
		return
	}

	leftPath := a.leftBrowser.currentPath
	rightPath := a.rightBrowser.currentPath

	var syncOps []string
	if leftToRight {
		syncOps = append(syncOps, fmt.Sprintf("Left→Right: %s → %s", leftPath, rightPath))
	}
	if rightToLeft {
		syncOps = append(syncOps, fmt.Sprintf("Right→Left: %s → %s", rightPath, leftPath))
	}

	// Show confirmation dialog
	confirmMsg := fmt.Sprintf("Sync directories?\n\n%s", strings.Join(syncOps, "\n"))
	dialog.ShowConfirm("Rsync Sync", confirmMsg, func(confirmed bool) {
		if !confirmed {
			return
		}

		progress := dialog.NewProgressInfinite("Syncing", "Synchronizing directories...", a.window)
		progress.Show()

		go func() {
			defer func() {
				fyne.Do(func() {
					progress.Hide()
					// Refresh both browsers
					a.leftBrowser.Refresh()
					a.rightBrowser.Refresh()
				})
			}()

			var syncErrs []string

			// Sync left to right
			if leftToRight {
				if err := runRsync(leftPath, rightPath, a.leftConn, a.rightConn, a.leftConnInfo, a.rightConnInfo); err != nil {
					syncErrs = append(syncErrs, fmt.Sprintf("Left→Right sync failed: %v", err))
				}
			}

			// Sync right to left
			if rightToLeft {
				if err := runRsync(rightPath, leftPath, a.rightConn, a.leftConn, a.rightConnInfo, a.leftConnInfo); err != nil {
					syncErrs = append(syncErrs, fmt.Sprintf("Right→Left sync failed: %v", err))
				}
			}

			fyne.Do(func() {
				if len(syncErrs) > 0 {
					dialog.ShowError(errors.New(strings.Join(syncErrs, "\n")), a.window)
				} else {
					dialog.ShowInformation("Success", "Synchronization completed successfully", a.window)
				}
			})
		}()
	}, a.window)
}

// pathLooksLikeRsyncRemote returns true for user@host:/path style arguments.
func pathLooksLikeRsyncRemote(p string) bool {
	return strings.Contains(p, "@") && strings.Contains(p, ":")
}

// connectionForRsyncSSHTransport picks which saved profile should supply rsync's -e ssh ...
// when at least one path is remote. A single rsync invocation uses one ssh settings bundle;
// if both ends are remote with incompatible ssh options, that case is not supported.
func connectionForRsyncSSHTransport(srcPath, dstPath string, srcInfo, dstInfo *Connection) *Connection {
	if srcInfo != nil && (srcInfo.Type == "sftp" || srcInfo.Type == "scp") && pathLooksLikeRsyncRemote(srcPath) {
		return srcInfo
	}
	if dstInfo != nil && (dstInfo.Type == "sftp" || dstInfo.Type == "scp") && pathLooksLikeRsyncRemote(dstPath) {
		return dstInfo
	}
	if srcInfo != nil && srcInfo.Type == "rsync" && pathLooksLikeRsyncRemote(srcPath) {
		return srcInfo
	}
	if dstInfo != nil && dstInfo.Type == "rsync" && pathLooksLikeRsyncRemote(dstPath) {
		return dstInfo
	}
	return nil
}

// effectiveRsyncPathForSync returns --rsync-path value when the source or destination
// uses an rsync profile that specifies a non-default remote rsync executable.
func effectiveRsyncPathForSync(srcInfo, dstInfo *Connection) string {
	if srcInfo != nil && srcInfo.Type == "rsync" {
		if p := strings.TrimSpace(srcInfo.RemoteRsyncPath); p != "" {
			return p
		}
	}
	if dstInfo != nil && dstInfo.Type == "rsync" {
		if p := strings.TrimSpace(dstInfo.RemoteRsyncPath); p != "" {
			return p
		}
	}
	return ""
}

// runRsync executes an rsync command to sync from src to dst
func runRsync(src, dst string, srcConn, dstConn ConnectionManager, srcConnInfo, dstConnInfo *Connection) error {
	// Check if rsync is available
	rsyncPath, err := exec.LookPath("rsync")
	if err != nil {
		return fmt.Errorf("rsync not found in PATH: %w", err)
	}

	// Build rsync command
	// Basic rsync options: -a (archive), -v (verbose), --delete (delete files not in source)
	args := []string{
		"-av",
		"--delete",
	}
	if rp := effectiveRsyncPathForSync(srcConnInfo, dstConnInfo); rp != "" {
		args = append([]string{"--rsync-path=" + rp}, args...)
	}

	// Handle remote paths if needed
	srcPath := src
	dstPath := dst

	// If source is remote (SFTP/SCP), build remote path
	if srcConnInfo != nil && (srcConnInfo.Type == "sftp" || srcConnInfo.Type == "scp") {
		user := srcConnInfo.Username
		host := srcConnInfo.Host
		if !strings.HasSuffix(srcPath, "/") {
			srcPath += "/"
		}
		srcPath = fmt.Sprintf("%s@%s:%s", user, host, srcPath)
	} else {
		// Local or rsync-style source - ensure trailing slash
		if !strings.HasSuffix(srcPath, "/") {
			srcPath += "/"
		}
	}

	// If destination is remote (SFTP/SCP), build remote path
	if dstConnInfo != nil && (dstConnInfo.Type == "sftp" || dstConnInfo.Type == "scp") {
		user := dstConnInfo.Username
		host := dstConnInfo.Host
		if !strings.HasSuffix(dstPath, "/") {
			dstPath += "/"
		}
		dstPath = fmt.Sprintf("%s@%s:%s", user, host, dstPath)
	} else {
		if !strings.HasSuffix(dstPath, "/") {
			dstPath += "/"
		}
	}

	if sshProf := connectionForRsyncSSHTransport(srcPath, dstPath, srcConnInfo, dstConnInfo); sshProf != nil && rsyncNeedsExplicitSSH(sshProf) {
		args = append([]string{"-e", buildRsyncSSHShell(sshProf)}, args...)
	}

	// Add source and destination paths
	args = append(args, srcPath, dstPath)

	// Execute rsync command
	cmd := exec.Command(rsyncPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync command failed: %w", err)
	}

	return nil
}

// setupMenuBeforePassword sets up a menu before password entry with all available options
func (a *App) setupMenuBeforePassword() {
	settingsMenu := fyne.NewMenu("Settings",
		fyne.NewMenuItem("Show Window", func() {
			a.window.Show()
			a.window.RequestFocus()
		}),
		fyne.NewMenuItem("Hide Window", func() {
			a.window.Hide()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Theme Settings", func() {
			// Theme settings can be accessed before password
			a.showThemeSettingsDialog()
		}),
		fyne.NewMenuItem("Application Settings", func() {
			a.showApplicationSettingsDialog()
		}),
		fyne.NewMenuItem("Change Master Password", func() {
			a.showChangePasswordDialog()
		}),
		fyne.NewMenuItem("Remove ALL Settings", func() {
			a.showRemoveAllSettingsDialog()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Debug Mode", func() {
			a.toggleDebugMode()
		}),
		fyne.NewMenuItem("View Debug Log", func() {
			a.showDebugLogDialog()
		}),
	)

	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("Help", func() {
			a.showHelpDialogBeforePassword(a.window)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Check for Update", func() {
			a.checkForUpdate()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("About", func() {
			a.showAboutDialogBeforePassword(a.window)
		}),
	)

	toolsMenu := fyne.NewMenu("Tools",
		fyne.NewMenuItem("Run LAN file agent on this computer…", func() {
			a.showFileAgentLocalWindow()
		}),
	)

	mainMenu := fyne.NewMainMenu(settingsMenu, toolsMenu, helpMenu)
	a.window.SetMainMenu(mainMenu)
}

func (a *App) setupMenu() {
	settingsMenu := fyne.NewMenu("Settings",
		fyne.NewMenuItem("Show Window", func() {
			a.window.Show()
			a.window.RequestFocus()
		}),
		fyne.NewMenuItem("Hide Window", func() {
			a.window.Hide()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Theme Settings", func() {
			a.showThemeSettingsDialog()
		}),
		fyne.NewMenuItem("Application Settings", func() {
			a.showApplicationSettingsDialog()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Change Master Password", func() {
			a.showChangePasswordDialog()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Remove ALL Settings", func() {
			a.showRemoveAllSettingsDialog()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Debug Mode", func() {
			a.toggleDebugMode()
		}),
		fyne.NewMenuItem("View Debug Log", func() {
			a.showDebugLogDialog()
		}),
	)

	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("Help", func() {
			if a.db != nil {
				a.showHelpDialog()
			} else {
				a.showHelpDialogBeforePassword(a.window)
			}
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Check for Update", func() {
			a.checkForUpdate()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("About", func() {
			if a.db != nil {
				a.showAboutDialog()
			} else {
				a.showAboutDialogBeforePassword(a.window)
			}
		}),
	)

	toolsMenu := fyne.NewMenu("Tools",
		fyne.NewMenuItem("Run LAN file agent on this computer…", func() {
			a.showFileAgentLocalWindow()
		}),
	)

	mainMenu := fyne.NewMainMenu(settingsMenu, toolsMenu, helpMenu)
	a.window.SetMainMenu(mainMenu)
}

// toggleDebugMode toggles debug logging on/off
func (a *App) toggleDebugMode() {
	if globalDebugLogger == nil {
		dialog.ShowError(fmt.Errorf("debug logger not initialized"), a.window)
		return
	}

	currentState := globalDebugLogger.IsEnabled()
	newState := !currentState
	globalDebugLogger.SetEnabled(newState)
	a.app.Preferences().SetBool("debug_mode", newState)

	status := "disabled"
	if newState {
		status = "enabled"
	}
	dialog.ShowInformation("Debug Mode", fmt.Sprintf("Debug logging is now %s.\n\nLog file: %s", status, globalDebugLogger.GetLogFilePath()), a.window)
}

// showDebugLogDialog displays the debug log file
func (a *App) showDebugLogDialog() {
	if globalDebugLogger == nil {
		dialog.ShowError(fmt.Errorf("debug logger not initialized"), a.window)
		return
	}

	logPath := globalDebugLogger.GetLogFilePath()

	// Read log file
	content, err := os.ReadFile(logPath)
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to read log file: %w", err), a.window)
		return
	}

	// Create dialog window
	logWindow := a.app.NewWindow("Debug Log")
	logWindow.Resize(fyne.NewSize(800, 600))

	// Create scrollable text widget
	text := widget.NewRichTextFromMarkdown("```\n" + string(content) + "\n```")
	scroll := container.NewScroll(text)
	scroll.SetMinSize(fyne.NewSize(780, 550))

	// Buttons
	openBtn := widget.NewButton("Open Log File", func() {
		// Use system default application to open the file
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", logPath)
		case "linux":
			cmd = exec.Command("xdg-open", logPath)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", "", logPath)
		default:
			dialog.ShowError(fmt.Errorf("unsupported platform"), a.window)
			return
		}
		if err := cmd.Run(); err != nil {
			dialog.ShowError(fmt.Errorf("failed to open log file: %w", err), a.window)
		}
	})
	closeBtn := widget.NewButton("Close", func() {
		logWindow.Close()
	})

	contentBox := container.NewBorder(nil, container.NewHBox(openBtn, closeBtn), nil, nil, scroll)
	logWindow.SetContent(container.NewPadded(contentBox))

	// Register and show
	a.registerDialog(logWindow)
	logWindow.Show()
	a.centerDialogOnMainWindow(logWindow)
}

func (a *App) setupSystemTrayMenu() {
	// Check if window is still valid (might have been closed)
	if a.window == nil {
		return
	}

	desk, ok := a.app.(desktop.App)
	if !ok {
		// System tray not supported on this platform
		return
	}

	// Determine icon based on month
	_, month, _ := time.Now().Date()
	var trayIcon fyne.Resource
	if month == time.December {
		trayIcon = resourceKrankyBearChristmasGrinchPng
	} else {
		trayIcon = resourceKrankyBearCowboyBrownPng
	}

	// Create menu items - use appropriate dialog functions based on whether db is initialized
	var helpAction func()
	var aboutAction func()
	if a.db != nil {
		helpAction = func() {
			a.showHelpDialog()
		}
		aboutAction = func() {
			a.showAboutDialog()
		}
	} else {
		helpAction = func() {
			a.showHelpDialogBeforePassword(a.window)
		}
		aboutAction = func() {
			a.showAboutDialogBeforePassword(a.window)
		}
	}

	trayMenu := fyne.NewMenu(appName,
		fyne.NewMenuItem("Show", func() {
			a.showAllTrackedWindows()
		}),
		fyne.NewMenuItem("Hide", func() {
			a.hideAllTrackedWindows()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Theme Settings", func() {
			if a.db != nil {
				a.showThemeSettingsDialog()
			}
		}),
		fyne.NewMenuItem("Application Settings", func() {
			if a.db != nil {
				a.showApplicationSettingsDialog()
			}
		}),
		fyne.NewMenuItem("Change Master Password", func() {
			if a.db != nil {
				a.showChangePasswordDialog()
			}
		}),
		fyne.NewMenuItem("Remove ALL Settings", func() {
			if a.db != nil {
				a.showRemoveAllSettingsDialog()
			}
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Debug Mode", func() {
			if a.db != nil {
				a.toggleDebugMode()
			}
		}),
		fyne.NewMenuItem("View Debug Log", func() {
			if a.db != nil {
				a.showDebugLogDialog()
			}
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Run LAN file agent locally…", func() {
			a.showFileAgentLocalWindow()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Help", helpAction),
		fyne.NewMenuItem("Check for Update", func() {
			a.checkForUpdate()
		}),
		fyne.NewMenuItem("About", aboutAction),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() {
			desk.SetSystemTrayMenu(nil)
			a.systemTrayActive = false
			if a.db != nil {
				a.db.Close()
			}
			a.app.Quit()
		}),
	)

	// Set icon first, then menu
	desk.SetSystemTrayIcon(trayIcon)
	desk.SetSystemTrayMenu(trayMenu)
	a.systemTrayActive = true
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

	releasenoteslink, rnerr := url.Parse("https://github.com/amarillier/KrankyBearFileMover/blob/allanm/ReleaseNotes.txt")
	if rnerr != nil {
		fyne.LogError("Could not parse URL", rnerr)
	}
	myreleasenoteslink := widget.NewHyperlink("https://github.com/amarillier/KrankyBearFileMover/blob/allanm/ReleaseNotes.txt", releasenoteslink)
	myreleasenoteslink.Alignment = fyne.TextAlignLeading

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
		myreleasenoteslink,
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

		// Set close intercept to properly clean up (after registerDialog to override its intercept)
		a.updateWindow.SetCloseIntercept(func() {
			windowToClose := a.updateWindow
			title := windowToClose.Title()

			// Remove from child windows
			for i, w := range a.childWindows {
				if w == windowToClose {
					a.childWindows = append(a.childWindows[:i], a.childWindows[i+1:]...)
					break
				}
			}
			// Remove from open dialogs
			delete(a.openDialogs, title)
			// Clear the reference
			a.updateWindow = nil
			windowToClose.Close()
		})
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
	a.updateWindow.RequestFocus()
}

func (a *App) showThemeSettingsDialog() {
	// Check if dialog is already open
	if existingWindow := a.showOrFocusDialog("Theme Settings"); existingWindow != nil {
		return
	}

	dialogWindow := a.app.NewWindow("Theme Settings")
	dialogWindow.Resize(fyne.NewSize(400, 200))

	// Register dialog (this will set up proper cleanup on close)
	a.registerDialog(dialogWindow)

	// Get current theme preference
	savedTheme := a.app.Preferences().StringWithFallback("theme", "default")
	var currentThemeText string
	if savedTheme == "light" {
		currentThemeText = "Light"
	} else if savedTheme == "dark" {
		currentThemeText = "Dark"
	} else {
		currentThemeText = "Default"
	}

	currentLabel := widget.NewLabel(fmt.Sprintf("Current theme: %s", currentThemeText))
	currentLabel.Alignment = fyne.TextAlignCenter

	lightBtn := widget.NewButton("Light Theme", func() {
		// Set light theme wrapped in our custom theme
		a.app.Settings().SetTheme(&appTheme{Theme: theme.LightTheme()})
		// Save preference
		a.app.Preferences().SetString("theme", "light")
		currentLabel.SetText("Current theme: Light")
		// Refresh all windows to apply theme change
		if a.window != nil {
			a.window.Content().Refresh()
		}
		for _, w := range a.childWindows {
			if w != nil {
				w.Content().Refresh()
			}
		}
	})

	darkBtn := widget.NewButton("Dark Theme", func() {
		// Set dark theme wrapped in our custom theme
		a.app.Settings().SetTheme(&appTheme{Theme: theme.DarkTheme()})
		// Save preference
		a.app.Preferences().SetString("theme", "dark")
		currentLabel.SetText("Current theme: Dark")
		// Refresh all windows to apply theme change
		if a.window != nil {
			a.window.Content().Refresh()
		}
		for _, w := range a.childWindows {
			if w != nil {
				w.Content().Refresh()
			}
		}
	})

	defaultBtn := widget.NewButton("Default Theme", func() {
		// Set default theme wrapped in our custom theme
		a.app.Settings().SetTheme(&appTheme{Theme: theme.DefaultTheme()})
		// Save preference
		a.app.Preferences().SetString("theme", "default")
		currentLabel.SetText("Current theme: Default")
		// Refresh all windows to apply theme change
		if a.window != nil {
			a.window.Content().Refresh()
		}
		for _, w := range a.childWindows {
			if w != nil {
				w.Content().Refresh()
			}
		}
	})

	closeBtn := widget.NewButton("Close", func() {
		dialogWindow.Close()
	})

	content := container.NewVBox(
		widget.NewLabel("Select Theme"),
		currentLabel,
		container.NewHBox(lightBtn, darkBtn, defaultBtn),
		widget.NewLabel("Note: Theme changes apply to all Fyne applications."),
	)

	bottomButtons := container.NewHBox(closeBtn)
	dialogWindow.SetContent(container.NewBorder(
		nil,
		bottomButtons,
		nil,
		nil,
		content,
	))
	a.centerDialogOnMainWindow(dialogWindow)
}

// loadPanelColors loads panel colors from preferences
func (a *App) loadPanelColors() {
	// Load left panel color
	if leftColorHex := a.app.Preferences().StringWithFallback("left_panel_color", ""); leftColorHex != "" {
		if c := parseColorHex(leftColorHex); c != nil {
			a.leftPanelColor = c
		}
	}
	// Load right panel color
	if rightColorHex := a.app.Preferences().StringWithFallback("right_panel_color", ""); rightColorHex != "" {
		if c := parseColorHex(rightColorHex); c != nil {
			a.rightPanelColor = c
		}
	}
}

// parseColorHex parses a hex color string (e.g., "#RRGGBB" or "#RRGGBBAA")
func parseColorHex(hexStr string) color.Color {
	hexStr = strings.TrimPrefix(hexStr, "#")
	if len(hexStr) == 6 {
		hexStr += "FF" // Add alpha if missing
	}
	if len(hexStr) != 8 {
		return nil
	}
	bytes, err := hex.DecodeString(hexStr)
	if err != nil || len(bytes) != 4 {
		return nil
	}
	return color.NRGBA{R: bytes[0], G: bytes[1], B: bytes[2], A: bytes[3]}
}

// colorToHex converts a color to hex string
func colorToHex(c color.Color) string {
	r, g, b, a := c.RGBA()
	return fmt.Sprintf("#%02X%02X%02X%02X", r>>8, g>>8, b>>8, a>>8)
}

// showApplicationSettingsDialog shows the application settings dialog
func (a *App) showApplicationSettingsDialog() {
	// Check if dialog is already open
	if existingWindow := a.showOrFocusDialog("Application Settings"); existingWindow != nil {
		return
	}

	dialogWindow := a.app.NewWindow("Application Settings")
	dialogWindow.Resize(fyne.NewSize(600, 530))

	// Register dialog
	a.registerDialog(dialogWindow)

	// Get current timeout
	currentTimeout := a.app.Preferences().IntWithFallback("connection_timeout_seconds", 10)
	timeoutEntry := widget.NewEntry()
	timeoutEntry.SetText(fmt.Sprintf("%d", currentTimeout))
	timeoutEntry.SetPlaceHolder("10")

	pwdGraceMin := a.app.Preferences().IntWithFallback("master_password_prompt_timeout_minutes", 5)
	pwdTimeoutEntry := widget.NewEntry()
	pwdTimeoutEntry.SetText(fmt.Sprintf("%d", pwdGraceMin))
	pwdTimeoutEntry.SetPlaceHolder("5")

	// Remember the master password in this computer's OS keychain.
	rememberPwd := a.app.Preferences().BoolWithFallback("remember_master_password", false)
	rememberPwdCheck := widget.NewCheck("Remember master password in this computer's keychain", nil)
	rememberPwdCheck.SetChecked(rememberPwd)

	// Color pickers for panels
	leftColorHex := a.app.Preferences().StringWithFallback("left_panel_color", "")
	rightColorHex := a.app.Preferences().StringWithFallback("right_panel_color", "")

	leftColorEntry := widget.NewEntry()
	leftColorEntry.SetText(leftColorHex)
	leftColorEntry.SetPlaceHolder("#RRGGBB or #RRGGBBAA")
	leftColorEntry.Wrapping = fyne.TextWrapOff

	rightColorEntry := widget.NewEntry()
	rightColorEntry.SetText(rightColorHex)
	rightColorEntry.SetPlaceHolder("#RRGGBB or #RRGGBBAA")
	rightColorEntry.Wrapping = fyne.TextWrapOff

	// Color preview rectangles
	var leftColorPreview *canvas.Rectangle
	var rightColorPreview *canvas.Rectangle

	if leftColorHex != "" && parseColorHex(leftColorHex) != nil {
		leftColorPreview = canvas.NewRectangle(parseColorHex(leftColorHex))
	} else {
		leftColorPreview = canvas.NewRectangle(theme.BackgroundColor())
	}
	leftColorPreview.SetMinSize(fyne.NewSize(50, 30))

	if rightColorHex != "" && parseColorHex(rightColorHex) != nil {
		rightColorPreview = canvas.NewRectangle(parseColorHex(rightColorHex))
	} else {
		rightColorPreview = canvas.NewRectangle(theme.BackgroundColor())
	}
	rightColorPreview.SetMinSize(fyne.NewSize(50, 30))

	// Update preview when color entry changes
	updateLeftPreview := func() {
		if c := parseColorHex(leftColorEntry.Text); c != nil {
			leftColorPreview.FillColor = c
			leftColorPreview.Refresh()
		} else {
			leftColorPreview.FillColor = theme.BackgroundColor()
			leftColorPreview.Refresh()
		}
	}
	updateRightPreview := func() {
		if c := parseColorHex(rightColorEntry.Text); c != nil {
			rightColorPreview.FillColor = c
			rightColorPreview.Refresh()
		} else {
			rightColorPreview.FillColor = theme.BackgroundColor()
			rightColorPreview.Refresh()
		}
	}

	leftColorEntry.OnChanged = func(_ string) {
		updateLeftPreview()
	}
	rightColorEntry.OnChanged = func(_ string) {
		updateRightPreview()
	}

	// Color picker buttons - show advanced color picker dialog
	leftColorBtn := widget.NewButton("Pick Color", func() {
		currentColor := parseColorHex(leftColorEntry.Text)
		if currentColor == nil {
			currentColor = theme.BackgroundColor()
		}
		a.showAdvancedColorPicker("Select Left Panel Color", currentColor, func(c color.Color) {
			if c != nil {
				hexStr := colorToHex(c)
				leftColorEntry.SetText(hexStr)
				updateLeftPreview()
			}
		}, dialogWindow)
	})

	rightColorBtn := widget.NewButton("Pick Color", func() {
		currentColor := parseColorHex(rightColorEntry.Text)
		if currentColor == nil {
			currentColor = theme.BackgroundColor()
		}
		a.showAdvancedColorPicker("Select Right Panel Color", currentColor, func(c color.Color) {
			if c != nil {
				hexStr := colorToHex(c)
				rightColorEntry.SetText(hexStr)
				updateRightPreview()
			}
		}, dialogWindow)
	})

	// Reset colors button
	resetColorsBtn := widget.NewButton("Reset to Default Colors", func() {
		leftColorEntry.SetText("")
		rightColorEntry.SetText("")
		leftColorPreview.FillColor = theme.BackgroundColor()
		rightColorPreview.FillColor = theme.BackgroundColor()
		leftColorPreview.Refresh()
		rightColorPreview.Refresh()
	})

	// Save button
	saveBtn := widget.NewButton("Save", func() {
		// Validate and save timeout
		timeoutStr := timeoutEntry.Text
		timeout, err := strconv.Atoi(timeoutStr)
		if err != nil || timeout < 1 || timeout > 300 {
			dialog.ShowError(fmt.Errorf("timeout must be between 1 and 300 seconds"), dialogWindow)
			return
		}
		a.app.Preferences().SetInt("connection_timeout_seconds", timeout)

		pwdGraceStr := strings.TrimSpace(pwdTimeoutEntry.Text)
		pwdGrace, err := strconv.Atoi(pwdGraceStr)
		if err != nil || pwdGrace < 0 || pwdGrace > 1440 {
			dialog.ShowError(fmt.Errorf("master password re-prompt timeout must be between 0 and 1440 minutes (0 = always prompt)"), dialogWindow)
			return
		}
		a.app.Preferences().SetInt("master_password_prompt_timeout_minutes", pwdGrace)
		if pwdGrace <= 0 {
			a.settingsAuthExpiry = time.Time{}
		}

		// Apply the "remember master password" toggle.
		wantRemember := rememberPwdCheck.Checked
		if wantRemember != rememberPwd {
			if wantRemember {
				if a.db == nil {
					dialog.ShowError(fmt.Errorf("cannot store master password: database is locked"), dialogWindow)
					return
				}
				if err := keychainSetMasterPassword(a.db.MasterPassword()); err != nil {
					dialog.ShowError(fmt.Errorf("failed to store master password in keychain: %w", err), dialogWindow)
					return
				}
			} else {
				if err := keychainDeleteMasterPassword(); err != nil {
					dialog.ShowError(fmt.Errorf("failed to remove master password from keychain: %w", err), dialogWindow)
					return
				}
			}
			a.app.Preferences().SetBool("remember_master_password", wantRemember)
		}

		// Validate and save left panel color
		leftColorStr := strings.TrimSpace(leftColorEntry.Text)
		if leftColorStr == "" {
			a.app.Preferences().RemoveValue("left_panel_color")
			a.leftPanelColor = nil
		} else if c := parseColorHex(leftColorStr); c != nil {
			a.app.Preferences().SetString("left_panel_color", leftColorStr)
			a.leftPanelColor = c
		} else {
			dialog.ShowError(fmt.Errorf("invalid left panel color format. Use #RRGGBB or #RRGGBBAA"), dialogWindow)
			return
		}

		// Validate and save right panel color
		rightColorStr := strings.TrimSpace(rightColorEntry.Text)
		if rightColorStr == "" {
			a.app.Preferences().RemoveValue("right_panel_color")
			a.rightPanelColor = nil
		} else if c := parseColorHex(rightColorStr); c != nil {
			a.app.Preferences().SetString("right_panel_color", rightColorStr)
			a.rightPanelColor = c
		} else {
			dialog.ShowError(fmt.Errorf("invalid right panel color format. Use #RRGGBB or #RRGGBBAA"), dialogWindow)
			return
		}

		// Update layout to apply colors
		a.updateLayout()

		// Remove from tracking before closing
		delete(a.openDialogs, "Application Settings")
		dialog.ShowInformation("Settings Saved", "Application settings have been saved.", dialogWindow)
		dialogWindow.Close()
	})

	// Cancel button
	cancelBtn := widget.NewButton("Cancel", func() {
		// Remove from tracking before closing
		delete(a.openDialogs, "Application Settings")
		dialogWindow.Close()
	})

	// Layout - make color entry fields wider
	// Use Border layout with entry field in center to allow it to expand
	leftColorRow := container.NewBorder(
		nil, nil,
		widget.NewLabel("Color:"),
		container.NewHBox(leftColorBtn, leftColorPreview),
		leftColorEntry,
	)

	rightColorRow := container.NewBorder(
		nil, nil,
		widget.NewLabel("Color:"),
		container.NewHBox(rightColorBtn, rightColorPreview),
		rightColorEntry,
	)

	content := container.NewVBox(
		widget.NewLabel("Connection Timeout (seconds):"),
		timeoutEntry,
		widget.NewLabel(""),
		widget.NewLabel("Master password re-prompt timeout (minutes):"),
		pwdTimeoutEntry,
		widget.NewLabel("After a successful master password entry, editing connections or changing the master password will not ask again until this period ends. Use 0 to require the password every time. Default is 5."),
		widget.NewLabel(""),
		rememberPwdCheck,
		widget.NewLabel("When enabled, this computer's OS keychain stores your master password so you are not prompted at launch. The encrypted database is unchanged and stays portable: copied to another computer (with no keychain entry), it will still prompt for the master password."),
		widget.NewLabel(""),
		widget.NewLabel("Panel Colors:"),
		widget.NewLabel("Left Panel:"),
		leftColorRow,
		widget.NewLabel("Right Panel:"),
		rightColorRow,
		resetColorsBtn,
		widget.NewLabel(""),
		container.NewHBox(saveBtn, cancelBtn),
	)

	dialogWindow.SetContent(container.NewPadded(content))
	dialogWindow.SetCloseIntercept(func() {
		// Remove from tracking when closed
		delete(a.openDialogs, "Application Settings")
		dialogWindow.Close()
	})
	dialogWindow.Show()
	a.centerDialogOnMainWindow(dialogWindow)
}

// showAdvancedColorPicker shows an advanced color picker dialog with RGB/HSV controls
func (a *App) showAdvancedColorPicker(title string, initialColor color.Color, onSelect func(color.Color), parent fyne.Window) {
	pickerWindow := a.app.NewWindow(title)
	pickerWindow.Resize(fyne.NewSize(500, 400))

	// Convert initial color to NRGBA for easier manipulation
	var currentColor color.NRGBA
	if initialColor != nil {
		r, g, b, a := initialColor.RGBA()
		currentColor = color.NRGBA{
			R: uint8(r >> 8),
			G: uint8(g >> 8),
			B: uint8(b >> 8),
			A: uint8(a >> 8),
		}
	} else {
		currentColor = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}

	// Color preview
	colorPreview := canvas.NewRectangle(currentColor)
	colorPreview.SetMinSize(fyne.NewSize(200, 100))

	// RGB sliders
	rSlider := widget.NewSlider(0, 255)
	rSlider.SetValue(float64(currentColor.R))
	rSlider.Step = 1

	gSlider := widget.NewSlider(0, 255)
	gSlider.SetValue(float64(currentColor.G))
	gSlider.Step = 1

	bSlider := widget.NewSlider(0, 255)
	bSlider.SetValue(float64(currentColor.B))
	bSlider.Step = 1

	aSlider := widget.NewSlider(0, 255)
	aSlider.SetValue(float64(currentColor.A))
	aSlider.Step = 1

	// RGB entry fields
	rEntry := widget.NewEntry()
	rEntry.SetText(fmt.Sprintf("%d", currentColor.R))
	rEntry.Wrapping = fyne.TextWrapOff

	gEntry := widget.NewEntry()
	gEntry.SetText(fmt.Sprintf("%d", currentColor.G))
	gEntry.Wrapping = fyne.TextWrapOff

	bEntry := widget.NewEntry()
	bEntry.SetText(fmt.Sprintf("%d", currentColor.B))
	bEntry.Wrapping = fyne.TextWrapOff

	aEntry := widget.NewEntry()
	aEntry.SetText(fmt.Sprintf("%d", currentColor.A))
	aEntry.Wrapping = fyne.TextWrapOff

	// Hex entry
	hexEntry := widget.NewEntry()
	hexEntry.SetText(colorToHex(currentColor))
	hexEntry.Wrapping = fyne.TextWrapOff
	hexEntry.SetPlaceHolder("#RRGGBBAA")

	// Update color from RGB values
	updateColor := func() {
		r := uint8(rSlider.Value)
		g := uint8(gSlider.Value)
		b := uint8(bSlider.Value)
		a := uint8(aSlider.Value)

		currentColor = color.NRGBA{R: r, G: g, B: b, A: a}
		colorPreview.FillColor = currentColor
		colorPreview.Refresh()

		// Update hex entry
		hexEntry.SetText(colorToHex(currentColor))
	}

	// Update from sliders
	rSlider.OnChanged = func(v float64) {
		rEntry.SetText(fmt.Sprintf("%.0f", v))
		updateColor()
	}
	gSlider.OnChanged = func(v float64) {
		gEntry.SetText(fmt.Sprintf("%.0f", v))
		updateColor()
	}
	bSlider.OnChanged = func(v float64) {
		bEntry.SetText(fmt.Sprintf("%.0f", v))
		updateColor()
	}
	aSlider.OnChanged = func(v float64) {
		aEntry.SetText(fmt.Sprintf("%.0f", v))
		updateColor()
	}

	// Update from entry fields
	updateFromEntry := func(entry *widget.Entry, slider *widget.Slider) {
		val, err := strconv.Atoi(entry.Text)
		if err == nil && val >= 0 && val <= 255 {
			slider.SetValue(float64(val))
		}
	}

	rEntry.OnChanged = func(_ string) {
		updateFromEntry(rEntry, rSlider)
	}
	gEntry.OnChanged = func(_ string) {
		updateFromEntry(gEntry, gSlider)
	}
	bEntry.OnChanged = func(_ string) {
		updateFromEntry(bEntry, bSlider)
	}
	aEntry.OnChanged = func(_ string) {
		updateFromEntry(aEntry, aSlider)
	}

	// Update from hex entry
	hexEntry.OnChanged = func(_ string) {
		if c := parseColorHex(hexEntry.Text); c != nil {
			r, g, b, a := c.RGBA()
			currentColor = color.NRGBA{
				R: uint8(r >> 8),
				G: uint8(g >> 8),
				B: uint8(b >> 8),
				A: uint8(a >> 8),
			}
			rSlider.SetValue(float64(currentColor.R))
			gSlider.SetValue(float64(currentColor.G))
			bSlider.SetValue(float64(currentColor.B))
			aSlider.SetValue(float64(currentColor.A))
			rEntry.SetText(fmt.Sprintf("%d", currentColor.R))
			gEntry.SetText(fmt.Sprintf("%d", currentColor.G))
			bEntry.SetText(fmt.Sprintf("%d", currentColor.B))
			aEntry.SetText(fmt.Sprintf("%d", currentColor.A))
			colorPreview.FillColor = currentColor
			colorPreview.Refresh()
		}
	}

	// Also offer the basic color picker as an option
	basicColorBtn := widget.NewButton("Use System Color Picker", func() {
		dialog.ShowColorPicker(title, "Choose a color", func(c color.Color) {
			if c != nil {
				r, g, b, a := c.RGBA()
				currentColor = color.NRGBA{
					R: uint8(r >> 8),
					G: uint8(g >> 8),
					B: uint8(b >> 8),
					A: uint8(a >> 8),
				}
				rSlider.SetValue(float64(currentColor.R))
				gSlider.SetValue(float64(currentColor.G))
				bSlider.SetValue(float64(currentColor.B))
				aSlider.SetValue(float64(currentColor.A))
				rEntry.SetText(fmt.Sprintf("%d", currentColor.R))
				gEntry.SetText(fmt.Sprintf("%d", currentColor.G))
				bEntry.SetText(fmt.Sprintf("%d", currentColor.B))
				aEntry.SetText(fmt.Sprintf("%d", currentColor.A))
				hexEntry.SetText(colorToHex(currentColor))
				updateColor()
			}
		}, pickerWindow)
	})

	// OK and Cancel buttons
	okBtn := widget.NewButton("OK", func() {
		onSelect(currentColor)
		pickerWindow.Close()
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		pickerWindow.Close()
	})

	// Layout
	content := container.NewVBox(
		widget.NewLabel("Color Preview:"),
		colorPreview,
		widget.NewLabel(""),
		widget.NewLabel("RGB Controls:"),
		container.NewBorder(nil, nil, widget.NewLabel("Red:"), rEntry, rSlider),
		container.NewBorder(nil, nil, widget.NewLabel("Green:"), gEntry, gSlider),
		container.NewBorder(nil, nil, widget.NewLabel("Blue:"), bEntry, bSlider),
		container.NewBorder(nil, nil, widget.NewLabel("Alpha:"), aEntry, aSlider),
		widget.NewLabel(""),
		widget.NewLabel("Hex Color:"),
		hexEntry,
		widget.NewLabel(""),
		basicColorBtn,
		widget.NewLabel(""),
		container.NewHBox(okBtn, cancelBtn),
	)

	pickerWindow.SetContent(container.NewPadded(content))
	pickerWindow.CenterOnScreen()
	pickerWindow.Show()
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

		hintEntry := widget.NewEntry()
		hintEntry.SetPlaceHolder("Optional hint (stored unencrypted)")
		if a.db != nil {
			hintEntry.SetText(a.db.GetPasswordHint())
		}

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

			// Persist the (optional) password hint.
			if err := a.db.SetPasswordHint(hintEntry.Text); err != nil {
				dialog.ShowError(fmt.Errorf("failed to save password hint: %w", err), a.window)
				return
			}

			// If the password is cached in this computer's keychain, refresh it.
			if a.app.Preferences().BoolWithFallback("remember_master_password", false) {
				keychainSetMasterPassword(newPassword)
			}

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
			widget.NewLabel("Password Hint (optional):"),
			hintEntry,
			widget.NewLabel("Shown after 2 failed login attempts. Stored unencrypted — do not put the password itself here."),
			container.NewHBox(
				widget.NewButton("Change Password", changePassword),
				widget.NewButton("Cancel", func() {
					changeDialog.Hide()
				}),
			),
		)

		changeDialog = dialog.NewCustom("Change Master Password", "", content, a.window)
		changeDialog.Resize(fyne.NewSize(420, 380))
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
// to ensure it appears on the same display. Fyne's CenterOnScreen() centers on
// the monitor where the window is currently positioned, so we show the dialog
// first to ensure it's on the same display as the main window, then center it.
func (a *App) centerDialogOnMainWindow(dialogWindow fyne.Window) {
	if a.window == nil {
		dialogWindow.CenterOnScreen()
		return
	}
	// Show the dialog first (it will appear on the same display as the main window)
	// then center it on that display
	dialogWindow.Show()
	dialogWindow.CenterOnScreen()
}

// raiseAndFocusWindow shows a window and requests focus, including a short
// deferred pass for platforms where the first RequestFocus does not stick.
func raiseAndFocusWindow(w fyne.Window) {
	if w == nil {
		return
	}
	w.Show()
	w.RequestFocus()
	go func(win fyne.Window) {
		time.Sleep(40 * time.Millisecond)
		fyne.Do(func() {
			if win == nil || win.Content() == nil {
				return
			}
			win.Show()
			win.RequestFocus()
		})
	}(w)
}

// showOrFocusDialog checks if a dialog with the given title is already open.
// If it exists, brings it to front and focuses it. Otherwise, returns nil.
func (a *App) showOrFocusDialog(title string) fyne.Window {
	if existingWindow, exists := a.openDialogs[title]; exists {
		if existingWindow != nil {
			// Check if window is still valid and visible
			if existingWindow.Content() != nil {
				raiseAndFocusWindow(existingWindow)
				return existingWindow
			} else {
				// Window is no longer valid, clean up
				delete(a.openDialogs, title)
				// Also clear updateWindow reference if it's the update window
				if title == appName+": Update Check" && a.updateWindow == existingWindow {
					a.updateWindow = nil
				}
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
	// Note: Close intercept is set by the caller if needed
	// This allows callers to set their own intercept that can clean up properly
}

// allTrackedWindows lists every window that should participate in tray Show/Hide (deduplicated).
func (a *App) allTrackedWindows() []fyne.Window {
	seen := make(map[fyne.Window]struct{})
	var out []fyne.Window
	add := func(w fyne.Window) {
		if w == nil {
			return
		}
		if _, ok := seen[w]; ok {
			return
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	add(a.window)
	add(a.passwordWindow)
	for _, w := range a.childWindows {
		add(w)
	}
	for _, w := range a.openDialogs {
		add(w)
	}
	add(a.updateWindow)
	add(a.helpWindow)
	return out
}

func (a *App) showAllTrackedWindows() {
	wins := a.allTrackedWindows()
	for _, w := range wins {
		w.Show()
	}
	for _, w := range wins {
		raiseAndFocusWindow(w)
	}
}

func (a *App) hideAllTrackedWindows() {
	for _, w := range a.allTrackedWindows() {
		w.Hide()
	}
}

// showHelpDialogBeforePassword shows Help dialog before password is entered
func (a *App) showHelpDialogBeforePassword(parent fyne.Window) {
	helpTitle := appName + ": Help"
	if existingWindow := a.showOrFocusDialog(helpTitle); existingWindow != nil {
		return
	}

	helpWindow := a.app.NewWindow(helpTitle)

	// Set icon based on month - Christmas Grinch in December, otherwise CowboyBrown
	_, month, _ := time.Now().Date()
	if month == time.December {
		helpWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
	} else {
		helpWindow.SetIcon(resourceKrankyBearCowboyBrownPng)
	}

	hlpText := fileMoverHelpBody()
	hlpText += "\n" + appName + " v " + appVersion
	hlpText += "\n" + appCopyright
	hlpText += "\n\n" + appAuthor + ", using Go and fyne GUI"

	helpLabel := widget.NewLabel(hlpText)
	helpLabel.Wrapping = fyne.TextWrapWord

	tabs := container.NewDocTabs(
		container.NewTabItem("Help", container.NewScroll(helpLabel)),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	helpWindow.Resize(fyne.NewSize(800, 560))
	helpWindow.SetContent(tabs)

	helpWindow.SetCloseIntercept(func() {
		a.untrackDialogWindow(helpTitle, helpWindow)
		helpWindow.Close()
	})
	a.registerDialog(helpWindow)
	a.centerDialogOnMainWindow(helpWindow)
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

	hlpText := fileMoverHelpBody()
	hlpText += "\n" + appName + " v " + appVersion
	hlpText += "\n" + appCopyright
	hlpText += "\n\n" + appAuthor + ", using Go and fyne GUI"

	helpLabel := widget.NewLabel(hlpText)
	helpLabel.Wrapping = fyne.TextWrapWord

	tabs := container.NewDocTabs(
		container.NewTabItem("Help", container.NewScroll(helpLabel)),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	a.helpWindow.Resize(fyne.NewSize(800, 560))
	a.helpWindow.SetContent(tabs)
	a.registerDialog(a.helpWindow)

	a.helpWindow.SetCloseIntercept(func() {
		w := a.helpWindow
		a.helpWindow = nil
		if w != nil {
			a.untrackDialogWindow(w.Title(), w)
			w.Close()
		}
	})

	a.centerDialogOnMainWindow(a.helpWindow)
	a.helpWindow.Show()
}

// showAboutDialogBeforePassword shows About dialog before password is entered
func (a *App) showAboutDialogBeforePassword(parent fyne.Window) {
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
	text.Wrapping = fyne.TextWrapWord
	text.Alignment = fyne.TextAlignCenter
	content := container.NewVBox(
		container.NewCenter(kbImage),
		text,
	)

	dialogWindow := a.app.NewWindow("About")
	if month == time.December {
		dialogWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
	} else {
		dialogWindow.SetIcon(resourceKrankyBearCowboyBrownPng)
	}
	dialogWindow.Resize(fyne.NewSize(500, 500))
	dialogWindow.SetContent(container.NewPadded(content))

	dialogWindow.SetCloseIntercept(func() {
		a.untrackDialogWindow(dialogWindow.Title(), dialogWindow)
		dialogWindow.Close()
	})
	a.registerDialog(dialogWindow)
	a.centerDialogOnMainWindow(dialogWindow)
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
	text.Wrapping = fyne.TextWrapWord
	text.Alignment = fyne.TextAlignCenter
	// Use same layout as showAboutDialogBeforePassword for consistency
	content := container.NewVBox(
		container.NewCenter(kbImage),
		text,
	)

	dialogWindow := a.app.NewWindow("About")
	if month == time.December {
		dialogWindow.SetIcon(resourceKrankyBearChristmasGrinchPng)
	} else {
		dialogWindow.SetIcon(resourceKrankyBearCowboyBrownPng)
	}
	dialogWindow.Resize(fyne.NewSize(500, 500))
	dialogWindow.SetContent(container.NewPadded(content))

	dialogWindow.SetCloseIntercept(func() {
		a.untrackDialogWindow(dialogWindow.Title(), dialogWindow)
		dialogWindow.Close()
	})
	a.registerDialog(dialogWindow)
	a.centerDialogOnMainWindow(dialogWindow)
}

func (a *App) Run() {
	// Show window first - this initializes GLFW
	a.window.Show()

	// Setup a basic menu before password entry so About/Help work from menu bar
	a.setupMenuBeforePassword()

	// Setup system tray menu immediately after window is shown (synchronously)
	// This allows Help/About to be accessed before password entry
	a.setupSystemTrayMenu()

	// Show password dialog after window is ready
	go func() {
		time.Sleep(50 * time.Millisecond) // Small delay to ensure window is initialized
		fyne.Do(func() {
			a.showPasswordDialog(a.window)
		})
	}()

	// Run the event loop (this blocks)
	a.window.ShowAndRun()

	// Cleanup when app exits
	// Note: Don't call SetSystemTrayMenu(nil) here as it can cause crashes
	// The system tray will be cleaned up automatically when the app quits
	if a.db != nil {
		a.db.Close()
	}
}

func main() {
	prepareWindowsCLI()

	fileAgent := flag.Bool("file-agent", false, "Run LAN TLS file agent (foreground, no GUI). Stop with Ctrl+C. Any other -file-agent-* flag on the command line implies this mode.")
	agentListen := flag.String("file-agent-listen", fileAgentDefaultListen, "Agent listen address (e.g. :9742 or 192.168.1.5:9742)")
	agentRoot := flag.String("file-agent-root", ".", "Root directory to share")
	agentPSK := flag.String("file-agent-psk", "", "Pre-shared key (a random key is generated and printed if empty)")
	agentLANOnly := flag.Bool("file-agent-lan-only", true, "Accept only loopback, private, or link-local client IPs")
	agentTTL := flag.Duration("file-agent-ttl", 0, "Stop the agent after this duration (e.g. 30m, 2h). 0 means run until Ctrl+C.")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", filepath.Base(os.Args[0]))
		fmt.Fprintln(os.Stderr, "Default: start the graphical application.")
		fmt.Fprintln(os.Stderr, "Headless: pass -file-agent or any -file-agent-* flag (LAN TLS file agent, no GUI).")
		fmt.Fprintln(os.Stderr, "")
		flag.PrintDefaults()
	}
	for _, arg := range os.Args[1:] {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "-h", "--help", "-?", "/?":
			flag.Usage()
			os.Exit(0)
		}
	}
	flag.Parse()

	impliedAgent := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "file-agent" {
			return
		}
		if strings.HasPrefix(f.Name, "file-agent") {
			impliedAgent = true
		}
	})
	if *fileAgent || impliedAgent {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runFileAgent(ctx, *agentListen, *agentRoot, *agentPSK, *agentLANOnly, *agentTTL); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		return
	}

	app, err := NewApp()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	app.Run()
}

// passwordEntryWithEsc wraps a password entry to handle Esc key
type passwordEntryWithEsc struct {
	widget.BaseWidget
	entry *widget.Entry
	onEsc func()
}

func (p *passwordEntryWithEsc) CreateRenderer() fyne.WidgetRenderer {
	return &passwordEntryWithEscRenderer{
		wrapper: p,
		objects: []fyne.CanvasObject{p.entry},
	}
}

func (p *passwordEntryWithEsc) TypedKey(key *fyne.KeyEvent) {
	if key.Name == fyne.KeyEscape {
		if p.onEsc != nil {
			p.onEsc()
		}
		return
	}
	// Pass through to entry
	if p.entry != nil {
		p.entry.TypedKey(key)
	}
}

func (p *passwordEntryWithEsc) TypedRune(r rune) {
	if p.entry != nil {
		p.entry.TypedRune(r)
	}
}

func (p *passwordEntryWithEsc) FocusGained() {
	if p.entry != nil {
		p.entry.FocusGained()
	}
}

func (p *passwordEntryWithEsc) FocusLost() {
	if p.entry != nil {
		p.entry.FocusLost()
	}
}

type passwordEntryWithEscRenderer struct {
	wrapper *passwordEntryWithEsc
	objects []fyne.CanvasObject
}

func (r *passwordEntryWithEscRenderer) Layout(size fyne.Size) {
	r.objects[0].Resize(size)
	r.objects[0].Move(fyne.NewPos(0, 0))
}

func (r *passwordEntryWithEscRenderer) MinSize() fyne.Size {
	return r.objects[0].MinSize()
}

func (r *passwordEntryWithEscRenderer) Refresh() {
	r.objects[0].Refresh()
}

func (r *passwordEntryWithEscRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *passwordEntryWithEscRenderer) Destroy() {}

// escKeyWrapper is a focusable widget that handles Esc key presses
type escKeyWrapper struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onEsc   func()
}

func (e *escKeyWrapper) CreateRenderer() fyne.WidgetRenderer {
	return &escKeyWrapperRenderer{
		wrapper: e,
		objects: []fyne.CanvasObject{e.content},
	}
}

func (e *escKeyWrapper) TypedKey(key *fyne.KeyEvent) {
	if key.Name == fyne.KeyEscape {
		if e.onEsc != nil {
			e.onEsc()
		}
		return
	}
	// Pass through to content if not Esc
	if e.content != nil {
		if focusable, ok := e.content.(fyne.Focusable); ok {
			focusable.TypedKey(key)
		}
	}
}

func (e *escKeyWrapper) TypedRune(r rune) {
	// Pass through to content
	if e.content != nil {
		if focusable, ok := e.content.(fyne.Focusable); ok {
			focusable.TypedRune(r)
		}
	}
}

func (e *escKeyWrapper) FocusGained() {
	// Widget gained focus - nothing special needed
}

func (e *escKeyWrapper) FocusLost() {
	// Widget lost focus - nothing special needed
}

type escKeyWrapperRenderer struct {
	wrapper *escKeyWrapper
	objects []fyne.CanvasObject
}

func (r *escKeyWrapperRenderer) Layout(size fyne.Size) {
	r.objects[0].Resize(size)
	r.objects[0].Move(fyne.NewPos(0, 0))
}

func (r *escKeyWrapperRenderer) MinSize() fyne.Size {
	return r.objects[0].MinSize()
}

func (r *escKeyWrapperRenderer) Refresh() {
	r.objects[0].Refresh()
}

func (r *escKeyWrapperRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *escKeyWrapperRenderer) Destroy() {}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
