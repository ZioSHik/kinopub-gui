package gui

import (
	"context"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

// guiChooser implements domain.AudioChooser by surfacing the available audio
// tracks to the web UI (through the job's PendingAudio field) and blocking until
// the user answers via the REST endpoint, or the timeout elapses (keep all).
type guiChooser struct {
	mgr *JobManager
	job *Job
}

func newGUIChooser(mgr *JobManager, job *Job) *guiChooser {
	return &guiChooser{mgr: mgr, job: job}
}

var _ domain.AudioChooser = (*guiChooser)(nil)

// ChooseAudio publishes the track list to the UI and waits for a selection.
// A nil/empty result means "keep all tracks".
func (c *guiChooser) ChooseAudio(tracks []domain.AudioTrackInfo, timeout time.Duration) ([]int, error) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	answer := make(chan []int, 1)

	c.job.mu.Lock()
	c.job.audioAnswer = answer
	c.job.pendingAudio = &AudioRequestView{
		Tracks:         tracks,
		TimeoutSeconds: int(timeout / time.Second),
		DeadlineUnix:   time.Now().Add(timeout).Unix(),
	}
	done := c.job.done
	c.job.mu.Unlock()
	c.mgr.publishNow(c.job)

	defer func() {
		c.job.mu.Lock()
		c.job.pendingAudio = nil
		c.job.audioAnswer = nil
		c.job.mu.Unlock()
		c.mgr.publishNow(c.job)
	}()

	select {
	case sel := <-answer:
		return sel, nil
	case <-done:
		// Job canceled while waiting — stop blocking immediately instead of
		// hanging until the timeout. The engine treats the error as "keep all".
		return nil, context.Canceled
	case <-time.After(timeout):
		return nil, nil // timed out → keep all tracks
	}
}

// answerAudio delivers the user's selection to a blocked ChooseAudio call.
// Returns false if the job has no pending audio request.
func (m *JobManager) answerAudio(id string, indices []int) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	ch := j.audioAnswer
	j.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- indices:
		return true
	default:
		return false
	}
}
