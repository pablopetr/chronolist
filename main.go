package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite" // <- Aqui estava o erro de sintaxe

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type itemStatus int

const (
	NotStarted itemStatus = iota
	Started
	Done
)

type item struct {
	text           string
	status         itemStatus
	createdAt      time.Time
	checkedAt      *time.Time
	frozenDuration time.Duration
}

type model struct {
	items          []item
	cursor         int
	input          textinput.Model
	viewportHeight int
	paused         bool
	pausedAt       time.Time
	db             *sql.DB
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func ptr(t time.Time) *time.Time {
	return &t
}

func openDB() (*sql.DB, error) {
	return sql.Open("sqlite", "./checklist.db")
}

func loadItems(db *sql.DB) []item {
	items := []item{}
	rows, err := db.Query("SELECT text, status, created_at, checked_at, frozen_duration FROM items")
	if err != nil {
		return items
	}
	defer rows.Close()

	for rows.Next() {
		var it item
		var createdAt, checkedAtStr string
		rows.Scan(&it.text, &it.status, &createdAt, &checkedAtStr, &it.frozenDuration)
		it.createdAt, _ = time.Parse(time.RFC3339, createdAt)
		if checkedAtStr != "" {
			t, _ := time.Parse(time.RFC3339, checkedAtStr)
			it.checkedAt = &t
		}
		items = append(items, it)
	}
	return items
}

func saveItem(db *sql.DB, it item) {
	var checkedAtStr string
	if it.checkedAt != nil {
		checkedAtStr = it.checkedAt.Format(time.RFC3339)
	}
	_, _ = db.Exec(`
		INSERT INTO items (text, status, created_at, checked_at, frozen_duration)
		VALUES (?, ?, ?, ?, ?)
	`, it.text, it.status, it.createdAt.Format(time.RFC3339), checkedAtStr, it.frozenDuration)
}

func updateItem(db *sql.DB, it item) {
	var checkedAtStr string
	if it.checkedAt != nil {
		checkedAtStr = it.checkedAt.Format(time.RFC3339)
	}
	_, _ = db.Exec(`
		UPDATE items SET status=?, created_at=?, checked_at=?, frozen_duration=? WHERE text=?
	`, it.status, it.createdAt.Format(time.RFC3339), checkedAtStr, it.frozenDuration, it.text)
}

func initialModel() model {
	db, err := openDB()
	if err != nil {
		fmt.Println("Failed to open DB:", err)
		os.Exit(1)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			text TEXT,
			status INTEGER,
			created_at TEXT,
			checked_at TEXT,
			frozen_duration INTEGER
		);
	`)
	if err != nil {
		fmt.Println("Failed to create table:", err)
		os.Exit(1)
	}

	input := textinput.New()
	input.Placeholder = "Add new item"
	input.Focus()

	return model{
		items: loadItems(db),
		input: input,
		db:    db,
	}
}

func (m model) Init() tea.Cmd {
	return tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewportHeight = msg.Height - 4
		return m, nil

	case tea.KeyMsg:
		input := strings.TrimSpace(m.input.Value())
		inputIsEmpty := input == ""

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "q":
			if inputIsEmpty {
				return m, tea.Quit
			}

		case "up":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}

		case "enter":
			switch input {
			case "\\q":
				return m, tea.Quit

			case "\\p":
				now := time.Now()
				if !m.paused {
					m.paused = true
					m.pausedAt = now
					for i := range m.items {
						if m.items[i].status == Started {
							m.items[i].frozenDuration = time.Since(m.items[i].createdAt)
							updateItem(m.db, m.items[i])
						}
					}
				} else {
					elapsedPaused := now.Sub(m.pausedAt)
					for i := range m.items {
						if m.items[i].status == Started {
							m.items[i].createdAt = m.items[i].createdAt.Add(elapsedPaused)
							updateItem(m.db, m.items[i])
						}
					}
					m.paused = false
				}

			case "\\r":
				if m.cursor >= 0 && m.cursor < len(m.items) {
					item := &m.items[m.cursor]
					item.createdAt = time.Now()
					item.frozenDuration = 0
					item.checkedAt = nil
					updateItem(m.db, *item)
				}

			default:
				if input != "" {
					now := time.Now()
					it := item{
						text:           input,
						status:         NotStarted,
						createdAt:      now,
						frozenDuration: 0,
					}
					m.items = append(m.items, it)
					saveItem(m.db, it)
				}
			}
			m.input.SetValue("")

		case " ":
			if inputIsEmpty && len(m.items) > 0 {
				i := &m.items[m.cursor]
				switch i.status {
				case NotStarted:
					i.status = Started
					if i.frozenDuration > 0 {
						i.createdAt = time.Now().Add(-i.frozenDuration)
					} else {
						i.createdAt = time.Now()
					}
					i.checkedAt = nil
				case Started:
					if m.paused {
						i.checkedAt = ptr(i.createdAt.Add(i.frozenDuration))
					} else {
						i.checkedAt = ptr(time.Now())
					}
					i.frozenDuration = i.checkedAt.Sub(i.createdAt)
					i.status = Done
				case Done:
					i.status = NotStarted
					// Do NOT reset createdAt or frozenDuration
				}
				updateItem(m.db, *i)
				return m, nil
			}
		}

	case tickMsg:
		return m, tick()
	}

	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString("Checklist:\n\n")

	start := 0
	end := len(m.items)

	if m.viewportHeight > 0 && len(m.items) > m.viewportHeight {
		if m.cursor >= m.viewportHeight {
			start = m.cursor - m.viewportHeight + 1
		}
		end = start + m.viewportHeight
		if end > len(m.items) {
			end = len(m.items)
		}
	}

	for i := start; i < end; i++ {
		it := m.items[i]
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}
		var icon string
		switch it.status {
		case NotStarted:
			icon = "[ ]"
		case Started:
			icon = "[>]"
		case Done:
			icon = "[x]"
		}
		var duration time.Duration
		if it.status == Done && it.checkedAt != nil {
			duration = it.frozenDuration
		} else if it.status == Started {
			if m.paused {
				duration = it.frozenDuration
			} else {
				duration = time.Since(it.createdAt)
			}
		} else {
			duration = it.frozenDuration
		}
		line := fmt.Sprintf("%s %s %s (%s)", cursor, icon, it.text, duration.Round(time.Second))
		b.WriteString(line + "\n")
	}

	status := ""
	if m.paused {
		status = " ⏸ PAUSED"
	}

	b.WriteString("\n" + m.input.View())
	b.WriteString(fmt.Sprintf("\n\n↑/↓ to move • [Space] to toggle status • [Enter] to add • \\q to quit • \\p to pause • \\r to restart%s", status))
	return b.String()
}

func main() {
	if err := tea.NewProgram(initialModel(), tea.WithAltScreen()).Start(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
