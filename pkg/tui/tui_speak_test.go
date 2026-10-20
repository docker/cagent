package tui

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/audio/transcribe"
	"github.com/docker/docker-agent/pkg/tui/components/notification"
)

type fakeTranscriber struct {
	supported bool
	err       error
	calls     atomic.Int32
	cleaned   atomic.Bool
	handler   func(context.Context, transcribe.TranscriptHandler, func(context.Context)) error
}

func (f *fakeTranscriber) IsSupported() bool { return f.supported }
func (f *fakeTranscriber) Run(ctx context.Context, handler transcribe.TranscriptHandler, ready func(context.Context)) error {
	f.calls.Add(1)
	defer f.cleaned.Store(true)
	if f.handler != nil {
		return f.handler(ctx, handler, ready)
	}
	if f.err != nil {
		return f.err
	}
	ready(ctx)
	<-ctx.Done()
	return nil
}

func newSpeakTestModel(t *testing.T, ft *fakeTranscriber) *appModel {
	t.Helper()
	m, _ := newTestModel(t)
	m.transcriber = ft
	m.speechCtx, m.cancelSpeech = context.WithCancel(t.Context())
	t.Cleanup(m.cleanupManagedResources)
	return m
}

func nextSpeechEvent(t *testing.T, cmd tea.Cmd) speechEvent {
	t.Helper()
	require.NotNil(t, cmd)
	event, ok := cmd().(speechEvent)
	require.True(t, ok)
	return event
}

func TestHandleStartSpeak_NoOpIfAlreadyRunning(t *testing.T) {
	t.Parallel()
	ft := &fakeTranscriber{}
	m := newSpeakTestModel(t, ft)
	_, cmd := m.handleStartSpeak()
	event := nextSpeechEvent(t, cmd)
	assert.True(t, event.ready)
	_, cmd = m.handleStartSpeak()
	assert.Nil(t, cmd)
	assert.EqualValues(t, 1, ft.calls.Load())
	m.stopSpeech()
	assert.True(t, ft.cleaned.Load())
}

func TestHandleStartSpeak_ReturnsErrorNotification(t *testing.T) {
	t.Parallel()
	m := newSpeakTestModel(t, &fakeTranscriber{err: errors.New("boom")})
	_, cmd := m.handleStartSpeak()
	event := nextSpeechEvent(t, cmd)
	assert.True(t, event.ended)
	_, cmd = m.handleSpeechEvent(event)
	assert.False(t, m.isTranscribing())
	msgs := collectMsgs(cmd)
	var found bool
	for _, msg := range msgs {
		if show, ok := msg.(notification.ShowMsg); ok {
			found = true
			assert.Equal(t, notification.TypeError, show.Type)
			assert.Contains(t, show.Text, "boom")
		}
	}
	assert.True(t, found)
}

func TestHandleStopSpeak_NoOpWhenNotRunning(t *testing.T) {
	t.Parallel()
	m := newSpeakTestModel(t, &fakeTranscriber{})
	_, cmd := m.handleStopSpeak()
	assert.Nil(t, cmd)
}

func TestHandleStopSpeak_CancelsBlockedNotification(t *testing.T) {
	t.Parallel()
	ft := &fakeTranscriber{}
	m := newSpeakTestModel(t, ft)
	_, _ = m.handleStartSpeak() // No consumer for the readiness notification.
	run := m.speech
	_, cmd := m.handleStopSpeak()
	require.NotNil(t, cmd)
	assert.False(t, m.isTranscribing())
	assert.True(t, ft.cleaned.Load())
	_, stale := m.handleSpeechEvent(speechEvent{run: run, ready: true})
	assert.Nil(t, stale)
}

func TestSpeech_IgnoresPreviousRun(t *testing.T) {
	t.Parallel()
	m := newSpeakTestModel(t, &fakeTranscriber{})
	_, _ = m.handleStartSpeak()
	previous := m.stopSpeech()
	_, _ = m.handleStartSpeak()
	_, cmd := m.handleSpeechEvent(speechEvent{run: previous, delta: "stale"})
	assert.Nil(t, cmd)
	_, cmd = m.handleSpeechEvent(speechEvent{run: previous, ended: true})
	assert.Nil(t, cmd)
	assert.True(t, m.isTranscribing())
}

func TestSpeech_ShutdownJoinsCapture(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		ft := &fakeTranscriber{handler: func(ctx context.Context, _ transcribe.TranscriptHandler, ready func(context.Context)) error {
			ready(ctx)
			<-ctx.Done()
			<-release
			return nil
		}}
		m := newSpeakTestModel(t, ft)
		_, cmd := m.handleStartSpeak()
		nextSpeechEvent(t, cmd)
		done := make(chan struct{})
		go func() {
			defer close(done)
			m.cleanupManagedResources()
		}()
		synctest.Wait()
		assert.False(t, ft.cleaned.Load())
		select {
		case <-done:
			t.Fatal("shutdown returned before capture cleanup")
		default:
		}
		close(release)
		<-done
		assert.True(t, ft.cleaned.Load())
		_, cmd = m.handleStartSpeak()
		assert.Nil(t, cmd)
	})
}

func TestSpeech_ParentCancellation(t *testing.T) {
	t.Parallel()
	ft := &fakeTranscriber{}
	m := newSpeakTestModel(t, ft)
	_, cmd := m.handleStartSpeak()
	event := nextSpeechEvent(t, cmd)
	m.cancelSpeech()
	ended := nextSpeechEvent(t, waitForSpeech(event.run))
	assert.True(t, ended.ended)
	assert.True(t, ft.cleaned.Load())
}

type speechEditor struct {
	mockEditor

	text      string
	recording bool
}

