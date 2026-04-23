package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type fyneLogWriter struct {
	app   fyne.App
	entry *widget.Entry
	mu    sync.Mutex
	buf   strings.Builder
}

func (w *fyneLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf.Write(p)
	s := w.buf.String()
	if len(s) > 256*1024 {
		s = s[len(s)-200*1024:]
		w.buf.Reset()
		w.buf.WriteString(s)
	}
	w.mu.Unlock()
	fyne.Do(func() {
		w.entry.SetText(s)
	})
	return len(p), nil
}

func (a *App) stopLocalFileAgent() {
	a.fileAgentStopMu.Lock()
	c := a.fileAgentStop
	a.fileAgentStopMu.Unlock()
	if c != nil {
		c()
	}
}

func (a *App) localFileAgentRunning() bool {
	a.fileAgentStopMu.Lock()
	defer a.fileAgentStopMu.Unlock()
	return a.fileAgentStop != nil
}

// showFileAgentLocalWindow runs the LAN file agent from the GUI (same protocol as CLI -file-agent).
func (a *App) showFileAgentLocalWindow() {
	if a.localFileAgentRunning() {
		dialog.ShowInformation("LAN file agent", "An agent is already running. Stop it from the agent window first.", a.window)
		return
	}

	w := a.app.NewWindow("LAN file agent (this computer)")
	w.Resize(fyne.NewSize(720, 560))

	rootE := widget.NewEntry()
	if home, err := os.UserHomeDir(); err == nil {
		rootE.SetText(home)
	} else {
		rootE.SetText(".")
	}
	rootE.SetPlaceHolder("Directory to share")

	portE := widget.NewEntry()
	portE.SetText("9742")

	pskE := widget.NewEntry()
	pskE.SetPlaceHolder("Optional; leave empty to generate a random key")

	ttlE := widget.NewEntry()
	ttlE.SetPlaceHolder("Optional TTL, e.g. 30m or 2h (empty = until Stop)")

	lanC := widget.NewCheck("Restrict to LAN/private/link-local clients", func(bool) {})
	lanC.SetChecked(true)

	logE := widget.NewMultiLineEntry()
	logE.Wrapping = fyne.TextWrapOff

	startBtn := widget.NewButton("Start listener", nil)
	stopBtn := widget.NewButton("Stop listener", nil)
	stopBtn.Disable()

	startBtn.OnTapped = func() {
		root := strings.TrimSpace(rootE.Text)
		if root == "" {
			dialog.ShowError(fmt.Errorf("choose a root directory to share"), w)
			return
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			dialog.ShowError(fmt.Errorf("root path: %w", err), w)
			return
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			dialog.ShowError(fmt.Errorf("root is not a directory: %s", abs), w)
			return
		}
		root = abs

		port := 9742
		if s := strings.TrimSpace(portE.Text); s != "" {
			var err error
			port, err = strconv.Atoi(s)
			if err != nil || port <= 0 || port > 65535 {
				dialog.ShowError(fmt.Errorf("port must be 1-65535"), w)
				return
			}
		}
		listen := fmt.Sprintf(":%d", port)

		var ttl time.Duration
		if ts := strings.TrimSpace(ttlE.Text); ts != "" {
			d, err := time.ParseDuration(ts)
			if err != nil {
				dialog.ShowError(fmt.Errorf("TTL: %w", err), w)
				return
			}
			ttl = d
		}

		psk := strings.TrimSpace(pskE.Text)
		lanOnly := lanC.Checked

		ctx, cancel := context.WithCancel(context.Background())
		a.fileAgentStopMu.Lock()
		if a.fileAgentStop != nil {
			a.fileAgentStopMu.Unlock()
			cancel()
			return
		}
		a.fileAgentStop = cancel
		a.fileAgentStopMu.Unlock()

		logE.SetText("")
		startBtn.Disable()
		stopBtn.Enable()
		rootE.Disable()
		portE.Disable()
		pskE.Disable()
		ttlE.Disable()
		lanC.Disable()

		logw := &fyneLogWriter{app: a.app, entry: logE}

		go func() {
			defer func() {
				a.fileAgentStopMu.Lock()
				a.fileAgentStop = nil
				a.fileAgentStopMu.Unlock()
				fyne.Do(func() {
					startBtn.Enable()
					stopBtn.Disable()
					rootE.Enable()
					portE.Enable()
					pskE.Enable()
					ttlE.Enable()
					lanC.Enable()
				})
			}()
			err := runFileAgentLog(ctx, listen, root, psk, lanOnly, ttl, logw)
			if err != nil && ctx.Err() == nil {
				fyne.Do(func() {
					dialog.ShowError(err, w)
				})
			}
		}()
	}

	stopBtn.OnTapped = func() {
		a.stopLocalFileAgent()
	}

	logScroll := container.NewScroll(logE)
	logScroll.SetMinSize(fyne.NewSize(100, 220))
	w.SetContent(container.NewBorder(
		container.NewVBox(
			widget.NewLabel("While the listener runs, use this window for PSK and TLS pin. Other panes can stay open, but avoid starting a second agent."),
			container.NewGridWithColumns(2,
				widget.NewLabel("Share root"), rootE,
				widget.NewLabel("Port"), portE,
				widget.NewLabel("Pre-shared key (optional)"), pskE,
				widget.NewLabel("TTL (optional)"), ttlE,
			),
			lanC,
			widget.NewLabel("Log:"),
			logScroll,
			container.NewHBox(startBtn, stopBtn),
		),
		nil, nil, nil,
	))

	w.SetCloseIntercept(func() {
		a.stopLocalFileAgent()
		a.untrackDialogWindow(w.Title(), w)
		w.Close()
	})
	a.registerDialog(w)
	a.centerDialogOnMainWindow(w)
}
