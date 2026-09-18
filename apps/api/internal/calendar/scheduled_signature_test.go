package calendar

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
)

type schedulerCertTransport func(*http.Request) (*http.Response, error)

func (f schedulerCertTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSchedulerChecksRealJWTSignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	jwks, _ := json.Marshal(map[string]any{"keys": []any{map[string]string{"kid": "synthetic", "kty": "RSA", "alg": "RS256", "use": "sig", "n": encode(key.N.Bytes()), "e": encode(big.NewInt(int64(key.E)).Bytes())}}})
	client := &http.Client{Transport: schedulerCertTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://www.googleapis.com/oauth2/v3/certs" {
			t.Errorf("unexpected trust source: %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Cache-Control": []string{"max-age=3600"}}, Body: io.NopCloser(strings.NewReader(string(jwks)))}, nil
	})}
	validator, err := idtoken.NewValidator(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	s := &batchStub{}
	h := schedulerFixture(s, io.Discard)
	h.validate = validator.Validate
	p := validSchedulerPayload(h.config)
	claims := map[string]any{"iss": p.Issuer, "aud": p.Audience, "sub": p.Subject, "iat": p.IssuedAt, "exp": p.Expires, "email": p.Claims["email"], "email_verified": true}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "synthetic"})
	body, _ := json.Marshal(claims)
	message := encode(header) + "." + encode(body)
	sum := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	for _, valid := range []bool{true, false} {
		token := message + "." + encode(sig)
		if !valid {
			damaged := append([]byte(nil), sig...)
			damaged[0] ^= 1
			token = message + "." + encode(damaged)
		}
		r := schedulerRequest()
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 401
		if valid {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("valid=%v status=%d", valid, w.Code)
		}
	}
	if s.calls != 1 {
		t.Fatal("forged JWT reached worker")
	}
}
