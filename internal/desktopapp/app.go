package desktopapp

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"printer-bridge/internal/activity"
	"printer-bridge/internal/assets"
	"printer-bridge/internal/config"
	"printer-bridge/internal/logging"
	"printer-bridge/internal/server"
)

const disclaimer = "printer-bridge is an independent project, not affiliated with or endorsed by any printer manufacturer."

type application struct {
	app          fyne.App
	window       fyne.Window
	cfgPath      string
	cfg          config.Config
	server       *server.Server
	logger       *logging.Logger
	statusIcon   *widget.Icon
	statusLabel  *widget.Label
	infoLabel    *widget.Label
	messageLabel *widget.Label
	closeNotice  bool
	stopRefresh  chan struct{}
}

func Run() error {
	cfgPath, err := config.ConfigPath()
	if err != nil {
		return err
	}
	cfg, loadResult, err := config.LoadOrCreate(cfgPath)
	if err != nil {
		return err
	}

	logPath, err := config.LogPath()
	if err != nil {
		return err
	}

	logger := logging.New(logPath, logging.DefaultMaxBytes, logging.DefaultMaxBackups, nil)
	if loadResult.Warning != "" {
		logger.Record(activity.Entry{
			Timestamp: time.Now(),
			Endpoint:  "config",
			Outcome:   "warning",
			Detail:    loadResult.Warning,
		})
	}

	fyneApp := fyneapp.NewWithID(config.AppID)
	appIcon := assets.AppIcon()
	fyneApp.SetIcon(appIcon)

	bridgeServer := server.New(cfg, logger)
	ui := &application{
		app:         fyneApp,
		cfgPath:     cfgPath,
		cfg:         cfg,
		server:      bridgeServer,
		logger:      logger,
		stopRefresh: make(chan struct{}),
	}

	ui.window = fyneApp.NewWindow(config.AppDisplayName)
	ui.window.Resize(fyne.NewSize(680, 360))
	ui.window.SetContent(ui.mainContent())

	if err := bridgeServer.Start(); err != nil {
		ui.setMessage("Listener failed to start: " + err.Error())
	} else {
		ui.setMessage("Listening on " + bridgeServer.Status().Address)
	}
	ui.updateStatus()
	ui.setupTray()
	ui.runRefreshLoop()

	ui.window.ShowAndRun()
	close(ui.stopRefresh)
	return nil
}

