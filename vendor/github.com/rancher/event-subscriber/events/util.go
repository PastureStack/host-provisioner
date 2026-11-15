package events

import (
	"fmt"
	"time"
)

type doneTranitioningFunc func() (bool, error)

// WaitForTransition waits for a resource transition using the event subscriber's polling settings.
func WaitForTransition(waitFunc func() (bool, error)) error {
	return waitForTransition(waitFunc)
}

func waitForTransition(waitFunc doneTranitioningFunc) error {
	timeoutAt := time.Now().Add(MaxWait)
	ticker := time.NewTicker(time.Millisecond * 250)
	defer ticker.Stop()
	for tick := range ticker.C {
		done, err := waitFunc()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if tick.After(timeoutAt) {
			return fmt.Errorf("Timed out waiting for transtion.")
		}
	}
	return fmt.Errorf("Timed out waiting for transtion.")
}
