package tui

import (
	"fmt"
	"strings"
	"time"

	"cpuguard/internal/api"
	"cpuguard/internal/model"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	eventLimit             = 100
	defaultRefreshInterval = time.Second
	minRefreshInterval     = time.Second
	maxRefreshInterval     = 30 * time.Second
)

type focusArea int

const (
	focusSubjects focusArea = iota
	focusEvents
)

type actionKind string

const (
	actionNone       actionKind = ""
	actionUnthrottle actionKind = "unthrottle"
	actionHold       actionKind = "hold"
)

type dataMsg struct {
	subjects    []model.Subject
	subjectPage model.SubjectPage
	events      []model.Event
	system      model.SystemMetrics
	summary     model.SubjectSummary
	subjectID   string
	at          time.Time
	err         error

	updateSubjects bool
	updateStatus   bool
	updateEvents   bool
	recordHistory  bool
}

type actionMsg struct {
	action actionKind
	err    error
}

type tickMsg time.Time

type Model struct {
	client *api.Client

	subjects []model.Subject
	events   []model.Event
	system   model.SystemMetrics
	summary  model.SubjectSummary

	subjectTable dataTable
	eventTable   dataTable

	focus   focusArea
	width   int
	height  int
	loading bool

	subjectTop    int
	subjectBottom int
	eventTop      int
	eventBottom   int

	refreshInterval      time.Duration
	subjectSortKey       subjectSortKey
	subjectSortDir       sortDirection
	eventSortKey         eventSortKey
	eventSortDir         sortDirection
	subjectDeselected    bool
	subjectPageOffset    int
	selectedSubjectRef   string
	selectedSubjectValue *model.Subject

	systemCPUHistory []float64
	throttledHistory []int

	lastRefresh time.Time
	lastError   string
	notice      string

	pendingAction  actionKind
	pendingSubject *model.Subject
}

