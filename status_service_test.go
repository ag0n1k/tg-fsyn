package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// mockSynologyClient implements SynologyClient for testing.
type mockSynologyClient struct {
	mu    sync.Mutex
	tasks []Task
	err   error
	calls int32 // atomic
}

func (m *mockSynologyClient) FetchTasks() ([]Task, error) {
	atomic.AddInt32(&m.calls, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks, m.err
}

func (m *mockSynologyClient) setTasks(tasks []Task) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = tasks
}

func (m *mockSynologyClient) getCalls() int32 {
	return atomic.LoadInt32(&m.calls)
}

// mockBotSender implements BotSender for testing.
type mockBotSender struct {
	mu       sync.Mutex
	messages []tgbotapi.Chattable
}

func (m *mockBotSender) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, c)
	return tgbotapi.Message{}, nil
}

func (m *mockBotSender) getMessages() []tgbotapi.Chattable {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]tgbotapi.Chattable, len(m.messages))
	copy(copied, m.messages)
	return copied
}

func newTestService(client *mockSynologyClient, sender *mockBotSender, interval time.Duration) *StatusService {
	admins := map[int64]bool{111: true}
	return NewStatusService(client, admins, sender, interval)
}

func TestGetStatusReturnsCachedData(t *testing.T) {
	client := &mockSynologyClient{
		tasks: []Task{
			{ID: "1", Title: "File A", Status: "downloading"},
			{ID: "2", Title: "File B", Status: "finished"},
		},
	}
	sender := &mockBotSender{}
	svc := newTestService(client, sender, time.Hour)

	// Manually trigger checkStatus
	svc.checkStatus()

	tasks, lastChecked := svc.GetStatus()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Title != "File A" {
		t.Errorf("expected task title 'File A', got '%s'", tasks[0].Title)
	}
	if time.Since(lastChecked) > 2*time.Second {
		t.Errorf("lastChecked too old: %v", lastChecked)
	}
}

func TestStatusUpdatesHappenPeriodically(t *testing.T) {
	client := &mockSynologyClient{
		tasks: []Task{{ID: "1", Title: "File A", Status: "downloading"}},
	}
	sender := &mockBotSender{}
	svc := newTestService(client, sender, 50*time.Millisecond)

	svc.Start()
	defer svc.Stop()

	// Wait for initial + at least 3 ticks
	time.Sleep(220 * time.Millisecond)

	calls := client.getCalls()
	if calls < 4 {
		t.Errorf("expected at least 4 calls (1 initial + 3 ticks), got %d", calls)
	}
}

func TestTickerContinuesAfterAllTasksFinish(t *testing.T) {
	// This is the regression test for the original bug:
	// the ticker must NOT stop when all tasks are finished.
	client := &mockSynologyClient{
		tasks: []Task{
			{ID: "1", Title: "File A", Status: "finished"},
			{ID: "2", Title: "File B", Status: "finished"},
		},
	}
	sender := &mockBotSender{}
	svc := newTestService(client, sender, 50*time.Millisecond)

	svc.Start()
	defer svc.Stop()

	// Wait for several ticks
	time.Sleep(220 * time.Millisecond)

	calls := client.getCalls()
	if calls < 4 {
		t.Errorf("expected at least 4 calls even with all-finished tasks, got %d (ticker stopped!)", calls)
	}
}

func TestTickerContinuesWhenNewTasksAppear(t *testing.T) {
	client := &mockSynologyClient{
		tasks: []Task{{ID: "1", Title: "File A", Status: "finished"}},
	}
	sender := &mockBotSender{}
	svc := newTestService(client, sender, 50*time.Millisecond)

	svc.Start()
	defer svc.Stop()

	time.Sleep(120 * time.Millisecond)
	callsBefore := client.getCalls()

	// Simulate new tasks appearing
	client.setTasks([]Task{
		{ID: "1", Title: "File A", Status: "finished"},
		{ID: "2", Title: "File B", Status: "downloading"},
	})

	time.Sleep(120 * time.Millisecond)
	callsAfter := client.getCalls()

	if callsAfter <= callsBefore {
		t.Errorf("expected more calls after new tasks appeared: before=%d, after=%d", callsBefore, callsAfter)
	}
}

