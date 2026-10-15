// Package desktopapp wires the Fyne UI, tray menu, config, logging, and server.
package desktopapp

import (
	"context"
	"fmt"
	"net"
	"net/url"
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

	"print-bridge/internal/activity"
	"print-bridge/internal/assets"
	"print-bridge/internal/config"
	"print-bridge/internal/logging"
	"print-bridge/internal/server"
)

const disclaimer = "print-bridge is an independent project, not affiliated with or endorsed by any printer manufacturer."

type application struct {
	app             fyne.App
	window          fyne.Window
	cfgPath         string
	cfg             config.Config
	server          *server.Server
	logger          *logging.Logger
	openURL         func(*url.URL) error
	statusIcon      *widget.Icon
	statusLabel     *widget.Label
	infoLabel       *widget.Label
	messageLabel    *widget.Label
	closeNotice     bool
	stopUpdates     chan struct{}
	recoveryMessage string
}

// Run starts the desktop app, local HTTP listener, tray integration, and UI loop.
func Run() error {
	cfgPath, err := config.DefaultConfigPath()
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
	defer func() {
		if err := bridgeServer.Close(); err != nil {
			logger.Record(activity.Entry{
				Timestamp: time.Now(), Endpoint: "shutdown", Outcome: "error",
				Detail: "server shutdown failed: " + err.Error(),
			})
		}
	}()
	ui := &application{
		app:         fyneApp,
		cfgPath:     cfgPath,
		cfg:         cfg,
		server:      bridgeServer,
		logger:      logger,
		stopUpdates: make(chan struct{}),
		openURL:     fyneApp.OpenURL,
	}
	if loadResult.RecoveryRequired {
		ui.recoveryMessage = loadResult.Warning
	}

	ui.window = fyneApp.NewWindow(config.AppDisplayName)
	ui.window.Resize(fyne.NewSize(840, 390))
	ui.window.SetContent(ui.mainContent())

	if err := bridgeServer.Start(); err != nil {
		if ui.recoveryMessage != "" {
			ui.setMessage(ui.recoveryMessage)
		} else {
			ui.setMessage("Listener failed to start: " + err.Error())
		}
	} else {
		ui.setMessage("Listening on " + bridgeServer.Status().Address)
	}
	ui.updateStatus()
	ui.setupTray()
	ui.runStatusLoop()

	ui.window.ShowAndRun()
	close(ui.stopUpdates)
	return nil
}