func Run(socketPath string) error {
	p := tea.NewProgram(New(api.NewClient(socketPath)), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func New(client *api.Client) Model {
	subjectTable := newDataTable(subjectColumnsForSort(100, subjectSortCPU, sortDesc), 10, true)
	eventTable := newDataTable(eventColumnsForSort(100, eventSortTime, sortDesc), 8, false)
	return Model{
		client:            client,
		subjectTable:      subjectTable,
		eventTable:        eventTable,
		focus:             focusSubjects,
		refreshInterval:   defaultRefreshInterval,
		subjectSortKey:    subjectSortCPU,
		subjectSortDir:    sortDesc,
		eventSortKey:      eventSortTime,
		eventSortDir:      sortDesc,
		subjectDeselected: true,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadDataCmd(), tick(m.refreshInterval))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeTables()
		return m, nil
	case tickMsg:
		if m.loading {
			return m, tick(m.refreshInterval)
		}
		m.loading = true
		return m, tea.Batch(m.loadDataCmd(), tick(m.refreshInterval))
	case dataMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			return m, nil
		}
		m.lastError = ""
		updateSubjects := msg.updateSubjects || msg.subjects != nil || msg.subjectPage.Total > 0 || msg.subjectPage.Limit > 0 || len(msg.subjectPage.Items) > 0
		updateStatus := msg.updateStatus || msg.summary.Total > 0 || !msg.system.SampledAt.IsZero()
		updateEvents := msg.updateEvents || msg.events != nil
		if updateStatus {
			m.lastRefresh = msg.at
		}
		previousSubjectID := m.selectedSubjectID()
		selectedSubjectID := msg.subjectID
		if m.subjectDeselected {
			selectedSubjectID = ""
		}
		if selectedSubjectID == "" && !m.subjectDeselected {
			selectedSubjectID = previousSubjectID
		}
		eventsMatchSelection := selectedSubjectID == msg.subjectID
		page := msg.subjectPage
		if updateSubjects {
			if page.Limit == 0 && msg.subjects != nil {
				subjects := sortSubjectsBy(msg.subjects, m.subjectSortKey, m.subjectSortDir)
				page = model.SubjectPage{
					Total:         len(subjects),
					Offset:        0,
					Limit:         len(subjects),
					Items:         subjects,
					SelectedIndex: -1,
				}
				for i := range subjects {
					if subjects[i].ID == msg.subjectID {
						page.Selected = &subjects[i]
						page.SelectedIndex = i
						break
					}
				}
			}
			m.subjects = page.Items
			m.subjectPageOffset = page.Offset
			m.refreshSubjectRows(page.Total)
		}
		if updateStatus {
			m.summary = msg.summary
			m.system = msg.system
			if msg.recordHistory {
				m.recordHistory()
			}
		} else if msg.summary.Total == 0 && msg.subjects != nil && len(page.Items) > 0 {
			m.summary = summarizeSubjects(page.Items)
		}
		if updateEvents && eventsMatchSelection {
			m.events = sortEventsBy(msg.events, m.eventSortKey, m.eventSortDir)
		} else if updateEvents {
			m.events = nil
		}
		if updateSubjects {
			if selectedSubjectID != "" {
				m.selectedSubjectRef = selectedSubjectID
				m.selectedSubjectValue = page.Selected
				if m.selectedSubjectValue == nil {
					m.selectedSubjectValue = m.subjectInCurrentPage(selectedSubjectID)
				}
				if page.SelectedIndex >= 0 {
					m.subjectTable.SetCursorPreserveOffset(page.SelectedIndex)
				}
				m.subjectDeselected = false
			} else if m.subjectDeselected {
				m.selectedSubjectRef = ""
				m.selectedSubjectValue = nil
				m.subjectTable.SetCursor(-1)
			}
		}
		if updateEvents {
			m.refreshEventRows()
		}
		if updateEvents && !eventsMatchSelection {
			m.loading = true
			return m, m.loadEventsCmd(selectedSubjectID)
		}
		return m, nil
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case actionMsg:
		if msg.err != nil {
			m.notice = fmt.Sprintf("%s failed: %v", msg.action, msg.err)
		} else {
			m.notice = fmt.Sprintf("%s completed", msg.action)
		}
		m.pendingAction = actionNone
		m.pendingSubject = nil
		m.loading = true
		return m, m.loadDataCmd()
	case tea.KeyMsg:
		if m.pendingAction != actionNone {
			return m.handleConfirm(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.toggleFocus()
			return m, nil
		case "r":
			m.loading = true
			m.notice = "refreshing"
			return m, m.loadDataCmd()
		case "[":
			m.decreaseRefreshInterval()
			return m, nil
		case "]":
			m.increaseRefreshInterval()
			return m, nil
		case "u":
			return m.prepareAction(actionUnthrottle)
		case "h":
			return m.prepareAction(actionHold)
		}
	}

	var cmd tea.Cmd
	previousSubjectID := m.selectedSubjectID()
	previousCursor := m.subjectTable.Cursor()
	previousOffset := m.subjectTable.offset
	if m.focus == focusSubjects {
		m.subjectTable, cmd = m.subjectTable.Update(msg)
	} else {
		m.eventTable, cmd = m.eventTable.Update(msg)
	}
	if m.focus == focusSubjects && m.subjectTable.Cursor() != previousCursor {
		if m.subjectTable.Cursor() < 0 {
			m.subjectDeselected = true
			m.selectedSubjectRef = ""
			m.selectedSubjectValue = nil
		} else if subject := m.subjectAtGlobalIndex(m.subjectTable.Cursor()); subject != nil {
			m.subjectDeselected = false
			m.selectedSubjectRef = subject.ID
			subjectCopy := *subject
			m.selectedSubjectValue = &subjectCopy
		}
	}
	if m.focus == focusSubjects && m.selectedSubjectID() != previousSubjectID {
		m.subjectDeselected = m.selectedSubjectID() == ""
		m.loading = true
		return m, tea.Batch(cmd, m.loadEventsCmd(m.selectedSubjectID()))
	}
	if m.focus == focusSubjects && m.subjectTable.offset != previousOffset && !m.hasCompleteSubjectPage() {
		m.loading = true
		return m, tea.Batch(cmd, m.loadSubjectPageCmd())
	}
	return m, cmd
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading cpuguard tui..."
	}
	parts := []string{
		m.header(),
		m.metrics(),
		m.trend(),
		m.panel("SUBJECTS", m.subjectTableView(), m.focus == focusSubjects),
		m.panel(m.eventsTitle(), m.eventTableView(), m.focus == focusEvents),
		m.footer(),
	}
	if m.pendingAction != actionNone && m.pendingSubject != nil {
		parts = append(parts, m.confirmation())
	}
	return clipView(strings.Join(parts, "\n"), m.width, m.height)
}

func (m *Model) resizeTables() {
	if m.width < 40 {
		m.width = 40
	}
	bodyHeight := m.height - 10
	if bodyHeight < 8 {
		bodyHeight = 8
	}
	panelWidth := max(20, m.width-2)
	tableWidth := max(10, panelWidth-panelStyle.GetHorizontalFrameSize())
	subjectHeight := bodyHeight / 2
	eventHeight := bodyHeight - subjectHeight
	m.subjectTable.SetWidth(tableWidth)
	m.eventTable.SetWidth(tableWidth)
	m.subjectTable.SetHeight(subjectHeight)
	m.eventTable.SetHeight(eventHeight)
	m.updateTableColumns()

	m.subjectTop = 3
	subjectPanelHeight := subjectHeight + 3
	eventPanelHeight := eventHeight + 3
	m.subjectBottom = m.subjectTop + subjectPanelHeight - 1
	m.eventTop = m.subjectTop + subjectPanelHeight
	m.eventBottom = m.eventTop + eventPanelHeight - 1
}

