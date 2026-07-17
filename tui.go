package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// runTUI starts the interactive job browser used when ges is invoked with no
// arguments: a job list, drilling into a job's spooled files, drilling into
// a file viewed like `less`.
func runTUI(w *Workspace) error {
	p := tea.NewProgram(newTuiModel(w), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// screen identifies which of the three nested views is active.
type screen int

const (
	screenJobs screen = iota
	screenFiles
	screenPager
)

var helpStyle = lipgloss.NewStyle().Faint(true).Padding(0, 1)

// jobItem adapts *Job to list.Item for the job-list screen.
type jobItem struct{ job *Job }

func (i jobItem) Title() string {
	status, rc := "done", "-"
	if i.job.Running() {
		status = fmt.Sprintf("running (pid %d)", i.job.PID)
	} else if code, ok := i.job.ExitCode(); ok {
		rc = fmt.Sprint(code)
	}
	return fmt.Sprintf("%s  %-20s  %-16s  rc=%s", formatJobNumber(i.job.Number), i.job.Entry, status, rc)
}
func (i jobItem) Description() string { return i.job.Dir }
func (i jobItem) FilterValue() string { return i.job.Entry }

// fileItem adapts a spooled file name to list.Item for the file-list screen.
type fileItem struct{ name string }

func (i fileItem) Title() string       { return i.name }
func (i fileItem) Description() string { return "" }
func (i fileItem) FilterValue() string { return i.name }

type tuiModel struct {
	w      *Workspace
	screen screen

	jobList  list.Model
	fileList list.Model
	pager    viewport.Model

	curJob      *Job
	curFileName string
	pagerReturn screen // screen to go back to on esc from the pager

	width, height int
	err           error
}

func newTuiModel(w *Workspace) *tuiModel {
	jl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	jl.Title = "ges jobs"

	fileDelegate := list.NewDefaultDelegate()
	fileDelegate.ShowDescription = false
	fileDelegate.SetHeight(1)
	fileDelegate.SetSpacing(0)
	fl := list.New(nil, fileDelegate, 0, 0)
	fl.Title = "job files"

	return &tuiModel{w: w, jobList: jl, fileList: fl, pager: viewport.New(0, 0)}
}

func (m *tuiModel) Init() tea.Cmd {
	return m.loadJobs()
}

// loadJobs refreshes the job list from disk.
func (m *tuiModel) loadJobs() tea.Cmd {
	return func() tea.Msg {
		jobs, err := m.w.listJobs()
		if err != nil {
			return errMsg{err}
		}
		items := make([]list.Item, len(jobs))
		for i, j := range jobs {
			items[i] = jobItem{job: j}
		}
		return jobsLoadedMsg{items}
	}
}

type jobsLoadedMsg struct{ items []list.Item }
type errMsg struct{ err error }

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case errMsg:
		m.err = msg.err
		return m, nil

	case jobsLoadedMsg:
		m.jobList.SetItems(msg.items)
		return m, nil

	case jobPurgedMsg:
		return m, m.loadJobs()

	case filesLoadedMsg, fileLoadedMsg:
		m.applyLoad(msg)
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		listW, listH := msg.Width, msg.Height-2
		m.jobList.SetSize(listW, listH)
		m.fileList.SetSize(listW, listH)
		m.pager.Width, m.pager.Height = msg.Width, msg.Height-2
		return m, nil

	case tea.KeyMsg:
		switch m.screen {
		case screenJobs:
			return m.updateJobs(msg)
		case screenFiles:
			return m.updateFiles(msg)
		case screenPager:
			return m.updatePager(msg)
		}
	}

	var cmd tea.Cmd
	switch m.screen {
	case screenJobs:
		m.jobList, cmd = m.jobList.Update(msg)
	case screenFiles:
		m.fileList, cmd = m.fileList.Update(msg)
	case screenPager:
		m.pager, cmd = m.pager.Update(msg)
	}
	return m, cmd
}

