package main

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Styles ──────────────────────────────────────────────────────────────────

var (
	// Nord-inspired palette — looks great on dark terminals.
	clrFrost1  = lipgloss.Color("#8FBCBB")
	clrFrost2  = lipgloss.Color("#88C0D0")
	clrFrost3  = lipgloss.Color("#81A1C1")
	clrFrost4  = lipgloss.Color("#5E81AC")
	clrSnow1   = lipgloss.Color("#ECEFF4")
	clrSnow2   = lipgloss.Color("#E5E9F0")
	clrSnow3   = lipgloss.Color("#D8DEE9")
	clrPolar3  = lipgloss.Color("#4C566A")
	clrAurora1 = lipgloss.Color("#BF616A") // red
	clrAurora2 = lipgloss.Color("#D08770") // orange
	clrAurora3 = lipgloss.Color("#EBCB8B") // yellow
	clrAurora4 = lipgloss.Color("#A3BE8C") // green
	clrAurora6 = lipgloss.Color("#B48EAD") // purple

	titleBarStyle = lipgloss.NewStyle().
			Background(clrFrost4).
			Foreground(clrSnow1).
			Bold(true).
			Padding(0, 1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clrFrost3)

	sectionTitle = lipgloss.NewStyle().
			Foreground(clrFrost2).
			Bold(true)

	labelStyle = lipgloss.NewStyle().
			Foreground(clrSnow3)

	valueStyle = lipgloss.NewStyle().
			Foreground(clrSnow1)

	tempStyle = lipgloss.NewStyle().
			Foreground(clrAurora3).
			Bold(true)

	windStyle = lipgloss.NewStyle().
			Foreground(clrFrost1)

	humidStyle = lipgloss.NewStyle().
			Foreground(clrAurora4)

	pressStyle = lipgloss.NewStyle().
			Foreground(clrAurora6)

	visStyle = lipgloss.NewStyle().
			Foreground(clrFrost2)

	alertStyle = lipgloss.NewStyle().
			Background(clrAurora1).
			Foreground(clrSnow1).
			Bold(true).
			Padding(0, 1)

	alertWarnStyle = lipgloss.NewStyle().
			Background(clrAurora3).
			Foreground(lipgloss.Color("#2E3440")).
			Bold(true).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(clrPolar3)

	mutedStyle = lipgloss.NewStyle().
			Foreground(clrPolar3)

	errorStyle = lipgloss.NewStyle().
			Foreground(clrAurora1).
			Bold(true)
)

// minRefreshInterval is the shortest time allowed between manual meta-r refreshes.
// weather.gov has no published rate limit; 1 minute is a reasonable courtesy floor.
const minRefreshInterval = time.Minute

// ── Messages ─────────────────────────────────────────────────────────────────

type (
	tickMsg        time.Time
	locationMsg    LocationInfo
	gridPointMsg   GridPoint
	weatherMsg     WeatherData
	errMsg         struct{ err error }
)

func (e errMsg) Error() string { return e.err.Error() }

// ── Model ─────────────────────────────────────────────────────────────────────

type loadStage int

const (
	stageLocation loadStage = iota
	stageGridPoint
	stageWeather
	stageDone
	stageError
)

type model struct {
	cfg    Config
	client *Client

	stage   loadStage
	err     error
	loading string

	loc  LocationInfo
	gp   GridPoint
	data WeatherData

	width        int
	height       int
	hourlyOffset int // horizontal scroll offset for hourly forecast

	spinner spinner.Model
}

func newModel(cfg Config) model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(clrFrost2)

	return model{
		cfg:     cfg,
		client:  newClient(cfg.UserAgent),
		stage:   stageLocation,
		loading: "Looking up " + cfg.ZipCode + "…",
		spinner: sp,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.cmdFetchLocation(),
	)
}

// ── Commands ──────────────────────────────────────────────────────────────────

func (m model) cmdFetchLocation() tea.Cmd {
	zip := m.cfg.ZipCode
	client := m.client
	return func() tea.Msg {
		loc, err := client.LocationFromZip(zip)
		if err != nil {
			return errMsg{err}
		}
		return locationMsg(loc)
	}
}