func (m *Model) toggleFocus() {
	if m.focus == focusSubjects {
		m.focus = focusEvents
		m.subjectTable.Blur()
		m.eventTable.Focus()
		return
	}
	m.focus = focusSubjects
	m.eventTable.Blur()
	m.subjectTable.Focus()
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	ev := tea.MouseEvent(msg)
	if ev.IsWheel() {
		return m.handleWheel(ev)
	}
	if ev.Action != tea.MouseActionPress || ev.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if ev.Y == 0 {
		return m.handleHeaderClick(ev)
	}
	if ev.Y >= m.subjectTop && ev.Y <= m.subjectBottom {
		if m.focus != focusSubjects {
			m.toggleFocus()
		}
		if ev.Y == m.subjectTop+2 {
			return m.handleSubjectHeaderClick(ev)
		}
		previousSubjectID := m.selectedSubjectID()
		previousCursor := m.subjectTable.Cursor()
		m.selectRowFromMouse(focusSubjects, ev.Y-m.subjectTop-3)
		if m.selectedSubjectID() == previousSubjectID && m.subjectTable.Cursor() == previousCursor {
			return m, nil
		}
		if m.selectedSubjectID() == "" {
			m.loading = true
			return m, m.loadEventsCmd("")
		}
		m.loading = true
		return m, m.loadEventsCmd(m.selectedSubjectID())
	}
	if ev.Y >= m.eventTop && ev.Y <= m.eventBottom {
		if m.focus != focusEvents {
			m.toggleFocus()
		}
		if ev.Y == m.eventTop+2 {
			return m.handleEventHeaderClick(ev)
		}
		m.selectRowFromMouse(focusEvents, ev.Y-m.eventTop-3)
		return m, nil
	}
	return m, nil
}

func (m Model) handleHeaderClick(ev tea.MouseEvent) (tea.Model, tea.Cmd) {
	minusStart, minusEnd, plusStart, plusEnd := m.refreshControlBounds()
	switch {
	case ev.X >= minusStart && ev.X <= minusEnd:
		m.decreaseRefreshInterval()
	case ev.X >= plusStart && ev.X <= plusEnd:
		m.increaseRefreshInterval()
	}
	return m, nil
}

func (m Model) handleSubjectHeaderClick(ev tea.MouseEvent) (tea.Model, tea.Cmd) {
	key, ok := subjectSortKeyAtX(m.subjectTable.Columns(), ev.X-1)
	if !ok {
		return m, nil
	}
	selectedID := m.selectedSubjectID()
	m.setSubjectSort(key)
	m.updateTableColumns()
	m.subjectTable.ScrollToTop()
	m.loading = true
	return m, loadSubjectPageForViewport(m.client, selectedID, m.subjectTable.offset, m.subjectPageLimit(), string(m.subjectSortKey), sortDirectionString(m.subjectSortDir))
}

func (m Model) handleEventHeaderClick(ev tea.MouseEvent) (tea.Model, tea.Cmd) {
	key, ok := eventSortKeyAtX(m.eventTable.Columns(), ev.X-1)
	if !ok {
		return m, nil
	}
	m.setEventSort(key)
	m.updateTableColumns()
	m.events = sortEventsBy(m.events, m.eventSortKey, m.eventSortDir)
	m.refreshEventRows()
	return m, nil
}