func (m *tuiModel) updateJobs(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "enter":
		if it, ok := m.jobList.SelectedItem().(jobItem); ok {
			m.curJob = it.job
			return m, m.loadFiles()
		}
		return m, nil
	case "s":
		if it, ok := m.jobList.SelectedItem().(jobItem); ok {
			m.curJob = it.job
			m.curFileName = "spool"
			m.pagerReturn = screenJobs
			return m, m.loadSpool()
		}
		return m, nil
	case "delete":
		if it, ok := m.jobList.SelectedItem().(jobItem); ok {
			return m, m.purgeJob(it.job)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.jobList, cmd = m.jobList.Update(msg)
	return m, cmd
}

type jobPurgedMsg struct{}

// purgeJob deletes a job's spooled directory, then refreshes the job list.
func (m *tuiModel) purgeJob(j *Job) tea.Cmd {
	return func() tea.Msg {
		if err := os.RemoveAll(j.Dir); err != nil {
			return errMsg{err}
		}
		return jobPurgedMsg{}
	}
}

// loadFiles lists the spooled files inside the current job's directory.
func (m *tuiModel) loadFiles() tea.Cmd {
	return func() tea.Msg {
		entries, err := os.ReadDir(m.curJob.Dir)
		if err != nil {
			return errMsg{err}
		}
		type fileWithTime struct {
			name  string
			mtime time.Time
		}
		files := make([]fileWithTime, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, fileWithTime{name: e.Name(), mtime: info.ModTime()})
		}
		sort.Slice(files, func(a, b int) bool { return files[a].mtime.Before(files[b].mtime) })

		items := make([]list.Item, len(files))
		for i, f := range files {
			items[i] = fileItem{name: f.name}
		}
		return filesLoadedMsg{items}
	}
}

type filesLoadedMsg struct{ items []list.Item }

func (m *tuiModel) updateFiles(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "backspace":
		m.screen = screenJobs
		return m, nil
	case "enter":
		if it, ok := m.fileList.SelectedItem().(fileItem); ok {
			m.curFileName = it.name
			m.pagerReturn = screenFiles
			return m, m.loadFile()
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.fileList, cmd = m.fileList.Update(msg)
	return m, cmd
}

// loadFile reads the selected file's content for the pager.
func (m *tuiModel) loadFile() tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(filepath.Join(m.curJob.Dir, m.curFileName))
		if err != nil {
			return errMsg{err}
		}
		return fileLoadedMsg{string(data)}
	}
}

// loadSpool reads the job's unified spool (gesmsgstart, stdout, stderr,
// gesmsgend concatenated in that order, like `ges job <n>`) for the pager.
func (m *tuiModel) loadSpool() tea.Cmd {
	return func() tea.Msg {
		var buf bytes.Buffer
		if err := m.curJob.writeSpool(&buf); err != nil {
			return errMsg{err}
		}
		return fileLoadedMsg{buf.String()}
	}
}

type fileLoadedMsg struct{ content string }

func (m *tuiModel) updatePager(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "backspace":
		m.screen = m.pagerReturn
		return m, nil
	}
	var cmd tea.Cmd
	m.pager, cmd = m.pager.Update(msg)
	return m, cmd
}

// handle the non-key messages that carry loaded data into the right screen.
func (m *tuiModel) applyLoad(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case filesLoadedMsg:
		m.fileList.SetItems(msg.items)
		m.screen = screenFiles
		return true
	case fileLoadedMsg:
		m.pager.SetContent(msg.content)
		m.pager.GotoTop()
		m.screen = screenPager
		return true
	}
	return false
}

func (m *tuiModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("error: %v\n", m.err)
	}

	var body, help string
	switch m.screen {
	case screenJobs:
		body = m.jobList.View()
		help = "enter: browse files  s: view spool  del: purge  q: quit"
	case screenFiles:
		body = m.fileList.View()
		help = fmt.Sprintf("job %s  enter: view file  esc: back  q: quit", formatJobNumber(m.curJob.Number))
	case screenPager:
		body = m.pager.View()
		help = fmt.Sprintf("%s  %3.f%%  esc: back  q: quit", m.curFileName, m.pager.ScrollPercent()*100)
	}
	return body + "\n" + helpStyle.Render(help)
}
