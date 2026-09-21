package auth_test

import (
	"testing"

	"github.com/Bitspark/archon/sdk/go/login"
	"github.com/Bitspark/nightseam/auth/go"
)

// FuzzAudience holds the audience grammar total: no input panics, an
// accepted audience is a fixed point of the grammar, is printable ASCII,
// and is what Archon derives from the same URL with a login tail; a
// refused one is refused by Archon too.
func FuzzAudience(f *testing.F) {
	for _, url := range []string{
		"wss://API.example.test:443/nightseam",
		"ws://localhost:8080",
		"https://api.example.test:8443/a/b",
		"https://api.example.test/Team%20A/v1",
		"wss://[2001:db8::1]:443/x",
		"wss://api.example.test/nightseam/",
		"wss://api.example.test/nightseam?x=1",
		"wss://user@api.example.test/nightseam",
		"wss://api.example.test/./nightseam",
		"wss://api.example.test:0443/nightseam",
		"ftp://api.example.test/nightseam",
		"http://[::1]",
		"http://a.b:65535/%zz",
		"",
		"://",
		"http://",
	} {
		f.Add(url)
	}
	f.Fuzz(func(t *testing.T, url string) {
		audience, err := auth.Audience(url)
		theirs, _, theirErr := login.DeriveAudience(url + "/login/00")
		if err != nil {
			if theirErr == nil {
				t.Fatalf("refused %q (%v), Archon derives %q", url, err, theirs)
			}
			return
		}
		for i := 0; i < len(audience); i++ {
			if c := audience[i]; c <= 0x20 || c >= 0x7f {
				t.Fatalf("audience %q carries byte 0x%02x", audience, c)
			}
		}
		if again, err := auth.Audience(audience); err != nil || again != audience {
			t.Fatalf("audience %q is not a fixed point: %q, %v", audience, again, err)
		}
		if theirErr != nil || theirs != audience {
			t.Fatalf("derived %q from %q, Archon %q (%v)", audience, url, theirs, theirErr)
		}
		if _, err := auth.ConnectionBinding(audience); err != nil {
			t.Fatalf("audience %q binds nothing: %v", audience, err)
		}
	})
}

// TestConnectionBindingHoldsTheAudience holds that a misconfigured
// audience is refused at the binding rather than failing closed at every
// proof, and that the binding is the packet's bytes.
func TestConnectionBindingHoldsTheAudience(t *testing.T) {
	for _, bad := range []string{"", "https://API.example.test/x", "wss://api.example.test/x", "https://api.example.test/x/", "https://api.example.test:443/x"} {
		if binding, err := auth.ConnectionBinding(bad); err == nil {
			t.Errorf("audience %q bound as %x", bad, binding)
		}
		if _, err := auth.Prove(make([]byte, 32), bad, make([]byte, 32)); err == nil {
			t.Errorf("audience %q proved", bad)
		}
		if auth.Verify(make([]byte, 32), bad, make([]byte, 32), make([]byte, 64)) {
			t.Errorf("audience %q verified", bad)
		}
	}
	binding, err := auth.ConnectionBinding("http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	if want := "\x01\x00\x15http://localhost:8080"; string(binding) != want {
		t.Fatalf("binding %x, want %x", binding, want)
	}
}