func (m Model) handleWheel(ev tea.MouseEvent) (tea.Model, tea.Cmd) {
	if ev.Y >= m.subjectTop && ev.Y <= m.subjectBottom {
		previousSubjectID := m.selectedSubjectID()
		previousOffset := m.subjectTable.offset
		if ev.Button == tea.MouseButtonWheelUp {
			m.subjectTable.ScrollUp(1)
		}
		if ev.Button == tea.MouseButtonWheelDown {
			m.subjectTable.ScrollDown(1)
		}
		if m.selectedSubjectID() != previousSubjectID {
			m.subjectDeselected = m.selectedSubjectID() == ""
			m.loading = true
			return m, m.loadEventsCmd(m.selectedSubjectID())
		}
		if m.subjectTable.offset != previousOffset && !m.hasCompleteSubjectPage() {
			m.loading = true
			return m, m.loadSubjectPageCmd()
		}
		return m, nil
	}
	if ev.Y >= m.eventTop && ev.Y <= m.eventBottom {
		if ev.Button == tea.MouseButtonWheelUp {
			m.eventTable.ScrollUp(1)
		}
		if ev.Button == tea.MouseButtonWheelDown {
			m.eventTable.ScrollDown(1)
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) selectRowFromMouse(area focusArea, row int) {
	if row < 0 {
		return
	}
	if area == focusSubjects {
		idx := m.subjectTable.offset + row
		if idx < 0 || idx >= len(m.subjects) {
			return
		}
		if m.subjectTable.Cursor() == idx {
			m.subjectTable.SetCursor(-1)
			m.subjectDeselected = true
			m.selectedSubjectRef = ""
			m.selectedSubjectValue = nil
			m.events = nil
			m.refreshEventRows()
			return
		}
		subject := m.subjectAtGlobalIndex(idx)
		if subject == nil {
			return
		}
		m.subjectTable.SetCursor(idx)
		m.subjectDeselected = false
		m.selectedSubjectRef = subject.ID
		subjectCopy := *subject
		m.selectedSubjectValue = &subjectCopy
	}
	if area == focusEvents {
		idx := m.eventTable.offset + row
		if idx < 0 || idx >= len(m.events) {
			return
		}
		if m.eventTable.Cursor() == idx {
			m.eventTable.SetCursor(-1)
			return
		}
		m.eventTable.SetCursor(idx)
	}
}

func (m Model) prepareAction(action actionKind) (tea.Model, tea.Cmd) {
	subject, ok := m.selectedSubject()
	if !ok {
		m.notice = "no subject selected"
		return m, nil
	}
	if subject.State != model.StateThrottled && subject.State != model.StateManualHold {
		m.notice = "selected subject is not throttled"
		return m, nil
	}
	if action == actionHold && subject.State != model.StateThrottled {
		m.notice = "hold requires a throttled subject"
		return m, nil
	}
	m.pendingAction = action
	subjectCopy := subject
	m.pendingSubject = &subjectCopy
	return m, nil
}

func (m Model) handleConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		subjectID := m.pendingSubject.ID
		action := m.pendingAction
		return m, runAction(m.client, action, subjectID)
	case "n", "esc":
		m.notice = "action cancelled"
		m.pendingAction = actionNone
		m.pendingSubject = nil
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) selectedSubject() (model.Subject, bool) {
	if m.selectedSubjectRef == "" {
		return model.Subject{}, false
	}
	if m.selectedSubjectValue != nil && m.selectedSubjectValue.ID == m.selectedSubjectRef {
		return *m.selectedSubjectValue, true
	}
	if subject := m.subjectAtGlobalIndex(m.subjectTable.Cursor()); subject != nil {
		return *subject, true
	}
	return model.Subject{ID: m.selectedSubjectRef, TargetRef: m.selectedSubjectRef}, true
}

func (m Model) selectedSubjectID() string {
	return m.selectedSubjectRef
}

func (m *Model) refreshSubjectRows(total int) {
	subjects := m.subjects
	offset := m.subjectPageOffset
	m.subjectTable.SetVirtualRows(total, func(index int) table.Row {
		if index >= offset && index < offset+len(subjects) {
			return subjectRow(subjects[index-offset])
		}
		return loadingSubjectRow()
	})
}

func (m *Model) refreshEventRows() {
	events := m.events
	m.eventTable.SetVirtualRows(len(events), func(index int) table.Row {
		return eventRow(events[index])
	})
}

func (m *Model) restoreSubjectCursor(subjectID string) {
	for i, subject := range m.subjects {
		if subject.ID == subjectID {
			m.subjectTable.SetCursorPreserveOffset(i)
			return
		}
	}
	m.subjectTable.SetCursor(0)
}

func (m Model) subjectAtGlobalIndex(index int) *model.Subject {
	local := index - m.subjectPageOffset
	if local < 0 || local >= len(m.subjects) {
		return nil
	}
	return &m.subjects[local]
}

func (m Model) subjectInCurrentPage(subjectID string) *model.Subject {
	for i := range m.subjects {
		if m.subjects[i].ID == subjectID {
			return &m.subjects[i]
		}
	}
	return nil
}

func (m Model) subjectPageLimit() int {
	limit := m.subjectTable.visibleRows() * 3
	if limit < 100 {
		return 100
	}
	return limit
}

func (m Model) hasCompleteSubjectPage() bool {
	return m.subjectPageOffset == 0 && len(m.subjects) == m.subjectTable.rowCount
}

func sortDirectionString(direction sortDirection) string {
	if direction == sortAsc {
		return "asc"
	}
	return "desc"
}

func (m *Model) setSubjectSort(key subjectSortKey) {
	if m.subjectSortKey == key {
		m.subjectSortDir = toggleSortDirection(m.subjectSortDir)
		return
	}
	m.subjectSortKey = key
	m.subjectSortDir = defaultSubjectSortDirection(key)
}

func (m *Model) setEventSort(key eventSortKey) {
	if m.eventSortKey == key {
		m.eventSortDir = toggleSortDirection(m.eventSortDir)
		return
	}
	m.eventSortKey = key
	m.eventSortDir = defaultEventSortDirection(key)
}

func (m *Model) increaseRefreshInterval() {
	m.refreshInterval = nextRefreshInterval(m.refreshInterval, 1)
}

func (m *Model) decreaseRefreshInterval() {
	m.refreshInterval = nextRefreshInterval(m.refreshInterval, -1)
}

func (m *Model) updateTableColumns() {
	m.subjectTable.SetColumns(subjectColumnsForSort(m.subjectTable.Width(), m.subjectSortKey, m.subjectSortDir))
	m.eventTable.SetColumns(eventColumnsForSort(m.eventTable.Width(), m.eventSortKey, m.eventSortDir))
}

func (m Model) header() string {
	status := "ok"
	if m.lastError != "" {
		status = "error: " + m.lastError
	}
	refresh := "never"
	if !m.lastRefresh.IsZero() {
		refresh = m.lastRefresh.Format("15:04:05")
	}
	title := "CPUGUARD"
	subtitle := fmt.Sprintf("daemon %s | subjects %d | throttled %d | refreshed %s", status, m.summary.Total, m.summary.Throttled+m.summary.Held, refresh)
	left := title + "  " + subtitle
	control := fmt.Sprintf("refresh [-] %s [+]", formatDuration(m.refreshInterval))
	content, _, _, _, _ := headerLayout(m.width, left, control)
	return renderSingleLineStyled(headerStyle, m.width, content)
}

func headerLayout(width int, left string, control string) (content string, minusStart, minusEnd, plusStart, plusEnd int) {
	contentWidth := width - headerStyle.GetHorizontalFrameSize()
	if contentWidth < 1 {
		contentWidth = 1
	}
	controlWidth := ansi.StringWidth(control)
	if controlWidth+1 >= contentWidth {
		return truncateLine(left, contentWidth), -1, -1, -1, -1
	}
	left = truncateLine(left, contentWidth-controlWidth-1)
	spacing := contentWidth - ansi.StringWidth(left) - controlWidth
	content = left + strings.Repeat(" ", spacing) + control
	controlStart := ansi.StringWidth(left) + spacing + headerStyle.GetHorizontalFrameSize()/2
	minusStart = controlStart + strings.Index(control, "[-]")
	minusEnd = minusStart + 2
	plusStart = controlStart + strings.Index(control, "[+]")
	plusEnd = plusStart + 2
	return content, minusStart, minusEnd, plusStart, plusEnd
}

func (m Model) refreshControlBounds() (minusStart, minusEnd, plusStart, plusEnd int) {
	status := "ok"
	if m.lastError != "" {
		status = "error: " + m.lastError
	}
	refresh := "never"
	if !m.lastRefresh.IsZero() {
		refresh = m.lastRefresh.Format("15:04:05")
	}
	left := fmt.Sprintf("CPUGUARD  daemon %s | subjects %d | throttled %d | refreshed %s", status, m.summary.Total, m.summary.Throttled+m.summary.Held, refresh)
	control := fmt.Sprintf("refresh [-] %s [+]", formatDuration(m.refreshInterval))
	_, minusStart, minusEnd, plusStart, plusEnd = headerLayout(m.width, left, control)
	return minusStart, minusEnd, plusStart, plusEnd
}

func (m Model) metrics() string {
	summary := m.summary
	if summary.Total == 0 && len(m.subjects) > 0 {
		summary = summarizeSubjects(m.subjects)
	}
	cards := []string{
		statCard("subjects", fmt.Sprintf("%d", summary.Total)),
		statCard("throttled", fmt.Sprintf("%d", summary.Throttled)),
		statCard("held", fmt.Sprintf("%d", summary.Held)),
		statCard("sys cpu", formatPercent(m.system.CPUPercent)),
		statCard("temp", formatOptionalTemperature(m.system.CPUTempC)),
		statCard("power", formatOptionalPower(m.system.CPUPowerW)),
		statCard("load1", fmt.Sprintf("%.2f", m.system.Load1)),
		statCard("tracked max", formatPercent(summary.MaxCPUPct)),
		statCard("tracked avg", formatPercent(summary.AvgCPUPct)),
	}
	return truncateLine(strings.Join(cards, " "), m.width)
}

func (m Model) trend() string {
	return renderTrendRow(m.width, []trendWidget{
		{
			Label: "SYS CPU",
			Value: latestFloat(m.systemCPUHistory),
			Peak:  maxFloat(m.systemCPUHistory),
			Line:  percentSparkline(m.systemCPUHistory, trendGraphWidth(m.width)),
			Style: trendCPUStyle,
		},
		{
			Label: "THROTTLED",
			Value: float64(latestInt(m.throttledHistory)),
			Peak:  float64(maxInt(m.throttledHistory)),
			Line:  intSparkline(m.throttledHistory, trendGraphWidth(m.width)),
			Style: trendThrottleStyle,
		},
	})
}

func (m Model) panel(title string, body string, focused bool) string {
	style := panelStyle
	if focused {
		style = focusedPanelStyle
	}
	return fitStyled(style, m.width, title+"\n"+body)
}

func (m Model) subjectTableView() string {
	return m.subjectTable.View()
}

func (m Model) eventTableView() string {
	return m.eventTable.View()
}

func (m Model) eventsTitle() string {
	subject, ok := m.selectedSubject()
	if !ok {
		return "EVENTS - all subjects"
	}
	return "EVENTS - " + subject.TargetRef
}

func (m Model) footer() string {
	msg := "click select | click headers sort | [/ ] refresh | tab focus | wheel/keys scroll | r refresh | u unthrottle | h hold | q quit"
	if m.notice != "" {
		msg += " | " + m.notice
	}
	return fitStyled(footerStyle, m.width, msg)
}

func (m Model) confirmation() string {
	return fitStyled(confirmStyle, m.width, fmt.Sprintf("Confirm %s for %s? y/n", m.pendingAction, m.pendingSubject.TargetRef))
}

func (m *Model) recordHistory() {
	m.systemCPUHistory = appendBoundedFloat(m.systemCPUHistory, m.system.CPUPercent, 60)
	m.throttledHistory = appendBoundedInt(m.throttledHistory, m.summary.Throttled+m.summary.Held, 60)
}

func loadData(client *api.Client) tea.Cmd {
	return loadFullDataForViewport(client, "", false, 0, 100, string(subjectSortCPU), "desc")
}

func (m Model) loadDataCmd() tea.Cmd {
	return loadFullDataForViewport(
		m.client,
		m.selectedSubjectID(),
		!m.subjectDeselected,
		m.subjectTable.offset,
		m.subjectPageLimit(),
		string(m.subjectSortKey),
		sortDirectionString(m.subjectSortDir),
	)
}

func (m Model) loadSubjectPageCmd() tea.Cmd {
	return loadSubjectPageForViewport(
		m.client,
		m.selectedSubjectID(),
		m.subjectTable.offset,
		m.subjectPageLimit(),
		string(m.subjectSortKey),
		sortDirectionString(m.subjectSortDir),
	)
}

func (m Model) loadEventsCmd(subjectID string) tea.Cmd {
	return loadEventsForSubject(m.client, subjectID)
}

func loadFullDataForViewport(client *api.Client, subjectID string, autoSelect bool, offset int, limit int, sortKey string, sortDir string) tea.Cmd {
	return func() tea.Msg {
		status, err := client.Status()
		if err != nil {
			return dataMsg{err: err}
		}
		page, err := client.QuerySubjects(model.SubjectQuery{
			Offset:     offset,
			Limit:      limit,
			Sort:       sortKey,
			Dir:        sortDir,
			SelectedID: subjectID,
		})
		if err != nil {
			return dataMsg{err: err}
		}
		subjectID = selectSubjectIDFromPage(page, subjectID, autoSelect)
		var events []model.Event
		if subjectID != "" {
			events, err = client.ListEventsForSubject(subjectID, eventLimit)
		} else {
			events, err = client.ListEvents(eventLimit)
		}
		if err != nil {
			return dataMsg{err: err}
		}
		if subjectID != "" && page.Selected == nil {
			for i := range page.Items {
				if page.Items[i].ID == subjectID {
					page.Selected = &page.Items[i]
					page.SelectedIndex = page.Offset + i
					break
				}
			}
		}
		return dataMsg{
			subjectPage:    page,
			events:         events,
			system:         status.System,
			summary:        status.Summary,
			subjectID:      subjectID,
			at:             time.Now(),
			updateSubjects: true,
			updateStatus:   true,
			updateEvents:   true,
			recordHistory:  true,
		}
	}
}

func loadSubjectPageForViewport(client *api.Client, subjectID string, offset int, limit int, sortKey string, sortDir string) tea.Cmd {
	return func() tea.Msg {
		page, err := client.QuerySubjects(model.SubjectQuery{
			Offset:     offset,
			Limit:      limit,
			Sort:       sortKey,
			Dir:        sortDir,
			SelectedID: subjectID,
		})
		if err != nil {
			return dataMsg{err: err}
		}
		return dataMsg{
			subjectPage:    page,
			subjectID:      subjectID,
			at:             time.Now(),
			updateSubjects: true,
		}
	}
}

func loadEventsForSubject(client *api.Client, subjectID string) tea.Cmd {
	return func() tea.Msg {
		var (
			events []model.Event
			err    error
		)
		if subjectID != "" {
			events, err = client.ListEventsForSubject(subjectID, eventLimit)
		} else {
			events, err = client.ListEvents(eventLimit)
		}
		if err != nil {
			return dataMsg{err: err}
		}
		return dataMsg{
			events:       events,
			subjectID:    subjectID,
			at:           time.Now(),
			updateEvents: true,
		}
	}
}

func selectSubjectIDFromPage(page model.SubjectPage, requested string, autoSelect bool) string {
	if requested != "" && page.Selected != nil {
		return requested
	}
	if !autoSelect {
		return ""
	}
	if len(page.Items) == 0 {
		return ""
	}
	return page.Items[0].ID
}

func summarizeSubjects(subjects []model.Subject) model.SubjectSummary {
	summary := model.SubjectSummary{Total: len(subjects)}
	sumCPU := 0.0
	for _, subject := range subjects {
		if subject.State == model.StateThrottled {
			summary.Throttled++
		}
		if subject.State == model.StateManualHold {
			summary.Held++
		}
		if subject.LastCPUPct > summary.MaxCPUPct {
			summary.MaxCPUPct = subject.LastCPUPct
		}
		sumCPU += subject.LastCPUPct
	}
	if summary.Total > 0 {
		summary.AvgCPUPct = sumCPU / float64(summary.Total)
	}
	return summary
}

func selectSubjectIDForEvents(subjects []model.Subject, requested string, autoSelect bool) string {
	sorted := sortSubjects(subjects)
	if requested != "" {
		for _, subject := range sorted {
			if subject.ID == requested {
				return requested
			}
		}
	}
	if !autoSelect {
		return ""
	}
	if len(sorted) == 0 {
		return ""
	}
	return sorted[0].ID
}

func subjectSortKeyAtX(columns []table.Column, x int) (subjectSortKey, bool) {
	title, ok := columnTitleAtX(columns, x)
	if !ok {
		return "", false
	}
	switch title {
	case "STATE":
		return subjectSortState, true
	case "CPU":
		return subjectSortCPU, true
	case "LIMIT":
		return subjectSortLimit, true
	case "SCOPE":
		return subjectSortScope, true
	case "TARGET":
		return subjectSortTarget, true
	case "RULE":
		return subjectSortRule, true
	default:
		return "", false
	}
}

func eventSortKeyAtX(columns []table.Column, x int) (eventSortKey, bool) {
	title, ok := columnTitleAtX(columns, x)
	if !ok {
		return "", false
	}
	switch title {
	case "TIME":
		return eventSortTime, true
	case "TYPE":
		return eventSortType, true
	case "SUBJECT":
		return eventSortSubject, true
	case "CPU":
		return eventSortCPU, true
	case "LIMIT":
		return eventSortLimit, true
	default:
		return "", false
	}
}

func columnTitleAtX(columns []table.Column, x int) (string, bool) {
	x -= tableGutterWidth
	if x < 0 {
		return "", false
	}
	pos := 0
	for _, column := range columns {
		if column.Width <= 0 {
			continue
		}
		if x >= pos && x < pos+column.Width {
			return baseColumnTitle(column.Title), true
		}
		pos += column.Width
	}
	return "", false
}

func baseColumnTitle(title string) string {
	return strings.TrimSuffix(strings.TrimSuffix(title, " ▲"), " ▼")
}

func toggleSortDirection(direction sortDirection) sortDirection {
	if direction == sortAsc {
		return sortDesc
	}
	return sortAsc
}

func defaultSubjectSortDirection(key subjectSortKey) sortDirection {
	switch key {
	case subjectSortCPU, subjectSortLimit:
		return sortDesc
	default:
		return sortAsc
	}
}

func defaultEventSortDirection(key eventSortKey) sortDirection {
	switch key {
	case eventSortTime, eventSortCPU, eventSortLimit:
		return sortDesc
	default:
		return sortAsc
	}
}

func nextRefreshInterval(current time.Duration, step int) time.Duration {
	intervals := []time.Duration{
		time.Second,
		2 * time.Second,
		5 * time.Second,
		10 * time.Second,
		30 * time.Second,
	}
	idx := 0
	for i, interval := range intervals {
		if current >= interval {
			idx = i
		}
	}
	idx += step
	if idx < 0 {
		idx = 0
	}
	if idx >= len(intervals) {
		idx = len(intervals) - 1
	}
	return intervals[idx]
}

func formatDuration(duration time.Duration) string {
	if duration < time.Second {
		duration = time.Second
	}
	return fmt.Sprintf("%ds", int(duration/time.Second))
}

func tick(interval time.Duration) tea.Cmd {
	if interval < minRefreshInterval {
		interval = minRefreshInterval
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func runAction(client *api.Client, action actionKind, subjectID string) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch action {
		case actionUnthrottle:
			err = client.Unthrottle(subjectID)
		case actionHold:
			err = client.Hold(subjectID)
		}
		return actionMsg{action: action, err: err}
	}
}

var (
	headerStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("24")).Padding(0, 1)
	footerStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	panelStyle         = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238"))
	focusedPanelStyle  = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("39"))
	confirmStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("52")).Padding(0, 1)
	statLabelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	statValueStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	trendShellStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	trendTitleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Bold(true)
	trendCPUStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("80")).Bold(true)
	trendThrottleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
)

