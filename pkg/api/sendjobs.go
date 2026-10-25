package api

import (
	"sync"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// sendJobRetention is how long a finished send stays readable. Long enough for
// an Organiser to come back to the tab they left it in, short enough that the names
// and addresses it lists do not sit in memory all week.
const sendJobRetention = 30 * time.Minute

// sendJob is one availability send: the thirty-odd emails an Organiser set going,
// and where they have got to.
//
// A send exists as a job rather than as the body of a request because it takes
// about a minute and a half — Gmail is throttled to one email every three
// seconds — and the browser arrives at it through an OAuth redirect. Holding the
// redirect open for ninety seconds would show a blank tab that is
// indistinguishable from a hang, so the redirect returns at once and the page
// watches this instead.
type sendJob struct {
	id string
	// The Organiser who started it. A send names every volunteer it reached and
	// every address it failed on, so it is readable only by them.
	owner   string
	mode    services.SendMode
	started time.Time

	done  int // emails attempted so far
	total int // recipients the mode selected, known once selection has run

	finished bool
	report   *services.SendReport
	err      string
}

// sendJobSnapshot is a job copied out from under the lock, so a handler can
// serialise it while the send goes on writing to the original.
type sendJobSnapshot struct {
	ID       string
	Mode     services.SendMode
	Done     int
	Total    int
	Finished bool
	Sent     []services.SentEmail
	Failed   []services.FailedEmail
	Err      string
}

// sendJobs is the in-memory register of sends. In memory and not in the
// database on purpose: a job is a progress bar for one Organiser's browser tab, not
// a fact about the rota. What a send actually changed — sent_at on each request
// — is written to Postgres as it goes, so nothing here is the only copy of
// anything.
type sendJobs struct {
	mu   sync.Mutex
	jobs map[string]*sendJob
	now  func() time.Time // injectable for tests
}

func newSendJobs() *sendJobs {
	return &sendJobs{jobs: make(map[string]*sendJob), now: time.Now}
}

// start registers a new job for Organiser, or refuses to while another send is
// still running and reports false. Expired jobs are swept on the way in: sends
// are rare, so there is nothing to gain from a timer doing it separately.
//
// One send at a time, whoever started it. A send stamps each volunteer as their
// email goes, so a second one started mid-run would mail again everybody the
// first had not reached yet (issue #149). The check and the registration share
// the lock, so two sends arriving together cannot both get through. Not keyed by
// rota: there is only ever one rota in flight to send for.
func (s *sendJobs) start(id, owner string, mode services.SendMode) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	for jobID, job := range s.jobs {
		if now.Sub(job.started) > sendJobRetention {
			delete(s.jobs, jobID)
		}
	}
	if s.runningLocked() {
		return false
	}

	s.jobs[id] = &sendJob{id: id, owner: owner, mode: mode, started: now}
	return true
}

// running reports whether a send is under way. It is the early answer, asked
// before anybody is sent to a consent screen; start asks again, because a send
// can begin while somebody else is on Google's.
func (s *sendJobs) running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runningLocked()
}

func (s *sendJobs) runningLocked() bool {
	for _, job := range s.jobs {
		if !job.finished {
			return true
		}
	}
	return false
}

// progress records how far a running send has got.
func (s *sendJobs) progress(id string, done, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[id]; ok {
		job.done, job.total = done, total
	}
}

// finish closes a job off with what the send produced. err is the failure that
// stopped the whole send, as distinct from the per-volunteer failures inside the
// report, which are not failures of the send.
func (s *sendJobs) finish(id string, report *services.SendReport, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return
	}
	job.finished = true
	job.report = report
	if err != nil {
		job.err = err.Error()
	}
	if report != nil {
		job.done = len(report.Sent) + len(report.Failed)
	}
}

// snapshot returns a copy of Organiser's job. A job belonging to a different Organiser
// is reported as absent rather than forbidden: to the caller asking, it is not
// a job that exists.
func (s *sendJobs) snapshot(id, owner string) (sendJobSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok || job.owner != owner || s.now().Sub(job.started) > sendJobRetention {
		return sendJobSnapshot{}, false
	}

	snapshot := sendJobSnapshot{
		ID:       job.id,
		Mode:     job.mode,
		Done:     job.done,
		Total:    job.total,
		Finished: job.finished,
		Err:      job.err,
		Sent:     []services.SentEmail{},
		Failed:   []services.FailedEmail{},
	}
	if job.report != nil {
		snapshot.Sent = append(snapshot.Sent, job.report.Sent...)
		snapshot.Failed = append(snapshot.Failed, job.report.Failed...)
	}
	return snapshot, true
}
