package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/clipboard"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// openClipboardStream starts a live session and returns a channel of the raw
// JSON bodies of every `event: clipboard` frame on its SSE stream.
func openClipboardStream(t *testing.T, srv *Server, taskID string) <-chan string {
	t.Helper()
	d := srv.db
	testutil.NoError(t, d.SetBackend("cat", config.Backend{Command: "cat"}))
	task := &model.Task{ID: taskID, Name: "clip-" + taskID, Status: model.StatusInProgress, Backend: "cat", Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(task))
	sess, err := srv.runner.Start(task, d.Config(), 24, 80, false)
	testutil.NoError(t, err)
	t.Cleanup(func() {
		_ = srv.runner.Stop(task.ID)
		<-sess.Done()
	})
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/tasks/"+taskID+"/stream", nil)
	testutil.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	out := make(chan string, 256)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 4<<20)
		isClip := false
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "event: clipboard":
				isClip = true
			case isClip && strings.HasPrefix(line, "data: "):
				out <- strings.TrimPrefix(line, "data: ")
				isClip = false
			}
		}
	}()
	return out
}

func nextClip(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case v, ok := <-ch:
		testutil.True(t, ok)
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for clipboard event")
		return ""
	}
}

func TestClipboardStream_SetEmptyVsClearVsExpiry(t *testing.T) {
	srv, _ := testServer(t)
	store := clipboard.NewWithTTL(80 * time.Millisecond)
	srv.clipboard = store
	ch := openClipboardStream(t, srv, "clip-live")
	time.Sleep(150 * time.Millisecond)

	testutil.NoError(t, store.Set("clip-live", ""))
	testutil.Equal(t, nextClip(t, ch), `{"text":""}`)

	testutil.NoError(t, store.Set("clip-live", "hello"))
	testutil.Equal(t, nextClip(t, ch), `{"text":"hello"}`)

	store.Clear("clip-live")
	testutil.Equal(t, nextClip(t, ch), `{"cleared":true}`)

	testutil.NoError(t, store.Set("clip-live", "x"))
	testutil.Equal(t, nextClip(t, ch), `{"text":"x"}`)
	time.Sleep(120 * time.Millisecond)
	_, ok := store.Get("clip-live")
	testutil.Equal(t, ok, false)
	testutil.Equal(t, nextClip(t, ch), `{"cleared":true}`)
}

func TestClipboardStream_SnapshotMatchesGetAndLive(t *testing.T) {
	srv, _ := testServer(t)
	store := clipboard.New()
	srv.clipboard = store
	testutil.NoError(t, store.Set("clip-snap", ""))
	ch := openClipboardStream(t, srv, "clip-snap")

	testutil.Equal(t, nextClip(t, ch), `{"text":""}`)

	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/clip-snap/clipboard", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.Equal(t, strings.TrimSpace(w.Body.String()), `{"text":""}`)

	testutil.NoError(t, store.Set("clip-snap", ""))
	testutil.Equal(t, nextClip(t, ch), `{"text":""}`)

	store.Clear("clip-snap")
	testutil.Equal(t, nextClip(t, ch), `{"cleared":true}`)
	w = httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", "/api/tasks/clip-snap/clipboard", ""))
	testutil.Equal(t, w.Code, http.StatusNoContent)
}

func TestClipboardStream_BurstNeverMixesPresenceAndText(t *testing.T) {
	srv, _ := testServer(t)
	store := clipboard.New()
	srv.clipboard = store
	ch := openClipboardStream(t, srv, "clip-burst")
	time.Sleep(150 * time.Millisecond)

	for i := 0; i < 200; i++ {
		switch i % 3 {
		case 0:
			testutil.NoError(t, store.Set("clip-burst", fmt.Sprintf("t%d", i)))
		case 1:
			testutil.NoError(t, store.Set("clip-burst", ""))
		default:
			store.Clear("clip-burst")
		}
	}
	got := 0
	deadline := time.After(2 * time.Second)
loop:
	for {
		select {
		case raw := <-ch:
			var m map[string]any
			testutil.NoError(t, json.Unmarshal([]byte(raw), &m))
			_, hasText := m["text"]
			_, hasCleared := m["cleared"]
			testutil.Equal(t, hasText != hasCleared, true)
			if hasCleared {
				testutil.Equal(t, len(m), 1)
			}
			got++
		case <-deadline:
			break loop
		}
	}
	testutil.True(t, got > 0)
}
