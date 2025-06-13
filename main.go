package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type itemStatus int

const (
	NotStarted itemStatus = iota
	Started
	Done
)

type task struct {
	ID     int64
	Code   string
	Title  string
	Status itemStatus
}

type item struct {
	ID             int64
	TaskID         int64
	Text           string
	Status         itemStatus
	CreatedAt      time.Time
	CheckedAt      *time.Time
	FrozenDuration time.Duration
}

type model struct {
	tasks          []task
	selectedTaskID int64

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

func loadTasks(db *sql.DB) []task {
	tasks := []task{}
	rows, _ := db.Query("SELECT id, code, title, status FROM tasks")
	defer rows.Close()
	for rows.Next() {
		var t task
		rows.Scan(&t.ID, &t.Code, &t.Title, &t.Status)
		tasks = append(tasks, t)
	}
	return tasks
}

func loadItems(db *sql.DB, taskID int64) []item {
	items := []item{}
	rows, _ := db.Query("SELECT id, task_id, text, status, created_at, checked_at, frozen_duration FROM items WHERE task_id = ?", taskID)
	defer rows.Close()
	for rows.Next() {
		var it item
		var createdAt, checkedAtStr string
		rows.Scan(&it.ID, &it.TaskID, &it.Text, &it.Status, &createdAt, &checkedAtStr, &it.FrozenDuration)
		it.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if checkedAtStr != "" {
			t, _ := time.Parse(time.RFC3339, checkedAtStr)
			it.CheckedAt = &t
		}
		items = append(items, it)
	}
	return items
}

func saveTask(db *sql.DB, code, title string) {
	db.Exec("INSERT INTO tasks (code, title, status) VALUES (?, ?, ?)", code, title, NotStarted)
}

func deleteTask(db *sql.DB, taskID int64) {
	db.Exec("DELETE FROM items WHERE task_id = ?", taskID)
	db.Exec("DELETE FROM tasks WHERE id = ?", taskID)
}

func deleteItem(db *sql.DB, itemID int64) {
	db.Exec("DELETE FROM items WHERE id = ?", itemID)
}

func saveItem(db *sql.DB, it item) {
	var checkedAtStr string
	if it.CheckedAt != nil {
		checkedAtStr = it.CheckedAt.Format(time.RFC3339)
	}
	db.Exec(`INSERT INTO items (task_id, text, status, created_at, checked_at, frozen_duration) VALUES (?, ?, ?, ?, ?, ?)`,
		it.TaskID, it.Text, it.Status, it.CreatedAt.Format(time.RFC3339), checkedAtStr, it.FrozenDuration)
}

func updateTaskStatus(db *sql.DB, taskID int64) {
	var total, done, started int
	row := db.QueryRow("SELECT COUNT(*) FROM items WHERE task_id = ?", taskID)
	row.Scan(&total)
	row = db.QueryRow("SELECT COUNT(*) FROM items WHERE task_id = ? AND status = ?", taskID, Done)
	row.Scan(&done)
	row = db.QueryRow("SELECT COUNT(*) FROM items WHERE task_id = ? AND status = ?", taskID, Started)
	row.Scan(&started)

	var newStatus itemStatus
	if done == total && total > 0 {
		newStatus = Done
	} else if started > 0 || done > 0 {
		newStatus = Started
	} else {
		newStatus = NotStarted
	}
	db.Exec("UPDATE tasks SET status = ? WHERE id = ?", newStatus, taskID)
}

func initialModel() model {
	db, err := openDB()
	if err != nil {
		fmt.Println("Failed to open DB:", err)
		os.Exit(1)
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		code TEXT,
		title TEXT,
		status INTEGER
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id INTEGER,
		text TEXT,
		status INTEGER,
		created_at TEXT,
		checked_at TEXT,
		frozen_duration INTEGER
	)`)
	input := textinput.New()
	input.Placeholder = "Add new task"
	input.Focus()
	return model{
		tasks: loadTasks(db),
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
		if input == "\\q" {
			return m, tea.Quit
		} else if input == "\\d" {
			if m.selectedTaskID == 0 && len(m.tasks) > 0 {
				taskID := m.tasks[m.cursor].ID
				deleteTask(m.db, taskID)
				m.tasks = loadTasks(m.db)
				if m.cursor > 0 {
					m.cursor--
				}
				m.input.SetValue("")
				return m, nil
			} else if m.selectedTaskID != 0 && len(m.items) > 0 {
				itemID := m.items[m.cursor].ID
				deleteItem(m.db, itemID)
				m.items = loadItems(m.db, m.selectedTaskID)
				updateTaskStatus(m.db, m.selectedTaskID)
				if m.cursor > 0 {
					m.cursor--
				}
				m.input.SetValue("")
				return m, nil
			}
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			if m.selectedTaskID == 0 {
				if len(m.tasks) > 0 && input == "" {
					m.selectedTaskID = m.tasks[m.cursor].ID
					m.items = loadItems(m.db, m.selectedTaskID)
					m.input.Placeholder = "Add new item"
					m.input.SetValue("")
					m.cursor = 0
				} else if input != "" {
					saveTask(m.db, fmt.Sprintf("T%02d", len(m.tasks)+1), input)
					m.tasks = loadTasks(m.db)
					m.input.SetValue("")
				}
			} else {
				if input != "" {
					it := item{
						TaskID:    m.selectedTaskID,
						Text:      input,
						Status:    NotStarted,
						CreatedAt: time.Now(),
					}
					saveItem(m.db, it)
					m.items = loadItems(m.db, m.selectedTaskID)
					updateTaskStatus(m.db, m.selectedTaskID)
					m.input.SetValue("")
				}
			}
		case "esc":
			m.selectedTaskID = 0
			m.items = nil
			m.input.Placeholder = "Add new task"
			m.input.SetValue("")
			m.tasks = loadTasks(m.db)
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if m.selectedTaskID == 0 && m.cursor < len(m.tasks)-1 {
				m.cursor++
			} else if m.selectedTaskID != 0 && m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case " ":
			if m.selectedTaskID != 0 && len(m.items) > 0 {
				i := &m.items[m.cursor]
				switch i.Status {
				case NotStarted:
					i.Status = Started
					i.CreatedAt = time.Now()
				case Started:
					i.Status = Done
					now := time.Now()
					i.CheckedAt = &now
					i.FrozenDuration = now.Sub(i.CreatedAt)
				case Done:
					i.Status = NotStarted
				}
				m.db.Exec("UPDATE items SET status = ?, created_at = ?, checked_at = ?, frozen_duration = ? WHERE id = ?",
					i.Status,
					i.CreatedAt.Format(time.RFC3339),
					func() string {
						if i.CheckedAt != nil {
							return i.CheckedAt.Format(time.RFC3339)
						}
						return ""
					}(),
					i.FrozenDuration,
					i.ID,
				)
				updateTaskStatus(m.db, m.selectedTaskID)
			}
		}
	}
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString("Checklist:\n\n")
	if m.selectedTaskID == 0 {
		for i, t := range m.tasks {
			cursor := " "
			if i == m.cursor {
				cursor = ">"
			}
			statusStr := map[itemStatus]string{NotStarted: "[ ]", Started: "[>]", Done: "[x]"}[t.Status]
			b.WriteString(fmt.Sprintf("%s %s %s - %s\n", cursor, statusStr, t.Code, t.Title))
		}
		b.WriteString("\n" + m.input.View())
		b.WriteString("\n\n↑/↓ to move • [Enter] to select • \\d to delete • esc to go back • \\q to quit")
	} else {
		for i, it := range m.items {
			cursor := " "
			if i == m.cursor {
				cursor = ">"
			}
			statusStr := map[itemStatus]string{NotStarted: "[ ]", Started: "[>]", Done: "[x]"}[it.Status]
			duration := it.FrozenDuration
			if it.Status == Started && !m.paused {
				duration = time.Since(it.CreatedAt)
			}
			b.WriteString(fmt.Sprintf("%s %s %s (%s)\n", cursor, statusStr, it.Text, duration.Round(time.Second)))
		}
		b.WriteString("\n" + m.input.View())
		b.WriteString("\n\n↑/↓ to move • [Space] to toggle • esc to go back • \\d to delete • \\q to quit")
	}
	return b.String()
}

func main() {
	if err := tea.NewProgram(initialModel(), tea.WithAltScreen()).Start(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