func fitStyled(style lipgloss.Style, width int, content string) string {
	if width < 1 {
		width = 1
	}
	contentWidth := width - style.GetHorizontalFrameSize()
	if contentWidth < 1 {
		contentWidth = 1
	}
	rendered := style.Width(contentWidth).Render(content)
	return clipView(rendered, width, 0)
}

func renderSingleLineStyled(style lipgloss.Style, width int, content string) string {
	if width < 1 {
		width = 1
	}
	contentWidth := width - style.GetHorizontalFrameSize()
	if contentWidth < 1 {
		contentWidth = 1
	}
	content = truncateLine(content, contentWidth)
	rendered := style.Render(content)
	return truncateLine(rendered, width)
}

func truncateLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(line, width, "")
}

func clipView(view string, width int, height int) string {
	lines := strings.Split(view, "\n")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = truncateLine(line, width)
	}
	return strings.Join(lines, "\n")
}

func statCard(label string, value string) string {
	return statLabelStyle.Render(label+":") + " " + statValueStyle.Render(value)
}

func formatOptionalTemperature(value *float64) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%.1fC", *value)
}

func formatOptionalPower(value *float64) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%.1fW", *value)
}

type trendWidget struct {
	Label string
	Value float64
	Peak  float64
	Line  string
	Style lipgloss.Style
}

