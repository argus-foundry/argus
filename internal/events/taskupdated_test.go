package events

import (
	"encoding/json"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type captureSink struct{ got []model.Event }

func (c *captureSink) Emit(ev model.Event) { c.got = append(c.got, ev) }

func TestEmitTaskUpdated(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		fields []string
		want   []string
	}{
		{"canonical order and dedupe", "t1", []string{"pinned", "archived", "pinned"}, []string{"archived", "pinned"}},
		{"unknown dropped", "t1", []string{"bogus", "name"}, []string{"name"}},
		{"all unknown is a no-op", "t1", []string{"bogus"}, nil},
		{"no fields is a no-op", "t1", nil, nil},
		{"empty task id is a no-op", "", []string{"name"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &captureSink{}
			prev := SetSink(sink)
			t.Cleanup(func() { SetSink(prev) })
			EmitTaskUpdated(tc.id, tc.fields...)
			if tc.want == nil {
				testutil.Equal(t, len(sink.got), 0)
				return
			}
			testutil.Equal(t, len(sink.got), 1)
			ev := sink.got[0]
			testutil.Equal(t, ev.Type, model.EventTypeTaskUpdated)
			testutil.Equal(t, ev.TaskID, tc.id)
			var p struct {
				Changed []string `json:"changed_fields"`
			}
			testutil.NoError(t, json.Unmarshal(ev.Payload, &p))
			testutil.DeepEqual(t, p.Changed, tc.want)
		})
	}
}