func (m model) cmdFetchGridPoint() tea.Cmd {
	loc := m.loc
	client := m.client
	return func() tea.Msg {
		gp, err := client.GridPointFromLatLon(loc.Lat, loc.Lon)
		if err != nil {
			return errMsg{err}
		}
		return gridPointMsg(gp)
	}
}

func (m model) cmdFetchWeather() tea.Cmd {
	loc := m.loc
	gp := m.gp
	client := m.client
	return func() tea.Msg {
		data, err := client.FetchAll(loc, gp)
		if err != nil {
			return errMsg{err}
		}
		return weatherMsg(data)
	}
}

func (m model) cmdScheduleRefresh() tea.Cmd {
	d := time.Duration(m.cfg.RefreshMinutes) * time.Minute
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if m.stage == stageDone {
				m.stage = stageWeather
				m.loading = "Refreshing…"
				return m, tea.Batch(m.spinner.Tick, m.cmdFetchWeather())
			}
		case "alt+r":
			if m.stage == stageDone && time.Since(m.data.FetchedAt) >= minRefreshInterval {
				m.stage = stageWeather
				m.loading = "Refreshing…"
				return m, tea.Batch(m.spinner.Tick, m.cmdFetchWeather())
			}
		case "left", "h":
			if m.hourlyOffset > 0 {
				m.hourlyOffset--
			}
		case "right", "l":
			m.hourlyOffset++
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case locationMsg:
		m.loc = LocationInfo(msg)
		m.stage = stageGridPoint
		m.loading = "Finding NWS grid point…"
		return m, m.cmdFetchGridPoint()

	case gridPointMsg:
		m.gp = GridPoint(msg)
		m.stage = stageWeather
		m.loading = "Fetching weather data…"
		return m, m.cmdFetchWeather()

	case weatherMsg:
		m.data = WeatherData(msg)
		m.stage = stageDone
		return m, m.cmdScheduleRefresh()

	case tickMsg:
		m.stage = stageWeather
		m.loading = "Refreshing…"
		return m, tea.Batch(m.spinner.Tick, m.cmdFetchWeather())

	case errMsg:
		m.stage = stageError
		m.err = msg.err
	}

	return m, nil
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	switch m.stage {
	case stageError:
		return m.viewError()
	case stageDone:
		return m.viewReady()
	default:
		return m.viewLoading()
	}
}

func (m model) viewLoading() string {
	center := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Align(lipgloss.Center, lipgloss.Center)

	content := lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.NewStyle().Foreground(clrFrost2).Bold(true).Render("🌤 cli-weather"),
		"",
		m.spinner.View()+" "+mutedStyle.Render(m.loading),
	)
	return center.Render(content)
}

func (m model) viewError() string {
	center := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Align(lipgloss.Center, lipgloss.Center)

	content := lipgloss.JoinVertical(lipgloss.Center,
		errorStyle.Render("✗ Error"),
		"",
		valueStyle.Render(m.err.Error()),
		"",
		helpStyle.Render("[q] quit"),
	)
	return center.Render(content)
}

func (m model) viewReady() string {
	// Calculate layout dimensions.
	const helpLines = 1
	const statusLines = 1

	alertLines := 0
	if len(m.data.Alerts) > 0 {
		alertLines = 2
	}
	hourlyLines := 4
	zoneLines := 0
	if len(m.data.ZoneForecast) > 0 {
		zoneLines = 7
	}
	mainHeight := m.height - statusLines - hourlyLines - zoneLines - alertLines - helpLines
	if mainHeight < 4 {
		mainHeight = 4
	}

	var sections []string
	sections = append(sections, m.renderStatusBar())

	// Two-column layout at >= 110 cols, single column otherwise.
	if m.width >= 110 {
		leftWidth := 38
		rightWidth := m.width - leftWidth - 1 // -1 for join
		left := m.renderConditions(leftWidth, mainHeight)
		right := m.renderForecast(rightWidth, mainHeight)
		sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top, left, right))
	} else {
		half := mainHeight / 2
		sections = append(sections, m.renderConditions(m.width, half))
		sections = append(sections, m.renderForecast(m.width, mainHeight-half))
	}

	sections = append(sections, m.renderHourly(m.width, hourlyLines))

	if zoneLines > 0 {
		sections = append(sections, m.renderZoneForecast(m.width, zoneLines))
	}

	if alertLines > 0 {
		sections = append(sections, m.renderAlerts(m.width))
	}

	sections = append(sections, m.renderHelp())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// ── Panel renderers ───────────────────────────────────────────────────────────