func (a *application) mainContent() fyne.CanvasObject {
	title := widget.NewLabelWithStyle(config.AppDisplayName, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	subtitle := widget.NewLabel("Local browser printing bridge")
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
	inspectorButton := widget.NewButtonWithIcon("Open Inspector", theme.VisibilityIcon(), a.openInspector)
	detailsButton := widget.NewButtonWithIcon("Details", theme.InfoIcon(), a.showDetails)
	helpButton := widget.NewButtonWithIcon("Help", theme.HelpIcon(), a.showHelp)
	logsButton := widget.NewButtonWithIcon("View Logs", theme.DocumentIcon(), a.showLogs)
	quitButton := widget.NewButtonWithIcon("Quit", theme.CancelIcon(), a.quit)
	actions := container.NewHBox(settingsButton, inspectorButton, detailsButton, helpButton, logsButton, layout.NewSpacer(), quitButton)

	content := container.NewVBox(heading, statusRow, a.infoLabel, a.messageLabel, disclaimerLabel, actions)

	a.window.SetCloseIntercept(func() {
		if !a.closeNotice {
			a.closeNotice = true
			a.app.SendNotification(&fyne.Notification{
				Title:   "print-bridge is still running",
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
		fyne.NewMenuItem("Open print-bridge", func() {
			a.window.Show()
		}),
		fyne.NewMenuItem("Details", a.showDetails),
		fyne.NewMenuItem("Help", a.showHelp),
		fyne.NewMenuItem("Open Inspector", a.openInspector),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(statusText, nil),
		fyne.NewMenuItem("Destination: "+destinationLabel(a.cfg.DestinationType), nil),
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

	a.infoLabel.SetText(fmt.Sprintf(
		"Destination: %s\nDefault printer port: %d\nDefault printer address: %s\nAllowed origins: %s",
		destinationLabel(a.cfg.DestinationType),
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
	settingsWindow.Resize(fyne.NewSize(620, 720))

	destinationSelect := widget.NewSelect([]string{"Physical", "Virtual (capture only)"}, nil)
	destinationSelect.SetSelected(destinationLabel(a.cfg.DestinationType))

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

	labelaryCheck := widget.NewCheck("Enable Labelary previews", nil)
	labelaryCheck.SetChecked(a.cfg.Preview.LabelaryEnabled)
	labelWidthEntry := widget.NewEntry()
	labelWidthEntry.SetText(strconv.FormatFloat(a.cfg.Preview.LabelWidthInches, 'f', -1, 64))
	labelHeightEntry := widget.NewEntry()
	labelHeightEntry.SetText(strconv.FormatFloat(a.cfg.Preview.LabelHeightInches, 'f', -1, 64))
	densitySelect := widget.NewSelect([]string{"6", "8", "12", "24"}, nil)
	densitySelect.SetSelected(strconv.Itoa(a.cfg.Preview.PrintDensityDpmm))
	previewDisclosure := widget.NewLabel("When enabled, complete label contents are sent over HTTPS to Labelary for rendering. Previews are optional; printing and capture do not depend on this service.")
	previewDisclosure.Wrapping = fyne.TextWrapWord

	errorLabel := widget.NewLabel("")
	errorLabel.Wrapping = fyne.TextWrapWord

	form := widget.NewForm(
		settingsFormItem(
			"Destination",
			destinationSelect,
			"Physical sends print jobs to the requested network printer. Virtual captures all jobs locally and never sends them to a printer.",
		),
		settingsFormItem(
			"HTTP port",
			httpPortEntry,
			"Local port used by web apps to reach print-bridge. Use 8080 unless another app is already using it.",
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
			"Website addresses allowed to use print-bridge from a browser. One per line, no page path.",
		),
		settingsFormItem("Optional Labelary rendering", container.NewVBox(labelaryCheck, previewDisclosure),
			"Rendering is off by default, works only in Virtual mode, and shares job contents with the Labelary service when enabled."),
		settingsFormItem("Label width (inches)", labelWidthEntry, "Preview label width from greater than 0 through 15 inches."),
		settingsFormItem("Label height (inches)", labelHeightEntry, "Preview label height from greater than 0 through 15 inches."),
		settingsFormItem("Print density (dpmm)", densitySelect, "Labelary density in dots per millimeter: 6, 8, 12, or 24."),
	)

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		if a.recoveryMessage != "" {
			errorLabel.SetText(a.recoveryMessage)
			return
		}
		nextCfg, err := settingsConfig(
			a.cfg, httpPortEntry.Text, printerPortEntry.Text, printerAddressEntry.Text,
			originsEntry.Text, destinationSelect.Selected, labelaryCheck.Checked,
			labelWidthEntry.Text, labelHeightEntry.Text, densitySelect.Selected,
		)
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
	if a.recoveryMessage != "" {
		errorLabel.SetText(a.recoveryMessage)
		saveButton.Disable()
	}

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

	detailsWindow.SetContent(container.NewPadded(detailsText))
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
		"  Destination: " + destinationLabel(a.cfg.DestinationType),
		"",
		"Preview",
		"  Labelary enabled: " + strconv.FormatBool(a.cfg.Preview.LabelaryEnabled),
		"  Label size: " + strconv.FormatFloat(a.cfg.Preview.LabelWidthInches, 'f', -1, 64) + " x " + strconv.FormatFloat(a.cfg.Preview.LabelHeightInches, 'f', -1, 64) + " inches",
		"  Print density: " + strconv.Itoa(a.cfg.Preview.PrintDensityDpmm) + " dpmm",
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

	helpContent := widget.NewRichTextFromMarkdown(a.helpText())
	helpContent.Wrapping = fyne.TextWrapWord

	helpWindow.SetContent(container.NewPadded(container.NewVScroll(helpContent)))
	helpWindow.Show()
}

func (a *application) helpText() string {
	listener := a.listenerAddress()

	return strings.Join([]string{
		"# Help",
		"",
		"## If Nothing Prints",
		"- Make sure print-bridge says it is listening.",
		"- Make sure the printer is powered on and connected to the network.",
		"- Check the printer IP address or hostname.",
		"- Check the printer port. Many network printers use port 9100, but your printer may use a different one.",
		"- Check whether VPN, Wi-Fi, or firewall settings are blocking this computer from reaching the printer.",
		"- Open View Logs after a failed print to see the latest result.",
		"",
		"## If The Website Cannot Connect",
		"- Check Allowed origins in Settings.",
		"- An allowed origin is the website address that may use print-bridge.",
		"- This is different from the printer IP address.",
		"- Add the exact website address that is allowed to use print-bridge.",
		"- Include the beginning, such as http:// or https://.",
		"- Include the port if the website uses one.",
		"- Do not include the page path after the address.",
		"",
		"Example: if the website is open at `http://localhost:3000/orders`, add `http://localhost:3000`.",
		"",
		"Only add websites you trust. If no origins are configured, websites cannot connect.",
		"",
		"## Check These Values",
		"- print-bridge address: `http://" + listener + "`",
		"- Destination: `" + destinationLabel(a.cfg.DestinationType) + "`",
		"- In Virtual mode, jobs are captured for inspection and are not sent to a physical printer.",
		"- Labelary previews are optional and share label contents with an external service when enabled.",
		"- Capture and inspection work offline; previews need an internet connection and do not guarantee physical printer fidelity.",
		"- Virtual job history is temporary and is cleared when print-bridge quits.",
		"- Default printer address: `" + displayValue(a.cfg.DefaultPrinterAddress, "Not set") + "`",
		"- Default printer port: `" + strconv.Itoa(a.cfg.DefaultPrinterPort) + "`",
		"- Allowed origins: " + displayOriginsForHelp(a.cfg.AllowedOrigins),
		"",
		"## When Asking For Help",
		"- Open Details when someone asks for technical information.",
		"- Open View Logs to see recent connection or printing errors.",
		"",
		"## Support And Feedback",
		"print-bridge is built by [Jaime Bolaños](https://jbolanos.dev) as a practical tool for teams that need browser apps to reach local printers. If you need help setting it up, find something confusing, or have an idea that would make it better, I'll be glad to help.",
		"",
		"If print-bridge saves you time, a GitHub star on the project or a follow is appreciated: [github.com/JamesBolanos](https://github.com/JamesBolanos)",
	}, "\n")
}

func (a *application) listenerAddress() string {
	status := a.server.Status()
	if status.Address != "" {
		return status.Address
	}
	return "127.0.0.1:" + strconv.Itoa(a.cfg.HTTPPort)
}

func (a *application) openInspector() {
	if err := launchInspector(a.cfg.HTTPPort, a.openURL); err != nil {
		message := "Could not open the inspector: " + err.Error()
		a.setMessage(message)
		a.app.SendNotification(&fyne.Notification{Title: "Unable to open inspector", Content: err.Error()})
		a.window.Show()
		return
	}
	a.setMessage("Opened inspector at " + inspectorAddress(a.cfg.HTTPPort))
}

// launchInspector opens the local dashboard through the system browser.
func launchInspector(port int, openURL func(*url.URL) error) error {
	if openURL == nil {
		return fmt.Errorf("system browser is unavailable")
	}
	target, err := inspectorURL(port)
	if err != nil {
		return err
	}
	if err := openURL(target); err != nil {
		return fmt.Errorf("system browser could not open %s: %w", target, err)
	}
	return nil
}

func inspectorURL(port int) (*url.URL, error) {
	if port < 1024 || port > 65535 {
		return nil, fmt.Errorf("inspector port is outside the valid range")
	}
	return url.Parse("http://" + inspectorAddress(port) + "/inspector/")
}

func inspectorAddress(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// settingsConfig applies every setting exposed by the desktop form. Recovery
// configs stay invalid, preventing an ordinary Save from choosing Physical.
func settingsConfig(
	base config.Config,
	httpPortText string,
	printerPortText string,
	printerAddress string,
	originsText string,
	destination string,
	labelaryEnabled bool,
	labelWidthText string,
	labelHeightText string,
	densityText string,
) (config.Config, error) {
	if _, err := config.Normalize(base); err != nil {
		return config.Config{}, err
	}
	httpPort, err := strconv.Atoi(strings.TrimSpace(httpPortText))
	if err != nil {
		return config.Config{}, fmt.Errorf("HTTP port must be a number")
	}
	printerPort, err := strconv.Atoi(strings.TrimSpace(printerPortText))
	if err != nil {
		return config.Config{}, fmt.Errorf("default printer port must be a number")
	}
	destinationType, err := destinationTypeFromLabel(destination)
	if err != nil {
		return config.Config{}, err
	}
	labelWidth, err := strconv.ParseFloat(strings.TrimSpace(labelWidthText), 64)
	if err != nil {
		return config.Config{}, fmt.Errorf("label width must be a number")
	}
	labelHeight, err := strconv.ParseFloat(strings.TrimSpace(labelHeightText), 64)
	if err != nil {
		return config.Config{}, fmt.Errorf("label height must be a number")
	}
	density, err := strconv.Atoi(strings.TrimSpace(densityText))
	if err != nil {
		return config.Config{}, fmt.Errorf("print density must be 6, 8, 12, or 24")
	}

	base.HTTPPort = httpPort
	base.DefaultPrinterPort = printerPort
	base.DefaultPrinterAddress = strings.TrimSpace(printerAddress)
	base.AllowedOrigins = splitOrigins(originsText)
	base.DestinationType = destinationType
	base.Preview = config.PreviewConfig{
		LabelaryEnabled: labelaryEnabled, LabelWidthInches: labelWidth,
		LabelHeightInches: labelHeight, PrintDensityDpmm: density,
	}
	return config.Normalize(base)
}

func destinationLabel(destination string) string {
	switch destination {
	case config.DestinationVirtual:
		return "Virtual (capture only)"
	case config.DestinationPhysical:
		return "Physical"
	default:
		return "Unavailable (repair required)"
	}
}

func destinationTypeFromLabel(label string) (string, error) {
	switch label {
	case "Physical":
		return config.DestinationPhysical, nil
	case "Virtual (capture only)":
		return config.DestinationVirtual, nil
	default:
		return "", fmt.Errorf("select a destination")
	}
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
			case <-a.stopUpdates:
				return
			}
		}
	}()

	logsWindow.SetContent(container.NewPadded(logText))
	logsWindow.Show()
}

func (a *application) quit() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = a.server.Stop(ctx)
	a.app.Quit()
}

func (a *application) runStatusLoop() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				fyne.Do(func() {
					a.updateStatus()
				})
			case <-a.stopUpdates:
				return
			}
		}
	}()
}
