package traffic

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestManagedUpstreamSelectionFailClosed(t *testing.T) {
	tr := &Traffic{}
	if err := tr.SetUpstreamProxy("http://127.0.0.1:9090"); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://lab.example.test/", nil)
	old, err := tr.selectUpstream(req)
	if err != nil || old.String() != "http://127.0.0.1:9090" {
		t.Fatalf("legacy upstream %v %v", old, err)
	}

	blocked := errors.New("target outside authorized pool scope")
	tr.SetUpstreamSelector(func(_ *http.Request) (*url.URL, error) { return nil, blocked })
	chosen, err := tr.selectUpstream(req)
	if chosen != nil || !errors.Is(err, blocked) {
		t.Fatalf("unexpected direct fallback: %v %v", chosen, err)
	}

	tr.SetUpstreamSelector(func(_ *http.Request) (*url.URL, error) { return url.Parse("socks5://127.0.0.1:1080") })
	chosen, err = tr.selectUpstream(req)
	if err != nil || chosen.String() != "socks5://127.0.0.1:1080" {
		t.Fatalf("pool upstream: %v %v", chosen, err)
	}

	tr.SetUpstreamSelector(nil)
	chosen, err = tr.selectUpstream(req)
	if err != nil || chosen.String() != old.String() {
		t.Fatalf("restore legacy upstream: %v %v", chosen, err)
	}
}