func (m model) renderStatusBar() string {
	loc := m.data.Location
	gp := m.data.GridPoint

	city := gp.City
	if city == "" {
		city = loc.City
	}
	state := gp.State
	if state == "" {
		state = loc.State
	}

	locationStr := fmt.Sprintf("📍 %s, %s  %s", city, state, loc.ZipCode)

	var updatedStr string
	tz, err := time.LoadLocation(gp.TimeZone)
	if err != nil || gp.TimeZone == "" {
		tz = time.Local
	}
	updatedStr = "Updated: " + m.data.FetchedAt.In(tz).Format("3:04 PM")

	quit := "[q] quit"
	space := m.width - visibleLen(locationStr) - visibleLen(updatedStr) - visibleLen(quit) - 4
	if space < 1 {
		space = 1
	}
	bar := locationStr + strings.Repeat(" ", space) + updatedStr + "  " + quit
	return titleBarStyle.Width(m.width).Render(bar)
}

func (m model) renderConditions(width, height int) string {
	obs := m.data.Conditions
	units := m.cfg.Units
	tz, _ := time.LoadLocation(m.gp.TimeZone)
	if tz == nil {
		tz = time.Local
	}

	// Determine daytime from current hour in the location's timezone.
	hour := time.Now().In(tz).Hour()
	daytime := hour >= 6 && hour < 20

	condText := obs.TextDescription
	if condText == "" && len(m.data.Forecast) > 0 {
		condText = m.data.Forecast[0].ShortForecast
		daytime = m.data.Forecast[0].IsDaytime
	}

	icon := conditionIcon(condText, daytime)

	var lines []string
	lines = append(lines, sectionTitle.Render("CURRENT CONDITIONS"))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("%s  %s", icon, valueStyle.Render(condText)))
	lines = append(lines, "")

	// Temperature
	if obs.TempC != nil {
		var tempStr string
		if units == "imperial" {
			tempStr = fmt.Sprintf("%.0f°F", cToF(*obs.TempC))
		} else {
			tempStr = fmt.Sprintf("%.0f°C", *obs.TempC)
		}
		lines = append(lines, row("🌡", "Temperature", tempStyle.Render(tempStr)))
	} else if len(m.data.Forecast) > 0 {
		p := m.data.Forecast[0]
		tempStr := fmt.Sprintf("%d°%s", p.Temperature, p.TemperatureUnit)
		lines = append(lines, row("🌡", "Temperature", tempStyle.Render(tempStr)))
	}

	// Dewpoint
	if obs.DewpointC != nil {
		var dewStr string
		if units == "imperial" {
			dewStr = fmt.Sprintf("%.0f°F", cToF(*obs.DewpointC))
		} else {
			dewStr = fmt.Sprintf("%.0f°C", *obs.DewpointC)
		}
		lines = append(lines, row("💧", "Dewpoint", valueStyle.Render(dewStr)))
	}

	// Humidity
	if obs.RelHumidity != nil {
		humStr := fmt.Sprintf("%.0f%%", *obs.RelHumidity)
		lines = append(lines, row("💦", "Humidity", humidStyle.Render(humStr)))
	}

	// Wind
	if obs.WindSpeedKmh != nil {
		var speedStr string
		if units == "imperial" {
			speedStr = fmt.Sprintf("%.0f mph", kmhToMph(*obs.WindSpeedKmh))
		} else {
			speedStr = fmt.Sprintf("%.0f km/h", *obs.WindSpeedKmh)
		}
		dirStr := ""
		if obs.WindDirDeg != nil {
			dirStr = degToCompass(*obs.WindDirDeg) + " "
		}
		windStr := dirStr + speedStr
		if obs.WindGustKmh != nil && *obs.WindGustKmh > 0 {
			var gustStr string
			if units == "imperial" {
				gustStr = fmt.Sprintf("%.0f mph", kmhToMph(*obs.WindGustKmh))
			} else {
				gustStr = fmt.Sprintf("%.0f km/h", *obs.WindGustKmh)
			}
			windStr += " (gusts " + gustStr + ")"
		}
		lines = append(lines, row("💨", "Wind", windStyle.Render(windStr)))
	} else if len(m.data.Forecast) > 0 {
		p := m.data.Forecast[0]
		windStr := p.WindDirection + " " + p.WindSpeed
		lines = append(lines, row("💨", "Wind", windStyle.Render(windStr)))
	}

	// Pressure
	if obs.PressurePa != nil {
		var pressStr string
		if units == "imperial" {
			pressStr = fmt.Sprintf("%.2f inHg", paToInHg(*obs.PressurePa))
		} else {
			pressStr = fmt.Sprintf("%.0f hPa", paToHPa(*obs.PressurePa))
		}
		lines = append(lines, row("📊", "Pressure", pressStyle.Render(pressStr)))
	}

	// Visibility
	if obs.VisibilityM != nil {
		var visStr string
		if units == "imperial" {
			mi := mToMiles(*obs.VisibilityM)
			if mi >= 10 {
				visStr = fmt.Sprintf("%.0f mi", mi)
			} else {
				visStr = fmt.Sprintf("%.1f mi", mi)
			}
		} else {
			visStr = fmt.Sprintf("%.1f km", *obs.VisibilityM/1000)
		}
		lines = append(lines, row("👁", "Visibility", visStyle.Render(visStr)))
	}

	lines = append(lines, "")
	if obs.StationName != "" {
		lines = append(lines, mutedStyle.Render("Station: "+obs.StationName))
	}
	if !obs.Timestamp.IsZero() {
		lines = append(lines, mutedStyle.Render("As of: "+obs.Timestamp.In(tz).Format("3:04 PM")))
	}

	innerW := width - 4 // 2 for border, 2 for padding
	if innerW < 1 {
		innerW = 1
	}
	content := strings.Join(lines, "\n")
	return panelStyle.
		Width(width - 2).
		Height(height - 2).
		Padding(0, 1).
		Render(content)
}