func renderTrendRow(width int, widgets []trendWidget) string {
	if width <= 0 || len(widgets) == 0 {
		return ""
	}
	parts := make([]string, 0, len(widgets))
	gap := trendShellStyle.Render("  ")
	gapWidth := ansi.StringWidth(gap) * (len(widgets) - 1)
	budget := max(1, (width-gapWidth)/len(widgets))
	remaining := max(1, width-gapWidth)
	for i, widget := range widgets {
		partWidth := budget
		if i == len(widgets)-1 {
			partWidth = remaining
		}
		parts = append(parts, renderTrendWidget(widget, partWidth))
		remaining -= partWidth
	}
	line := strings.Join(parts, gap)
	return truncateLine(line, width)
}

func renderTrendWidget(widget trendWidget, width int) string {
	value := formatPercent(widget.Value)
	peak := formatPercent(widget.Peak)
	if widget.Label == "THROTTLED" {
		value = fmt.Sprintf("%d", int(widget.Value))
		peak = fmt.Sprintf("%d", int(widget.Peak))
	}
	if width < 38 {
		prefix := fmt.Sprintf("%s %s pk%s ", widget.Label, value, peak)
		lineWidth := max(1, width-ansi.StringWidth(prefix))
		return truncateLine(
			trendTitleStyle.Render(widget.Label)+
				trendShellStyle.Render(" ")+
				widget.Style.Render(value)+
				trendShellStyle.Render(" pk")+
				widget.Style.Render(peak)+
				trendShellStyle.Render(" ")+
				widget.Style.Render(truncateLine(widget.Line, lineWidth)),
			width,
		)
	}
	prefix := fmt.Sprintf("[%s now %s peak %s 60s ", widget.Label, value, peak)
	lineWidth := max(4, width-ansi.StringWidth(prefix)-1)
	return trendShellStyle.Render("[") +
		trendTitleStyle.Render(widget.Label) +
		trendShellStyle.Render(" now ") +
		widget.Style.Render(value) +
		trendShellStyle.Render(" peak ") +
		widget.Style.Render(peak) +
		trendShellStyle.Render(" 60s ") +
		widget.Style.Render(truncateLine(widget.Line, lineWidth)) +
		trendShellStyle.Render("]")
}

