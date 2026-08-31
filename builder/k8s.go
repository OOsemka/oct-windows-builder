package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type K8sClient struct {
	baseURL   string
	tokenPath string
	client    *http.Client
}

func NewK8sClient() (*K8sClient, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("KUBERNETES_SERVICE_HOST/PORT not set — not running in-cluster")
	}
	tokenPath := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	if _, err := os.Stat(tokenPath); err != nil {
		return nil, fmt.Errorf("service account token not found: %w", err)
	}
	return &K8sClient{
		baseURL:   fmt.Sprintf("https://%s:%s", host, port),
		tokenPath: tokenPath,
		client: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // in-cluster API server
					MinVersion:         tls.VersionTLS12,
				},
			},
		},
	}, nil
}

func (c *K8sClient) token() (string, error) {
	data, err := os.ReadFile(c.tokenPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *K8sClient) request(method, path string, body interface{}) ([]byte, int, error) {
	ct := ""
	if body != nil {
		ct = "application/json"
	}
	return c.do(method, path, ct, body)
}

func (c *K8sClient) do(method, path, contentType string, body interface{}) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	tok, err := c.token()
	if err != nil {
		return nil, 0, fmt.Errorf("read SA token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respData, err := io.ReadAll(resp.Body)
	return respData, resp.StatusCode, err
}

func (c *K8sClient) GetBytes(path string) ([]byte, int, error) {
	return c.request(http.MethodGet, path, nil)
}

func (c *K8sClient) Patch(path string, body interface{}) error {
	data, code, err := c.do(http.MethodPatch, path, "application/merge-patch+json", body)
	if err != nil {
		return err
	}
	if code < 200 || code >= 300 {
		return fmt.Errorf("PATCH %s → %d: %s", path, code, truncate(string(data), 300))
	}
	return nil
}

func (c *K8sClient) Get(path string) (map[string]interface{}, int, error) {
	data, code, err := c.request(http.MethodGet, path, nil)
	if err != nil {
		return nil, 0, err
	}
	if code == http.StatusNotFound {
		return nil, code, nil
	}
	if code < 200 || code >= 300 {
		return nil, code, fmt.Errorf("GET %s → %d: %s", path, code, truncate(string(data), 300))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, code, fmt.Errorf("unmarshal: %w", err)
	}
	return result, code, nil
}

func (c *K8sClient) Create(path string, obj map[string]interface{}) (map[string]interface{}, error) {
	data, code, err := c.request(http.MethodPost, path, obj)
	if err != nil {
		return nil, err
	}
	if code < 200 || code >= 300 {
		return nil, fmt.Errorf("POST %s → %d: %s", path, code, truncate(string(data), 400))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return result, nil
}

func (c *K8sClient) Put(path string, obj map[string]interface{}) (map[string]interface{}, error) {
	data, code, err := c.request(http.MethodPut, path, obj)
	if err != nil {
		return nil, err
	}
	if code < 200 || code >= 300 {
		return nil, fmt.Errorf("PUT %s → %d: %s", path, code, truncate(string(data), 400))
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return result, nil
}

func (c *K8sClient) Delete(path string) error {
	data, code, err := c.request(http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if code >= 300 && code != http.StatusNotFound {
		return fmt.Errorf("DELETE %s → %d: %s", path, code, truncate(string(data), 200))
	}
	return nil
}

func nestedMap(obj map[string]interface{}, keys ...string) map[string]interface{} {
	cur := obj
	for _, k := range keys {
		if cur == nil {
			return nil
		}
		next, _ := cur[k].(map[string]interface{})
		cur = next
	}
	return cur
}

func nestedString(obj map[string]interface{}, keys ...string) string {
	if len(keys) == 0 {
		return ""
	}
	if len(keys) == 1 {
		s, _ := obj[keys[0]].(string)
		return s
	}
	m := nestedMap(obj, keys[:len(keys)-1]...)
	if m == nil {
		return ""
	}
	s, _ := m[keys[len(keys)-1]].(string)
	return s
}

func waitPhase(c *K8sClient, path, want string, timeout time.Duration, failed []string) (string, error) {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		obj, code, err := c.Get(path)
		if err != nil {
			return last, err
		}
		if code == http.StatusNotFound {
			last = "NotFound"
			time.Sleep(5 * time.Second)
			continue
		}
		phase := nestedString(obj, "status", "phase")
		last = phase
		if phase == want {
			return phase, nil
		}
		for _, f := range failed {
			if phase == f {
				msg := conditionMessage(obj)
				return phase, fmt.Errorf("resource entered %s: %s", phase, msg)
			}
		}
		time.Sleep(8 * time.Second)
	}
	return last, fmt.Errorf("timeout waiting for phase %s (last %s)", want, last)
}

func conditionMessage(obj map[string]interface{}) string {
	st, _ := obj["status"].(map[string]interface{})
	if st == nil {
		return ""
	}
	conds, _ := st["conditions"].([]interface{})
	var parts []string
	for _, raw := range conds {
		cm, _ := raw.(map[string]interface{})
		if cm == nil {
			continue
		}
		msg, _ := cm["message"].(string)
		if msg != "" {
			parts = append(parts, msg)
		}
	}
	return strings.Join(parts, "; ")
}

func guestAgentConnected(obj map[string]interface{}) bool {
	st, _ := obj["status"].(map[string]interface{})
	if st == nil {
		return false
	}
	conds, _ := st["conditions"].([]interface{})
	for _, raw := range conds {
		cm, _ := raw.(map[string]interface{})
		if cm == nil {
			continue
		}
		t, _ := cm["type"].(string)
		s, _ := cm["status"].(string)
		if t == "AgentConnected" && s == "True" {
			return true
		}
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

func logf(format string, args ...interface{}) {
	log.Printf("[windows-builder] "+format, args...)
}