func TestStatusChangeNotification(t *testing.T) {
	client := &mockSynologyClient{
		tasks: []Task{{ID: "1", Title: "File A", Status: "downloading"}},
	}
	sender := &mockBotSender{}
	svc := newTestService(client, sender, time.Hour)

	// First check — establishes baseline
	svc.checkStatus()

	msgs := sender.getMessages()
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after first check, got %d", len(msgs))
	}

	// Change status
	client.setTasks([]Task{{ID: "1", Title: "File A", Status: "finished"}})
	svc.checkStatus()

	msgs = sender.getMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 notification message, got %d", len(msgs))
	}
}

func TestConcurrentAccess(t *testing.T) {
	client := &mockSynologyClient{
		tasks: []Task{{ID: "1", Title: "File A", Status: "downloading"}},
	}
	sender := &mockBotSender{}
	svc := newTestService(client, sender, 50*time.Millisecond)

	svc.Start()
	defer svc.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				svc.GetStatus()
				svc.HasRunningTasks()
				svc.FormatStatusMessage()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	// Also force concurrent checkStatus calls
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				svc.checkStatus()
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
}

func TestStopGracefulShutdown(t *testing.T) {
	client := &mockSynologyClient{
		tasks: []Task{{ID: "1", Title: "File A", Status: "downloading"}},
	}
	sender := &mockBotSender{}
	svc := newTestService(client, sender, 50*time.Millisecond)

	svc.Start()

	// Let it run a bit
	time.Sleep(120 * time.Millisecond)
	svc.Stop()

	callsAtStop := client.getCalls()

	// Wait more — calls should not increase after Stop
	time.Sleep(120 * time.Millisecond)
	callsAfterStop := client.getCalls()

	// Allow at most 1 extra call (in-flight at time of stop)
	if callsAfterStop > callsAtStop+1 {
		t.Errorf("expected calls to stop after Stop(), but got %d (was %d at stop)", callsAfterStop, callsAtStop)
	}
}

func TestFormatStatusMessage(t *testing.T) {
	client := &mockSynologyClient{
		tasks: []Task{
			{ID: "1", Title: "Big Movie", Status: "downloading", Size: 1073741824}, // 1 GB
		},
	}
	sender := &mockBotSender{}
	svc := newTestService(client, sender, time.Hour)

	svc.checkStatus()

	msg := svc.FormatStatusMessage()
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	if !containsString(msg, "Big Movie") {
		t.Error("expected message to contain task title")
	}
	if !containsString(msg, "downloading") {
		t.Error("expected message to contain task status")
	}
	if !containsString(msg, "1.00 GB") {
		t.Error("expected message to contain formatted size")
	}
}

func TestFormatStatusMessageEmpty(t *testing.T) {
	client := &mockSynologyClient{tasks: []Task{}}
	sender := &mockBotSender{}
	svc := newTestService(client, sender, time.Hour)

	svc.checkStatus()

	msg := svc.FormatStatusMessage()
	if msg != "No download tasks found." {
		t.Errorf("expected empty message, got '%s'", msg)
	}
}

func TestHasRunningTasks(t *testing.T) {
	client := &mockSynologyClient{}
	sender := &mockBotSender{}
	svc := newTestService(client, sender, time.Hour)

	// No tasks
	client.setTasks([]Task{})
	svc.checkStatus()
	if svc.HasRunningTasks() {
		t.Error("expected no running tasks with empty list")
	}

	// All finished
	client.setTasks([]Task{{ID: "1", Status: "finished"}})
	svc.checkStatus()
	if svc.HasRunningTasks() {
		t.Error("expected no running tasks when all finished")
	}

	// Some running
	client.setTasks([]Task{
		{ID: "1", Status: "finished"},
		{ID: "2", Status: "downloading"},
	})
	svc.checkStatus()
	if !svc.HasRunningTasks() {
		t.Error("expected running tasks when one is downloading")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
