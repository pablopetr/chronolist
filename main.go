package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type item struct {
	text      string
	checked   bool
	createdAt time.Time
	checkedAt *time.Time
}

type model struct {
	items          []item
	cursor         int
	input          textinput.Model
	viewportHeight int
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

func initialModel() model {
	input := textinput.New()
	input.Placeholder = "Add new item"
	input.Focus()
	now := time.Now()

	return model{
		items: []item{
			{"Buy milk", false, now, nil},
			{"Learn Go", false, now, nil},
			{"Write checklist app", false, now, nil},
		},
		input: input,
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
		inputIsEmpty := strings.TrimSpace(m.input.Value()) == ""

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
			text := strings.TrimSpace(m.input.Value())
			if text != "" {
				m.items = append(m.items, item{
					text:      text,
					checked:   false,
					createdAt: time.Now(),
				})
				m.input.SetValue("")
			}

		case " ":
			if inputIsEmpty {
				if len(m.items) > 0 {
					i := &m.items[m.cursor]
					i.checked = !i.checked
					if i.checked {
						now := time.Now()
						i.checkedAt = &now
					} else {
						i.checkedAt = nil
					}
				}
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
		check := "[ ]"
		if it.checked {
			check = "[x]"
		}
		var duration time.Duration
		if it.checkedAt != nil {
			duration = it.checkedAt.Sub(it.createdAt)
		} else {
			duration = time.Since(it.createdAt)
		}
		line := fmt.Sprintf("%s %s %s (%s)", cursor, check, it.text, duration.Round(time.Second))
		b.WriteString(line + "\n")
	}

	b.WriteString("\n" + m.input.View())
	b.WriteString("\n\n↑/↓ to move • [Space] to check • [Enter] to add • q to quit")
	return b.String()
}

func main() {
	if err := tea.NewProgram(initialModel(), tea.WithAltScreen()).Start(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
