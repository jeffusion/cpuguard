package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cpuguard/internal/model"
	"github.com/charmbracelet/bubbles/table"
)

type sortDirection int

const (
	sortAsc sortDirection = iota
	sortDesc
)

type subjectSortKey string

const (
	subjectSortState  subjectSortKey = "state"
	subjectSortCPU    subjectSortKey = "cpu"
	subjectSortLimit  subjectSortKey = "limit"
	subjectSortScope  subjectSortKey = "scope"
	subjectSortTarget subjectSortKey = "target"
	subjectSortRule   subjectSortKey = "rule"
)

type eventSortKey string

const (
	eventSortTime    eventSortKey = "time"
	eventSortType    eventSortKey = "type"
	eventSortCPU     eventSortKey = "cpu"
	eventSortLimit   eventSortKey = "limit"
	eventSortSubject eventSortKey = "subject"
)

func sortSubjects(subjects []model.Subject) []model.Subject {
	return sortSubjectsBy(subjects, subjectSortCPU, sortDesc)
}

func sortSubjectsBy(subjects []model.Subject, key subjectSortKey, direction sortDirection) []model.Subject {
	out := append([]model.Subject(nil), subjects...)
	sort.SliceStable(out, func(i, j int) bool {
		cmp := compareSubjects(out[i], out[j], key)
		if cmp == 0 {
			cmp = -compareSubjects(out[i], out[j], subjectSortCPU)
			if cmp == 0 {
				cmp = strings.Compare(out[i].TargetRef, out[j].TargetRef)
			}
		}
		if direction == sortDesc {
			return cmp > 0
		}
		return cmp < 0
	})
	return out
}

func sortEventsBy(events []model.Event, key eventSortKey, direction sortDirection) []model.Event {
	out := append([]model.Event(nil), events...)
	sort.SliceStable(out, func(i, j int) bool {
		cmp := compareEvents(out[i], out[j], key)
		if cmp == 0 {
			cmp = out[i].CreatedAt.Compare(out[j].CreatedAt)
		}
		if direction == sortDesc {
			return cmp > 0
		}
		return cmp < 0
	})
	return out
}

func compareSubjects(left, right model.Subject, key subjectSortKey) int {
	switch key {
	case subjectSortState:
		return compareInt(stateRank(left.State), stateRank(right.State))
	case subjectSortCPU:
		return compareFloat(left.LastCPUPct, right.LastCPUPct)
	case subjectSortLimit:
		return compareFloat(limitSortValue(left), limitSortValue(right))
	case subjectSortScope:
		return strings.Compare(string(left.Scope), string(right.Scope))
	case subjectSortTarget:
		return strings.Compare(left.TargetRef, right.TargetRef)
	case subjectSortRule:
		return strings.Compare(left.RuleID, right.RuleID)
	default:
		return 0
	}
}

func compareEvents(left, right model.Event, key eventSortKey) int {
	switch key {
	case eventSortTime:
		return left.CreatedAt.Compare(right.CreatedAt)
	case eventSortType:
		return strings.Compare(left.Type, right.Type)
	case eventSortCPU:
		return compareFloat(left.CPUPct, right.CPUPct)
	case eventSortLimit:
		return compareFloat(eventLimitSortValue(left), eventLimitSortValue(right))
	case eventSortSubject:
		return strings.Compare(left.SubjectID, right.SubjectID)
	default:
		return 0
	}
}

func stateRank(state model.SubjectState) int {
	switch state {
	case model.StateThrottled:
		return 0
	case model.StateManualHold:
		return 1
	default:
		return 2
	}
}

func limitSortValue(subject model.Subject) float64 {
	if subject.State != model.StateThrottled && subject.State != model.StateManualHold {
		return -1
	}
	return subject.CurrentLimitPct
}

func eventLimitSortValue(event model.Event) float64 {
	if event.LimitPct <= 0 {
		return -1
	}
	return event.LimitPct
}