func trendGraphWidth(totalWidth int) int {
	switch {
	case totalWidth >= 120:
		return 24
	case totalWidth >= 90:
		return 18
	case totalWidth >= 70:
		return 12
	default:
		return 8
	}
}

func appendBoundedFloat(values []float64, next float64, limit int) []float64 {
	values = append(values, next)
	if len(values) > limit {
		return values[len(values)-limit:]
	}
	return values
}

func appendBoundedInt(values []int, next int, limit int) []int {
	values = append(values, next)
	if len(values) > limit {
		return values[len(values)-limit:]
	}
	return values
}

func sparkline(values []float64, width int) string {
	return sparklineWithCeiling(values, width, 0)
}

func percentSparkline(values []float64, width int) string {
	return sparklineWithCeiling(values, width, 100)
}

func sparklineWithCeiling(values []float64, width int, ceiling float64) string {
	if len(values) == 0 || width <= 0 {
		return strings.Repeat("·", max(1, width))
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	maxValue := ceiling
	if maxValue <= 0 {
		for _, value := range values {
			if value > maxValue {
				maxValue = value
			}
		}
	}
	if maxValue == 0 {
		maxValue = 1
	}
	var b strings.Builder
	for _, value := range values {
		ratio := value / maxValue
		if ratio > 1 {
			ratio = 1
		}
		switch {
		case ratio >= 0.875:
			b.WriteRune('█')
		case ratio >= 0.75:
			b.WriteRune('▇')
		case ratio >= 0.625:
			b.WriteRune('▆')
		case ratio >= 0.5:
			b.WriteRune('▅')
		case ratio >= 0.375:
			b.WriteRune('▄')
		case ratio >= 0.25:
			b.WriteRune('▃')
		case ratio >= 0.125:
			b.WriteRune('▂')
		default:
			b.WriteRune('▁')
		}
	}
	for ansi.StringWidth(b.String()) < width {
		b.WriteRune('·')
	}
	return b.String()
}

func intSparkline(values []int, width int) string {
	floatValues := make([]float64, len(values))
	for i, value := range values {
		floatValues[i] = float64(value)
	}
	return sparkline(floatValues, width)
}

func latestFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}

func latestInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}

func maxFloat(values []float64) float64 {
	maxValue := 0.0
	for _, value := range values {
		if value > maxValue {
			maxValue = value
		}
	}
	return maxValue
}

func maxInt(values []int) int {
	maxValue := 0
	for _, value := range values {
		if value > maxValue {
			maxValue = value
		}
	}
	return maxValue
}