func (e *speechEditor) InsertText(text string) { e.text += text }
func (e *speechEditor) SetRecording(recording bool) tea.Cmd {
	e.recording = recording
	return nil
}

func TestSpeech_StaysOnOriginTab(t *testing.T) {
	t.Parallel()
	ft := &fakeTranscriber{handler: func(ctx context.Context, handler transcribe.TranscriptHandler, ready func(context.Context)) error {
		ready(ctx)
		handler(ctx, "dictation")
		<-ctx.Done()
		return nil
	}}
	m := newSpeakTestModel(t, ft)
	origin := &speechEditor{}
	m.activeTab.editor = origin
	_, cmd := m.handleStartSpeak()
	ready := nextSpeechEvent(t, cmd)
	_, _ = m.handleSpeechEvent(ready)
	assert.True(t, origin.recording)

	other := &speechEditor{}
	m.activeTab = &tabModel{editor: other}
	delta := nextSpeechEvent(t, waitForSpeech(ready.run))
	_, _ = m.handleSpeechEvent(delta)
	assert.Equal(t, "dictation", origin.text)
	assert.Empty(t, other.text)
	m.handleStopSpeak()
	assert.False(t, origin.recording)
	assert.False(t, other.recording)
}

func TestSpeech_StopWhileStarting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ft := &fakeTranscriber{handler: func(ctx context.Context, _ transcribe.TranscriptHandler, _ func(context.Context)) error {
			<-ctx.Done()
			return nil
		}}
		m := newSpeakTestModel(t, ft)
		_, cmd := m.handleStartSpeak()
		m.handleStopSpeak()
		assert.True(t, nextSpeechEvent(t, cmd).ended)
		assert.False(t, m.isTranscribing())
		assert.True(t, ft.cleaned.Load())
	})
}

func TestSpeech_RuntimeFailureClearsRecording(t *testing.T) {
	t.Parallel()
	ft := &fakeTranscriber{handler: func(ctx context.Context, _ transcribe.TranscriptHandler, ready func(context.Context)) error {
		ready(ctx)
		return errors.New("connection lost")
	}}
	m := newSpeakTestModel(t, ft)
	ed := &speechEditor{}
	m.activeTab.editor = ed
	_, cmd := m.handleStartSpeak()
	ready := nextSpeechEvent(t, cmd)
	_, _ = m.handleSpeechEvent(ready)
	assert.True(t, ed.recording)
	ended := nextSpeechEvent(t, waitForSpeech(ready.run))
	_, cmd = m.handleSpeechEvent(ended)
	assert.False(t, ed.recording)
	assert.False(t, m.isTranscribing())
	require.NotEmpty(t, collectMsgs(cmd))
}

func TestSpeech_DeltaUsesRunContext(t *testing.T) {
	t.Parallel()
	ft := &fakeTranscriber{handler: func(ctx context.Context, handler transcribe.TranscriptHandler, _ func(context.Context)) error {
		runCtx, cancel := context.WithCancel(ctx)
		cancel()
		handler(runCtx, "ignored")
		return errors.New("capture failed")
	}}
	m := newSpeakTestModel(t, ft)
	_, _ = m.handleStartSpeak()
	select {
	case <-m.speech.done:
	case <-time.After(5 * time.Second):
		t.Fatal("transcript callback ignored internal cancellation")
	}
}

func TestSpeech_ExitSafetyNetCoversBlockedCleanup(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	ft := &fakeTranscriber{handler: func(ctx context.Context, _ transcribe.TranscriptHandler, ready func(context.Context)) error {
		ready(ctx)
		<-ctx.Done()
		<-release
		return nil
	}}
	m := newSpeakTestModel(t, ft)
	t.Cleanup(func() { close(release) })
	_, cmd := m.handleStartSpeak()
	nextSpeechEvent(t, cmd)
	m.program = tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(io.Discard))
	defer m.program.Kill()
	exited := make(chan struct{}, 1)
	m.exitFunc = func(int) { exited <- struct{}{} }
	m.shutdownTimeout = 10 * time.Millisecond
	m.cleanupAll()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("speech cleanup blocked the exit safety net")
	}
}

func TestSpeech_ServerFailureBeforeReadyConsumed(t *testing.T) {
	t.Parallel()
	ft := &fakeTranscriber{handler: func(ctx context.Context, _ transcribe.TranscriptHandler, ready func(context.Context)) error {
		ready(ctx)
		return errors.New("server disconnected")
	}}
	m := newSpeakTestModel(t, ft)
	_, _ = m.handleStartSpeak()
	select {
	case <-m.speech.done:
	case <-time.After(5 * time.Second):
		t.Fatal("readiness delivery blocked reporting failure")
	}
}

func TestSpeech_FailedRunCancelsReadyBehindTranscript(t *testing.T) {
	t.Parallel()
	ft := &fakeTranscriber{handler: func(ctx context.Context, handler transcribe.TranscriptHandler, ready func(context.Context)) error {
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		handler(runCtx, "queued before ready")
		cancel()
		ready(runCtx)
		return errors.New("server failed")
	}}
	m := newSpeakTestModel(t, ft)
	_, _ = m.handleStartSpeak()
	select {
	case <-m.speech.done:
	case <-time.After(5 * time.Second):
		t.Fatal("readiness ignored internal cancellation behind a queued transcript")
	}
}

func TestSpeech_DrainsFinalDeltaBeforeCompletion(t *testing.T) {
	t.Parallel()
	run := &speechRun{done: make(chan struct{}), events: make(chan speechEvent, 1)}
	run.events <- speechEvent{run: run, delta: "final"}
	close(run.done)
	assert.Equal(t, "final", nextSpeechEvent(t, waitForSpeech(run)).delta)
	assert.True(t, nextSpeechEvent(t, waitForSpeech(run)).ended)
}
