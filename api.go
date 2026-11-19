package main

// Sharesight API client. Read-only by design: the credentials Sharesight issues can also write,
// so this file only ever makes GET requests to the API (plus the OAuth token POST).

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const apiBase = "https://api.sharesight.com"

var (
	home      = os.Getenv("HOME")
	credsPath = filepath.Join(home, ".config/sharesight/credentials")
	cacheDir  = filepath.Join(home, ".cache/stonks")
	dataDir   = filepath.Join(home, ".local/share/stonks")
	cacheTTL  = 10 * time.Minute
	fresh     bool // --fresh: skip the response cache
	httpc     = &http.Client{Timeout: 45 * time.Second}
)

func loadCreds() (id, secret string, err error) {
	f, err := os.Open(credsPath)
	if err != nil {
		return "", "", fmt.Errorf("can't read %s: %w", credsPath, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "SHARESIGHT_CLIENT_ID":
			id = v
		case "SHARESIGHT_CLIENT_SECRET":
			secret = v
		}
	}
	if id == "" || secret == "" || strings.HasPrefix(id, "paste-") {
		return "", "", fmt.Errorf("client id/secret missing in %s", credsPath)
	}
	return id, secret, nil
}

type tokenFile struct {
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
}

func token() (string, error) {
	tp := filepath.Join(cacheDir, "token.json")
	var t tokenFile
	if b, err := os.ReadFile(tp); err == nil && json.Unmarshal(b, &t) == nil && time.Until(t.Expires) > time.Minute {
		return t.Token, nil
	}
	id, secret, err := loadCreds()
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {id}, "client_secret": {secret}}
	resp, err := httpc.PostForm(apiBase+"/oauth2/token", form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var r struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil || r.AccessToken == "" {
		return "", fmt.Errorf("sharesight token request failed (HTTP %d)", resp.StatusCode)
	}
	if r.ExpiresIn == 0 {
		r.ExpiresIn = 1800
	}
	t = tokenFile{r.AccessToken, time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)}
	os.MkdirAll(cacheDir, 0o700)
	b, _ := json.Marshal(t)
	os.WriteFile(tp, b, 0o600)
	return t.Token, nil
}

// get fetches an /api/v3 path (GET only) into out, using a 10-minute disk cache.
func get(path string, q url.Values, out any) error {
	u := apiBase + "/api/v3/" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	sum := sha256.Sum256([]byte(u))
	cp := filepath.Join(cacheDir, "http", hex.EncodeToString(sum[:8])+".json")
	if !fresh {
		if st, err := os.Stat(cp); err == nil && time.Since(st.ModTime()) < cacheTTL {
			if b, err := os.ReadFile(cp); err == nil && json.Unmarshal(b, out) == nil {
				return nil
			}
		}
	}
	var body []byte
	for attempt := 0; ; attempt++ {
		tok, err := token()
		if err != nil {
			return err
		}
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := httpc.Do(req)
		if err != nil {
			return err
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			os.Remove(filepath.Join(cacheDir, "token.json"))
			continue
		}
		// Sharesight answers 403 when requests come too fast; back off and retry.
		if (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) && attempt < 5 {
			time.Sleep(time.Duration(1<<attempt) * time.Second)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("sharesight %s: HTTP %d: %.200s", path, resp.StatusCode, body)
		}
		break
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("sharesight %s: %w", path, err)
	}
	os.MkdirAll(filepath.Dir(cp), 0o700)
	os.WriteFile(cp, body, 0o600)
	return nil
}
