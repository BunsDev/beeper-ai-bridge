package utils

import (
	"sync"

	ai "github.com/beeper/ai-bridge/pkg/ai"
)

type EventStream[T any, R any] struct {
	events chan T
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	result R
}

func NewEventStream[T any, R any]() *EventStream[T, R] {
	return &EventStream[T, R]{events: make(chan T, 64), done: make(chan struct{})}
}

func (s *EventStream[T, R]) Events() <-chan T {
	return s.events
}

func (s *EventStream[T, R]) Push(event T) {
	select {
	case <-s.done:
		return
	default:
		s.events <- event
	}
}

func (s *EventStream[T, R]) End(result R) {
	s.once.Do(func() {
		s.mu.Lock()
		s.result = result
		s.mu.Unlock()
		close(s.done)
		close(s.events)
	})
}

func (s *EventStream[T, R]) Result() R {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result
}

type AssistantMessageEventStream struct {
	*EventStream[ai.AssistantMessageEvent, ai.Message]
}

func NewAssistantMessageEventStream() *AssistantMessageEventStream {
	return &AssistantMessageEventStream{EventStream: NewEventStream[ai.AssistantMessageEvent, ai.Message]()}
}

func CreateAssistantMessageEventStream() *AssistantMessageEventStream {
	return NewAssistantMessageEventStream()
}

func (s *AssistantMessageEventStream) Push(event ai.AssistantMessageEvent) {
	if event.Type == "done" && event.Message != nil {
		s.EventStream.Push(event)
		s.End(*event.Message)
		return
	}
	if event.Type == "error" && event.Error != nil {
		s.EventStream.Push(event)
		s.End(*event.Error)
		return
	}
	s.EventStream.Push(event)
}