func compareFloat(left, right float64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func subjectRows(subjects []model.Subject) []table.Row {
	rows := make([]table.Row, 0, len(subjects))
	for _, subject := range subjects {
		rows = append(rows, subjectRow(subject))
	}
	return rows
}

func subjectRow(subject model.Subject) table.Row {
	return table.Row{
		string(subject.State),
		formatPercent(subject.LastCPUPct),
		formatSubjectLimit(subject),
		string(subject.Scope),
		subject.TargetRef,
		subject.RuleID,
	}
}

func loadingSubjectRow() table.Row {
	return table.Row{"", "", "", "", "loading...", ""}
}

func subjectColumnsForSort(width int, key subjectSortKey, direction sortDirection) []table.Column {
	columns := subjectColumns(width)
	for i := range columns {
		columns[i].Title = subjectColumnTitle(columns[i].Title, key, direction)
	}
	return columns
}

func eventColumnsForSort(width int, key eventSortKey, direction sortDirection) []table.Column {
	columns := eventColumns(width)
	for i := range columns {
		columns[i].Title = eventColumnTitle(columns[i].Title, key, direction)
	}
	return columns
}

func subjectColumnTitle(title string, key subjectSortKey, direction sortDirection) string {
	switch title {
	case "STATE":
		return sortTitle(title, key == subjectSortState, direction)
	case "CPU":
		return sortTitle(title, key == subjectSortCPU, direction)
	case "LIMIT":
		return sortTitle(title, key == subjectSortLimit, direction)
	case "SCOPE":
		return sortTitle(title, key == subjectSortScope, direction)
	case "TARGET":
		return sortTitle(title, key == subjectSortTarget, direction)
	case "RULE":
		return sortTitle(title, key == subjectSortRule, direction)
	default:
		return title
	}
}

func eventColumnTitle(title string, key eventSortKey, direction sortDirection) string {
	switch title {
	case "TIME":
		return sortTitle(title, key == eventSortTime, direction)
	case "TYPE":
		return sortTitle(title, key == eventSortType, direction)
	case "SUBJECT":
		return sortTitle(title, key == eventSortSubject, direction)
	case "CPU":
		return sortTitle(title, key == eventSortCPU, direction)
	case "LIMIT":
		return sortTitle(title, key == eventSortLimit, direction)
	default:
		return title
	}
}

func sortTitle(title string, active bool, direction sortDirection) string {
	if !active {
		return title
	}
	if direction == sortDesc {
		return title + " ▼"
	}
	return title + " ▲"
}

func eventRows(events []model.Event) []table.Row {
	rows := make([]table.Row, 0, len(events))
	for _, event := range events {
		rows = append(rows, eventRow(event))
	}
	return rows
}

func eventRow(event model.Event) table.Row {
	return table.Row{
		formatTime(event.CreatedAt),
		event.Type,
		shortSubject(event.SubjectID),
		formatPercent(event.CPUPct),
		formatPercent(event.LimitPct),
		event.Message,
	}
}

func subjectColumns(width int) []table.Column {
	contentWidth := tableContentWidth(width)
	if width < 40 {
		return []table.Column{
			{Title: "STATE", Width: 10},
			{Title: "CPU", Width: 7},
			{Title: "LIMIT", Width: 0},
			{Title: "SCOPE", Width: 0},
			{Title: "TARGET", Width: max(8, contentWidth-17)},
			{Title: "RULE", Width: 0},
		}
	}
	if width < 70 {
		return []table.Column{
			{Title: "STATE", Width: 11},
			{Title: "CPU", Width: 7},
			{Title: "LIMIT", Width: 9},
			{Title: "SCOPE", Width: 0},
			{Title: "TARGET", Width: max(10, contentWidth-27)},
			{Title: "RULE", Width: 0},
		}
	}
	if width < 90 {
		remaining := max(0, contentWidth-28)
		targetWidth := clamp(remaining*55/100, 18, 34)
		ruleWidth := max(0, remaining-targetWidth)
		return []table.Column{
			{Title: "STATE", Width: 12},
			{Title: "CPU", Width: 7},
			{Title: "LIMIT", Width: 9},
			{Title: "SCOPE", Width: 0},
			{Title: "TARGET", Width: targetWidth},
			{Title: "RULE", Width: ruleWidth},
		}
	}
	remaining := max(0, contentWidth-43)
	targetWidth := clamp(remaining*55/100, 22, 56)
	ruleWidth := max(0, remaining-targetWidth)
	return []table.Column{
		{Title: "STATE", Width: 12},
		{Title: "CPU", Width: 7},
		{Title: "LIMIT", Width: 9},
		{Title: "SCOPE", Width: 15},
		{Title: "TARGET", Width: targetWidth},
		{Title: "RULE", Width: ruleWidth},
	}
}

func eventColumns(width int) []table.Column {
	contentWidth := tableContentWidth(width)
	if width < 50 {
		return []table.Column{
			{Title: "TIME", Width: 8},
			{Title: "TYPE", Width: 12},
			{Title: "SUBJECT", Width: 0},
			{Title: "CPU", Width: 0},
			{Title: "LIMIT", Width: 0},
			{Title: "MESSAGE", Width: max(8, contentWidth-20)},
		}
	}
	if width < 70 {
		return []table.Column{
			{Title: "TIME", Width: 8},
			{Title: "TYPE", Width: 14},
			{Title: "SUBJECT", Width: 0},
			{Title: "CPU", Width: 0},
			{Title: "LIMIT", Width: 0},
			{Title: "MESSAGE", Width: max(10, contentWidth-22)},
		}
	}
	messageWidth := max(12, contentWidth-54)
	return []table.Column{
		{Title: "TIME", Width: 8},
		{Title: "TYPE", Width: 14},
		{Title: "SUBJECT", Width: 16},
		{Title: "CPU", Width: 7},
		{Title: "LIMIT", Width: 9},
		{Title: "MESSAGE", Width: messageWidth},
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("15:04:05")
}

func shortSubject(id string) string {
	if len(id) <= 18 {
		return id
	}
	parts := strings.Split(id, "/")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if len(last) <= 18 {
			return last
		}
		return last[:18]
	}
	return id[:18]
}

func formatPercent(cpuPct float64) string {
	if cpuPct <= 0 {
		return "0.0"
	}
	return fmt.Sprintf("%.1f", cpuPct)
}

func formatSubjectLimit(subject model.Subject) string {
	if subject.State != model.StateThrottled && subject.State != model.StateManualHold {
		return "--"
	}
	if subject.CurrentLimitPct <= 0 {
		return "--"
	}
	return formatPercent(subject.CurrentLimitPct)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func tableContentWidth(width int) int {
	return max(1, width-tableGutterWidth-2)
}
