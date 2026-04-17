package githubactions

import (
	"bytes"
	"encoding/json"
	"time"
)

// TimeValue handles API fields that may be missing or null.
type TimeValue struct {
	time.Time
}

// UnmarshalJSON decodes a RFC3339 timestamp or null.
func (t *TimeValue) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		t.Time = time.Time{}
		return nil
	}

	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == "" {
		t.Time = time.Time{}
		return nil
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}
