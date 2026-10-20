package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/docker-agent/pkg/tui/components/editor"
	"github.com/docker/docker-agent/pkg/tui/components/notification"
)

type speechRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	events chan speechEvent
	tab    *tabModel
	editor editor.Editor
	err    error // Read only after done closes.
}

type speechEvent struct {
	run   *speechRun
	delta string
	ready bool
	ended bool
}

func (m *appModel) isTranscribing() bool {
	m.speechMu.Lock()
	defer m.speechMu.Unlock()
	return m.speech != nil
}

func (m *appModel) handleStartSpeak() (tea.Model, tea.Cmd) {
	m.speechMu.Lock()
	defer m.speechMu.Unlock()
	if m.speech != nil || m.speechCtx == nil || m.speechCtx.Err() != nil {
		return m, nil
	}
	ctx, cancel := context.WithCancel(m.speechCtx)
	run := &speechRun{
		cancel: cancel,
		done:   make(chan struct{}),
		events: make(chan speechEvent, 1),
		tab:    m.activeTab,
		editor: m.activeTab.editor,
	}
	m.speech = run
	send := func(ctx context.Context, event speechEvent) {
		event.run = run
		select {
		case run.events <- event:
		case <-ctx.Done():
		}
	}
	go func() {
		defer close(run.done)
		defer cancel()
		run.err = m.transcriber.Run(ctx, func(ctx context.Context, delta string) {
			send(ctx, speechEvent{delta: delta})
		}, func(ctx context.Context) { send(ctx, speechEvent{ready: true}) })
	}()
	return m, waitForSpeech(run)
}

func waitForSpeech(run *speechRun) tea.Cmd {
	return func() tea.Msg {
		select {
		case event := <-run.events:
			return event
		case <-run.done:
			// Deliver any final queued delta before the terminal event.
			select {
			case event := <-run.events:
				return event
			default:
				return speechEvent{run: run, ended: true}
			}
		}
	}
}

func (m *appModel) handleSpeechEvent(event speechEvent) (tea.Model, tea.Cmd) {
	m.speechMu.Lock()
	defer m.speechMu.Unlock()
	if m.speech != event.run {
		return m, nil
	}
	run := event.run
	if event.ended {
		m.speech = nil
		var notice tea.Cmd
		if run.err != nil {
			notice = notification.ErrorCmd(fmt.Sprintf("Transcription failed: %v", run.err))
		}
		return m, tea.Batch(run.editor.SetRecording(false), notice)
	}
	if event.ready {
		return m, tea.Batch(
			run.editor.SetRecording(true),
			notification.InfoCmd("🎤 Listening... (ENTER to send or ESC to cancel)"),
			waitForSpeech(run),
		)
	}
	run.editor.InsertText(event.delta)
	return m, waitForSpeech(run)
}

// stopSpeech joins the run before releasing its resources or allowing another run.
func (m *appModel) stopSpeech() *speechRun {
	return m.stopSpeechForTab(nil)
}

func (m *appModel) stopSpeechForTab(tab *tabModel) *speechRun {
	m.speechMu.Lock()
	defer m.speechMu.Unlock()
	run := m.speech
	if run != nil && tab != nil && run.tab != tab {
		return nil
	}
	if run != nil {
		run.cancel()
		<-run.done
		m.speech = nil
	}
	return run
}

func (m *appModel) handleStopSpeak() (tea.Model, tea.Cmd) {
	run := m.stopSpeech()
	if run == nil {
		return m, nil
	}
	return m, tea.Batch(run.editor.SetRecording(false), notification.SuccessCmd("Stopped listening"))
}