func (m model) renderForecast(width, height int) string {
	var lines []string
	lines = append(lines, sectionTitle.Render("7-DAY FORECAST"))
	lines = append(lines, "")

	innerW := width - 6 // border + padding
	if innerW < 10 {
		innerW = 10
	}

	for i, p := range m.data.Forecast {
		if i >= height-4 {
			break
		}
		icon := conditionIcon(p.ShortForecast, p.IsDaytime)
		tempStr := tempStyle.Render(fmt.Sprintf("%3d°%s", p.Temperature, p.TemperatureUnit))
		name := p.Name
		if len(name) > 16 {
			name = name[:16]
		}
		wind := ""
		if p.WindSpeed != "" && p.WindDirection != "" {
			wind = mutedStyle.Render(" " + p.WindDirection + " " + p.WindSpeed)
		}
		cond := p.ShortForecast
		maxCond := innerW - 20
		if maxCond < 0 {
			maxCond = 0
		}
		if len(cond) > maxCond {
			cond = cond[:maxCond]
		}
		line := fmt.Sprintf("%-16s %s %s  %s%s",
			labelStyle.Render(name),
			icon,
			tempStr,
			valueStyle.Render(cond),
			wind,
		)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	return panelStyle.
		Width(width - 2).
		Height(height - 2).
		Padding(0, 1).
		Render(content)
}

func (m model) renderHourly(width, height int) string {
	var cells []string
	tz, _ := time.LoadLocation(m.gp.TimeZone)
	if tz == nil {
		tz = time.Local
	}

	for _, p := range m.data.Hourly {
		icon := conditionIcon(p.ShortForecast, p.IsDaytime)
		timeStr := p.StartTime.In(tz).Format("3PM")
		cell := fmt.Sprintf("%s %s %s",
			mutedStyle.Render(timeStr),
			icon,
			tempStyle.Render(fmt.Sprintf("%d°", p.Temperature)),
		)
		cells = append(cells, cell)
	}

	if len(cells) == 0 {
		return panelStyle.Width(width-2).Padding(0, 1).Render(sectionTitle.Render("HOURLY FORECAST"))
	}

	// Calculate how many cells fit in the width.
	cellWidth := 11 // approximate width of each cell + separator
	innerW := width - 4
	visible := innerW / cellWidth
	if visible < 1 {
		visible = 1
	}

	// Clamp offset.
	maxOffset := len(cells) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := m.hourlyOffset
	if offset > maxOffset {
		offset = maxOffset
	}

	end := offset + visible
	if end > len(cells) {
		end = len(cells)
	}

	var parts []string
	if offset > 0 {
		parts = append(parts, mutedStyle.Render("◀ "))
	} else {
		parts = append(parts, "  ")
	}
	parts = append(parts, strings.Join(cells[offset:end], "  "))
	if end < len(cells) {
		parts = append(parts, mutedStyle.Render(" ▶"))
	}

	content := sectionTitle.Render("HOURLY FORECAST") + "\n" + strings.Join(parts, "")
	return panelStyle.
		Width(width - 2).
		Padding(0, 1).
		Render(content)
}

func (m model) renderZoneForecast(width, height int) string {
	innerW := width - 6 // border + padding
	if innerW < 10 {
		innerW = 10
	}

	var lines []string
	lines = append(lines, sectionTitle.Render("ZONE FORECAST"))
	lines = append(lines, "")

	maxLines := height - 4 // leave room for title, blank line, border
	for _, p := range m.data.ZoneForecast {
		if len(lines) >= maxLines+2 {
			break
		}
		header := labelStyle.Render(p.Name+": ") + valueStyle.Render("")
		// Inline the period name with the first wrapped line of the forecast text.
		wrapped := wordWrap(p.DetailedForecast, innerW-len(p.Name)-2)
		if len(wrapped) == 0 {
			continue
		}
		lines = append(lines, header+valueStyle.Render(wrapped[0]))
		for _, wl := range wrapped[1:] {
			if len(lines) >= maxLines+2 {
				break
			}
			lines = append(lines, valueStyle.Render(strings.Repeat(" ", len(p.Name)+2)+wl))
		}
	}

	content := strings.Join(lines, "\n")
	return panelStyle.
		Width(width - 2).
		Padding(0, 1).
		Render(content)
}

func (m model) renderAlerts(width int) string {
	var parts []string
	for _, a := range m.data.Alerts {
		var style lipgloss.Style
		switch strings.ToLower(a.Severity) {
		case "extreme", "severe":
			style = alertStyle
		default:
			style = alertWarnStyle
		}
		headline := a.Headline
		if headline == "" {
			headline = a.Event
		}
		maxLen := width - 4
		if len(headline) > maxLen {
			headline = headline[:maxLen]
		}
		parts = append(parts, style.Render("⚠ "+headline))
	}
	return strings.Join(parts, "\n")
}

func (m model) renderHelp() string {
	keys := "[q] quit  [r] refresh  [⌥r] refresh (rate-limited)  [←/→] scroll hourly"
	return helpStyle.Width(m.width).Render(keys)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// row formats a labelled data row: icon, label (padded), value.
func row(icon, label, value string) string {
	return fmt.Sprintf("%s  %-11s %s", icon, labelStyle.Render(label), value)
}

// wordWrap breaks text into lines no longer than width characters.
func wordWrap(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	var lines []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len() == 0 {
			cur.WriteString(w)
		} else if cur.Len()+1+len(w) <= width {
			cur.WriteByte(' ')
			cur.WriteString(w)
		} else {
			lines = append(lines, cur.String())
			cur.Reset()
			cur.WriteString(w)
		}
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

// visibleLen returns the number of visible characters in a string,
// ignoring ANSI escape sequences.
func visibleLen(s string) int {
	// Strip ANSI sequences by counting only printable runes outside escape seqs.
	inEscape := false
	count := 0
	for _, r := range s {
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if utf8.ValidRune(r) {
			count++
		}
	}
	return count
}