func (a *application) mainContent() fyne.CanvasObject {
	title := widget.NewLabelWithStyle(config.AppDisplayName, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	subtitle := widget.NewLabel("Local browser-to-printer bridge")
	logo := canvas.NewImageFromResource(assets.AppIcon())
	logo.FillMode = canvas.ImageFillContain
	logoHolder := container.NewGridWrap(fyne.NewSize(96, 96), logo)
	heading := container.NewBorder(nil, nil, logoHolder, nil, container.NewVBox(title, subtitle))

	a.statusIcon = widget.NewIcon(theme.NewErrorThemedResource(theme.ErrorIcon()))
	a.statusLabel = widget.NewLabel("Starting listener")
	a.infoLabel = widget.NewLabel("")
	a.infoLabel.Wrapping = fyne.TextWrapWord
	a.messageLabel = widget.NewLabel("")
	a.messageLabel.Wrapping = fyne.TextWrapWord

	statusRow := container.NewHBox(a.statusIcon, a.statusLabel)

	disclaimerLabel := widget.NewLabel(disclaimer)
	disclaimerLabel.Wrapping = fyne.TextWrapWord

	settingsButton := widget.NewButtonWithIcon("Settings", theme.SettingsIcon(), a.showSettings)
	detailsButton := widget.NewButtonWithIcon("Details", theme.InfoIcon(), a.showDetails)
	helpButton := widget.NewButtonWithIcon("Help", theme.HelpIcon(), a.showHelp)
	logsButton := widget.NewButtonWithIcon("View Logs", theme.DocumentIcon(), a.showLogs)
	quitButton := widget.NewButtonWithIcon("Quit", theme.CancelIcon(), a.quit)
	actions := container.NewHBox(settingsButton, detailsButton, helpButton, logsButton, layout.NewSpacer(), quitButton)

	content := container.NewVBox(heading, statusRow, a.infoLabel, a.messageLabel, disclaimerLabel, actions)

	a.window.SetCloseIntercept(func() {
		if !a.closeNotice {
			a.closeNotice = true
			a.app.SendNotification(&fyne.Notification{
				Title:   "printer-bridge is still running",
				Content: "Use the tray icon or menu to reopen it, or choose Quit to stop the listener.",
			})
		}
		a.window.Hide()
	})

	return container.NewPadded(content)
}

func (a *application) setupTray() {
	desktopApp, ok := a.app.(desktop.App)
	if !ok {
		return
	}

	desktopApp.SetSystemTrayIcon(assets.AppIcon())
	desktopApp.SetSystemTrayWindow(a.window)
	a.updateTrayMenu()
}

func (a *application) updateTrayMenu() {
	desktopApp, ok := a.app.(desktop.App)
	if !ok {
		return
	}

	status := a.server.Status()
	statusText := "Stopped"
	if status.Running {
		statusText = "HTTP: " + status.Address
	}

	desktopApp.SetSystemTrayMenu(fyne.NewMenu(config.AppDisplayName,
		fyne.NewMenuItem("Open printer-bridge", func() {
			a.window.Show()
		}),
		fyne.NewMenuItem("Details", a.showDetails),
		fyne.NewMenuItem("Help", a.showHelp),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(statusText, nil),
		fyne.NewMenuItem("Printer port: "+strconv.Itoa(a.cfg.DefaultPrinterPort), nil),
		fyne.NewMenuItem("Allowed origins: "+strconv.Itoa(len(a.cfg.AllowedOrigins)), nil),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", a.quit),
	))
}

func (a *application) updateStatus() {
	status := a.server.Status()
	if status.Running {
		a.statusIcon.SetResource(theme.NewSuccessThemedResource(theme.ConfirmIcon()))
		a.statusLabel.SetText("Listening on " + status.Address)
	} else {
		a.statusIcon.SetResource(theme.NewErrorThemedResource(theme.ErrorIcon()))
		if status.Error != "" {
			a.statusLabel.SetText("Stopped: " + status.Error)
		} else {
			a.statusLabel.SetText("Stopped")
		}
	}
	a.statusIcon.Refresh()
	a.statusLabel.Refresh()
	a.updateInfo()
	a.updateTrayMenu()
}

func (a *application) updateInfo() {
	if a.infoLabel == nil {
		return
	}
	status := a.server.Status()
	listener := status.Address
	if listener == "" {
		listener = "127.0.0.1:" + strconv.Itoa(a.cfg.HTTPPort)
	}

	a.infoLabel.SetText(fmt.Sprintf(
		"Local listener: %s\nDefault printer port: %d\nDefault printer address: %s\nAllowed origins: %s",
		listener,
		a.cfg.DefaultPrinterPort,
		displayValue(a.cfg.DefaultPrinterAddress, "Not set"),
		displayOriginsSummary(a.cfg.AllowedOrigins),
	))
	a.infoLabel.Refresh()
}

func (a *application) setMessage(message string) {
	if a.messageLabel == nil {
		return
	}
	a.messageLabel.SetText(message)
	a.messageLabel.Refresh()
}

func (a *application) showSettings() {
	settingsWindow := a.app.NewWindow("Settings")
	settingsWindow.Resize(fyne.NewSize(560, 500))

	httpPortEntry := widget.NewEntry()
	httpPortEntry.SetText(strconv.Itoa(a.cfg.HTTPPort))

	printerPortEntry := widget.NewEntry()
	printerPortEntry.SetText(strconv.Itoa(a.cfg.DefaultPrinterPort))

	printerAddressEntry := widget.NewEntry()
	printerAddressEntry.SetText(a.cfg.DefaultPrinterAddress)
	printerAddressEntry.PlaceHolder = "Optional printer IP or hostname"

	originsEntry := widget.NewMultiLineEntry()
	originsEntry.SetText(strings.Join(a.cfg.AllowedOrigins, "\n"))
	originsEntry.SetMinRowsVisible(5)
	originsEntry.PlaceHolder = "Add required web app origins, one per line"

	errorLabel := widget.NewLabel("")
	errorLabel.Wrapping = fyne.TextWrapWord

	form := widget.NewForm(
		settingsFormItem(
			"HTTP port",
			httpPortEntry,
			"Local port used by web apps to reach printer-bridge. Use 8080 unless another app is already using it.",
		),
		settingsFormItem(
			"Default printer port",
			printerPortEntry,
			"Used when a print request does not include a printer port. Many network printers use 9100.",
		),
		settingsFormItem(
			"Default printer address",
			printerAddressEntry,
			"Optional printer IP address or hostname. Web apps can still send a different printer address per request.",
		),
		settingsFormItem(
			"Allowed origins",
			originsEntry,
			"Website addresses allowed to use printer-bridge from a browser. One per line, no page path.",
		),
	)

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		nextCfg, err := settingsConfig(httpPortEntry.Text, printerPortEntry.Text, printerAddressEntry.Text, originsEntry.Text)
		if err != nil {
			errorLabel.SetText(err.Error())
			return
		}

		if err := config.Save(a.cfgPath, nextCfg); err != nil {
			errorLabel.SetText(err.Error())
			return
		}

		a.cfg = nextCfg
		if err := a.server.Restart(nextCfg); err != nil {
			errorLabel.SetText("Settings saved, but listener failed to restart: " + err.Error())
			a.setMessage("Listener failed to restart: " + err.Error())
			a.updateStatus()
			return
		}

		errorLabel.SetText("")
		a.setMessage("Settings saved. Listening on " + a.server.Status().Address)
		a.updateStatus()
		settingsWindow.Close()
	})

	cancelButton := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		settingsWindow.Close()
	})

	settingsWindow.SetContent(container.NewPadded(container.NewBorder(
		nil,
		container.NewHBox(layout.NewSpacer(), cancelButton, saveButton),
		nil,
		nil,
		container.NewVScroll(container.NewVBox(form, errorLabel)),
	)))
	settingsWindow.Show()
}

