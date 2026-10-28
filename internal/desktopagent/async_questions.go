package desktopagent

func (a *agent) enableAsyncQuestions() {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	if a.questionsEnabled {
		return
	}
	a.questionsEnabled = true
	for threadID, state := range a.cacheSubs {
		if state.latestThread != nil {
			a.enqueueCacheSnapshot(threadID, state.streamID, state.latestSeq, state.latestThread)
		}
	}
}
