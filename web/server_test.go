package web

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/test"
)

// TestServerServesApp boots the webapp component on a free port and checks
// that it serves the embedded index.html with the credential cookies set.
func TestServerServesApp(t *testing.T) {
	t.Setenv("VIAM_MACHINE_FQDN", "test-main.viam.cloud")
	t.Setenv("VIAM_API_KEY_ID", "key-id")
	t.Setenv("VIAM_API_KEY", "key")

	l, err := net.Listen("tcp", "127.0.0.1:0")
	test.That(t, err, test.ShouldBeNil)
	port := l.Addr().(*net.TCPAddr).Port
	test.That(t, l.Close(), test.ShouldBeNil)

	conf := resource.Config{
		Name:                "webapp",
		API:                 generic.API,
		Model:               Model,
		ConvertedAttributes: &Config{Port: &port},
	}
	srv, err := NewServer(context.Background(), nil, conf, logging.NewTestLogger(t))
	test.That(t, err, test.ShouldBeNil)
	defer srv.Close(context.Background())

	var resp *http.Response
	for i := 0; i < 50; i++ {
		resp, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	test.That(t, err, test.ShouldBeNil)
	defer resp.Body.Close()
	test.That(t, resp.StatusCode, test.ShouldEqual, http.StatusOK)

	body, err := io.ReadAll(resp.Body)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, strings.Contains(string(body), "<title>Portrait Drawing</title>"), test.ShouldBeTrue)

	cookies := map[string]string{}
	for _, c := range resp.Cookies() {
		cookies[c.Name] = c.Value
	}
	test.That(t, cookies["host"], test.ShouldEqual, "test-main.viam.cloud")
	test.That(t, cookies["api-key-id"], test.ShouldEqual, "key-id")
	test.That(t, cookies["api-key"], test.ShouldEqual, "key")
	test.That(t, cookies["is_local"], test.ShouldEqual, "true")

	// The bundle referenced by index.html must be served too.
	start := strings.Index(string(body), "./assets/")
	test.That(t, start, test.ShouldBeGreaterThan, 0)
	asset := string(body)[start+2:]
	asset = asset[:strings.IndexAny(asset, `"'`)]
	resp2, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/%s", port, asset))
	test.That(t, err, test.ShouldBeNil)
	defer resp2.Body.Close()
	test.That(t, resp2.StatusCode, test.ShouldEqual, http.StatusOK)
}