func settingsFormItem(text string, control fyne.CanvasObject, hint string) *widget.FormItem {
	item := widget.NewFormItem(text, control)
	item.HintText = hint
	return item
}

func (a *application) showDetails() {
	detailsWindow := a.app.NewWindow("Details")
	detailsWindow.Resize(fyne.NewSize(760, 560))

	detailsText := widget.NewTextGridFromString(a.detailsText())

	refreshButton := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() {
		detailsText.SetText(a.detailsText())
	})
	copyButton := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		a.app.Clipboard().SetContent(detailsText.Text())
	})

	detailsWindow.SetContent(container.NewPadded(container.NewBorder(
		container.NewHBox(refreshButton, copyButton, layout.NewSpacer()),
		nil,
		nil,
		nil,
		detailsText,
	)))
	detailsWindow.Show()
}

func (a *application) detailsText() string {
	status := a.server.Status()
	running := "no"
	if status.Running {
		running = "yes"
	}
	statusError := displayValue(status.Error, "None")
	defaultPrinterAddress := displayValue(a.cfg.DefaultPrinterAddress, "Not set")

	return strings.Join([]string{
		"Application",
		"  Name: " + config.AppDisplayName,
		"  App ID: " + config.AppID,
		"",
		"Listener",
		"  Running: " + running,
		"  Address: " + status.Address,
		"  HTTP port: " + strconv.Itoa(a.cfg.HTTPPort),
		"  Last listener error: " + statusError,
		"",
		"Printer Defaults",
		"  Default printer address: " + defaultPrinterAddress,
		"  Default printer port: " + strconv.Itoa(a.cfg.DefaultPrinterPort),
		"",
		"Allowed Origins",
		indentLines(a.cfg.AllowedOrigins, "  ", "  None configured"),
		"",
		"Files",
		"  Config file: " + a.cfgPath,
		"  Log file: " + a.logger.Path(),
		"",
		"API Endpoints",
		"  GET /ping",
		"  GET /status?host=<printer_host>&port=<printer_port>",
		"  POST /print",
		"",
		"Timeouts",
		"  TCP connect timeout: " + server.DefaultConnectTimeout.String(),
		"  TCP write timeout: " + server.DefaultWriteTimeout.String(),
		"",
		"Logging",
		"  Max log file size: 5 MB",
		"  Retained rotated log files: 3",
	}, "\n")
}

func (a *application) showHelp() {
	helpWindow := a.app.NewWindow("Help")
	helpWindow.Resize(fyne.NewSize(720, 560))

	helpText := a.helpText()
	helpContent := widget.NewRichTextFromMarkdown(helpText)
	helpContent.Wrapping = fyne.TextWrapWord

	copyButton := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		a.app.Clipboard().SetContent(helpText)
	})

	helpWindow.SetContent(container.NewPadded(container.NewBorder(
		container.NewHBox(copyButton, layout.NewSpacer()),
		nil,
		nil,
		nil,
		container.NewVScroll(helpContent),
	)))
	helpWindow.Show()
}

