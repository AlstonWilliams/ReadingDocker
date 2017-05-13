package restartmanager

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/docker/engine-api/types/container"
)

const (
	backoffMultiplier = 2
	defaultTimeout    = 100 * time.Millisecond
)

// ErrRestartCanceled is returned when the restart manager has been
// canceled and will no longer restart the container.
var ErrRestartCanceled = errors.New("restart canceled")

// RestartManager defines object that controls container restarting rules.
type RestartManager interface {
	Cancel() error
	ShouldRestart(exitCode uint32, hasBeenManuallyStopped bool, executionDuration time.Duration) (bool, chan error, error)
}

type restartManager struct {
	sync.Mutex
	sync.Once
	policy       container.RestartPolicy
	restartCount int
	timeout      time.Duration
	active       bool
	cancel       chan struct{}
	canceled     bool
}

// New returns a new restartmanager based on a policy.
func New(policy container.RestartPolicy, restartCount int) RestartManager {
	return &restartManager{policy: policy, restartCount: restartCount, cancel: make(chan struct{})}
}

func (rm *restartManager) SetPolicy(policy container.RestartPolicy) {
	rm.Lock()
	rm.policy = policy
	rm.Unlock()
}

/* Reading:
 *  Judge whether we should restart the container.
 *  Parameters:
 *    exitCode: exit code of container, 0 if successful
 *    hasBeenManuallyStopped: Is container stop manually? I am not sure
 *    executionDuration: How long the container has run after it start
 *
 */
func (rm *restartManager) ShouldRestart(exitCode uint32, hasBeenManuallyStopped bool, executionDuration time.Duration) (bool, chan error, error) {

	// Reading: If the RestartPolicy is no, then return false directly.
	if rm.policy.IsNone() {
		return false, nil, nil
	}

	// NoIdea: I don't know why lock here, RestartManager is container-dependent.
	// NoIdea: Is it used to prevent multiple client start a container simultaneously? I have no idea.
	rm.Lock()

	// Reading: Prevent that lock isn't released if error.
	unlockOnExit := true
	defer func() {
		if unlockOnExit {
			rm.Unlock()
		}
	}()

	if rm.canceled {
		return false, nil, ErrRestartCanceled
	}

	if rm.active {
		return false, nil, fmt.Errorf("invalid call on active restartmanager")
	}
	// if the container ran for more than 10s, regardless of status and policy reset the
	// the timeout back to the default.
	if executionDuration.Seconds() >= 10 {
		rm.timeout = 0
	}
	if rm.timeout == 0 {
		rm.timeout = defaultTimeout
	} else {
		rm.timeout *= backoffMultiplier
	}

	var restart bool

	// Reading: Decide whether restart by RestartPolicy
	switch {
	case rm.policy.IsAlways():
		restart = true
	case rm.policy.IsUnlessStopped() && !hasBeenManuallyStopped:
		restart = true
	case rm.policy.IsOnFailure():
		// the default value of 0 for MaximumRetryCount means that we will not enforce a maximum count
		if max := rm.policy.MaximumRetryCount; max == 0 || rm.restartCount < max {
			// Reading: Container exit successfully if exitCode == 0
			restart = exitCode != 0
		}
	}

	if !restart {
		rm.active = false
		return false, nil, nil
	}

	rm.restartCount++

	unlockOnExit = false
	rm.active = true
	rm.Unlock()

	// NoIdea: What the following lines do?
	ch := make(chan error)
	go func() {
		select {
		case <-rm.cancel:
			ch <- ErrRestartCanceled
			close(ch)
		case <-time.After(rm.timeout):
			rm.Lock()
			close(ch)
			rm.active = false
			rm.Unlock()
		}
	}()

	return true, ch, nil
}

func (rm *restartManager) Cancel() error {
	rm.Do(func() {
		rm.Lock()
		rm.canceled = true
		close(rm.cancel)
		rm.Unlock()
	})
	return nil
}
