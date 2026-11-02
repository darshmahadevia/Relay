package showcase

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestControlsChangeRealBackendResponses(t *testing.T) {
	demo, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer demo.Close()
	for _, scenario := range []struct {
		key    byte
		status int
		stage  string
	}{{'f', 502, "OUTAGE"}, {'r', 200, "RECOVERY"}, {'b', 200, "BURST"}, {'a', 200, "HEALTHY"}} {
		demo.Control(scenario.key)
		response, err := http.Get(demo.URL + "/v1/orders")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != scenario.status || !strings.Contains(demo.Stage(), scenario.stage) {
			t.Fatalf("key %c: status %d, stage %q", scenario.key, response.StatusCode, demo.Stage())
		}
		if scenario.status == 200 && !strings.Contains(string(body), `"path":"/v1/orders"`) {
			t.Fatalf("request did not reach the demo backend: %s", body)
		}
	}
}