func (a *application) helpText() string {
	listener := a.listenerAddress()

	return strings.Join([]string{
		"# Help",
		"",
		"## If Nothing Prints",
		"- Make sure printer-bridge says it is listening.",
		"- Make sure the printer is powered on and connected to the network.",
		"- Check the printer IP address or hostname.",
		"- Check the printer port. Many network printers use port 9100, but your printer may use a different one.",
		"- Check whether VPN, Wi-Fi, or firewall settings are blocking this computer from reaching the printer.",
		"- Open View Logs after a failed print to see the latest result.",
		"",
		"## If The Website Cannot Connect",
		"- Check Allowed origins in Settings.",
		"- An allowed origin is the website address that may use printer-bridge.",
		"- This is different from the printer IP address.",
		"- Add the exact website address that is allowed to use printer-bridge.",
		"- Include the beginning, such as http:// or https://.",
		"- Include the port if the website uses one.",
		"- Do not include the page path after the address.",
		"",
		"Example: if the website is open at `http://localhost:3000/orders`, add `http://localhost:3000`.",
		"",
		"Only add websites you trust. If no origins are configured, websites cannot connect.",
		"",
		"## Check These Values",
		"- printer-bridge address: `http://" + listener + "`",
		"- Default printer address: `" + displayValue(a.cfg.DefaultPrinterAddress, "Not set") + "`",
		"- Default printer port: `" + strconv.Itoa(a.cfg.DefaultPrinterPort) + "`",
		"- Allowed origins: " + displayOriginsForHelp(a.cfg.AllowedOrigins),
		"",
		"## When Asking For Help",
		"- Open Details and use Copy when someone asks for technical information.",
		"- Open View Logs to see recent connection or printing errors.",
		"",
		"## Support And Feedback",
		"printer-bridge is built by [Jaime Bolaños](https://jbolanos.dev) as a practical tool for teams that need browser apps to reach local printers. If you need help setting it up, find something confusing, or have an idea that would make it better, I'll be glad to help.",
		"",
		"If printer-bridge saves you time, a GitHub star on the project or a follow is appreciated: [github.com/JamesBolanos](https://github.com/JamesBolanos)",
	}, "\n")
}

func (a *application) listenerAddress() string {
	status := a.server.Status()
	if status.Address != "" {
		return status.Address
	}
	return "127.0.0.1:" + strconv.Itoa(a.cfg.HTTPPort)
}

func settingsConfig(httpPortText string, printerPortText string, printerAddress string, originsText string) (config.Config, error) {
	httpPort, err := strconv.Atoi(strings.TrimSpace(httpPortText))
	if err != nil {
		return config.Config{}, fmt.Errorf("HTTP port must be a number")
	}
	printerPort, err := strconv.Atoi(strings.TrimSpace(printerPortText))
	if err != nil {
		return config.Config{}, fmt.Errorf("default printer port must be a number")
	}

	origins := splitOrigins(originsText)
	cfg := config.Config{
		HTTPPort:              httpPort,
		DefaultPrinterPort:    printerPort,
		DefaultPrinterAddress: strings.TrimSpace(printerAddress),
		AllowedOrigins:        origins,
	}

	return config.Normalize(cfg)
}

func splitOrigins(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ','
	})
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			origins = append(origins, part)
		}
	}
	return origins
}

func displayValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func displayOrigins(origins []string) string {
	if len(origins) == 0 {
		return "None configured"
	}
	return strings.Join(origins, ", ")
}

func displayOriginsSummary(origins []string) string {
	if len(origins) == 0 {
		return "None configured; add required web app origins in Settings"
	}
	return displayOrigins(origins)
}

func displayOriginsForHelp(origins []string) string {
	if len(origins) == 0 {
		return "`None configured`"
	}
	return "`" + strings.Join(origins, "`, `") + "`"
}

func indentLines(lines []string, prefix string, empty string) string {
	if len(lines) == 0 {
		return empty
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, prefix+line)
	}
	return strings.Join(out, "\n")
}

func (a *application) showLogs() {
	logsWindow := a.app.NewWindow("Logs")
	logsWindow.Resize(fyne.NewSize(760, 520))

	logText := widget.NewTextGrid()

	loadLogs := func() {
		lines, err := a.logger.ReadLinesNewestFirst(500)
		if err != nil {
			logText.SetText("Unable to read log file: " + err.Error())
			return
		}
		if len(lines) == 0 {
			logText.SetText("No log entries yet.")
			return
		}
		logText.SetText("Live log view - newest entries first\n\n" + strings.Join(lines, "\n"))
	}

	copyButton := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		a.app.Clipboard().SetContent(logText.Text())
	})
	revealButton := widget.NewButtonWithIcon("Reveal", theme.FolderOpenIcon(), func() {
		if err := revealFile(a.logger.Path()); err != nil {
			logText.SetText("Unable to reveal log file: " + err.Error())
		}
	})

	loadLogs()
	stopLogs := make(chan struct{})
	logsWindow.SetOnClosed(func() {
		close(stopLogs)
	})

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				fyne.Do(loadLogs)
			case <-stopLogs:
				return
			case <-a.stopRefresh:
				return
			}
		}
	}()

	logsWindow.SetContent(container.NewPadded(container.NewBorder(
		container.NewHBox(copyButton, revealButton, layout.NewSpacer()),
		nil,
		nil,
		nil,
		logText,
	)))
	logsWindow.Show()
}

func revealFile(path string) error {
	if path == "" {
		return fmt.Errorf("log file path is empty")
	}

	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,"+path).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}

func (a *application) quit() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = a.server.Stop(ctx)
	a.app.Quit()
}

func (a *application) runRefreshLoop() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				fyne.Do(func() {
					a.updateStatus()
				})
			case <-a.stopRefresh:
				return
			}
		}
	}()
}
