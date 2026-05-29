package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Handler struct {
	cfg    Config
	logger *slog.Logger
	proxy  *httputil.ReverseProxy
}

func NewHandler(cfg Config, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4096,
		MaxIdleConnsPerHost:   1024,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(cfg.UpstreamURL)
			pr.Out.Host = cfg.UpstreamURL.Host
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Sub2API-Fast-Proxy", "1")
		},
		Transport:     transport,
		BufferPool:    newBufferPool(32 << 10),
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Warn("proxy upstream error", "method", r.Method, "path", r.URL.Path, "error", err)
			writeJSONError(w, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		},
	}

	return &Handler{cfg: cfg, logger: logger, proxy: rp}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := normalizePath(r.URL.Path)
	if path == "/healthz" {
		h.writeHealth(w)
		return
	}

	if _, ok := h.cfg.AnthropicFastPaths[path]; ok {
		mergeHeaderToken(r.Header, "anthropic-beta", h.cfg.AnthropicFastBeta)
	}

	if _, ok := h.cfg.OpenAIJSONPaths[path]; ok && methodCanHaveBody(r.Method) {
		if err := h.forceServiceTier(w, r); err != nil {
			h.logger.Warn("request rejected", "method", r.Method, "path", r.URL.Path, "error", err)
			return
		}
	}

	h.proxy.ServeHTTP(w, r)
}

func (h *Handler) forceServiceTier(w http.ResponseWriter, r *http.Request) error {
	if encoding := strings.TrimSpace(r.Header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		if h.cfg.StrictInjection {
			writeJSONError(w, http.StatusUnsupportedMediaType, "compressed_request_not_supported", "Compressed request bodies cannot be forced to fast mode")
			return fmt.Errorf("unsupported content-encoding %q", encoding)
		}
		return nil
	}

	if r.Body == nil || r.Body == http.NoBody {
		if h.cfg.StrictInjection {
			writeJSONError(w, http.StatusBadRequest, "missing_body", "Request body is required for fast mode injection")
			return errors.New("missing request body")
		}
		return nil
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.cfg.MaxBodyBytes))
	closeErr := r.Body.Close()
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds proxy limit")
			return err
		}
		writeJSONError(w, http.StatusBadRequest, "read_body_failed", "Failed to read request body")
		return err
	}
	if closeErr != nil {
		h.logger.Debug("request body close failed", "error", closeErr)
	}

	updated, _, err := forceTopLevelStringProperty(body, "service_tier", h.cfg.ForceServiceTier)
	if err != nil {
		if h.cfg.StrictInjection {
			writeJSONError(w, http.StatusBadRequest, "invalid_json_body", err.Error())
			return err
		}
		updated = body
	}

	r.Body = io.NopCloser(bytes.NewReader(updated))
	r.ContentLength = int64(len(updated))
	r.Header.Set("Content-Length", strconv.Itoa(len(updated)))
	if strings.TrimSpace(r.Header.Get("Content-Type")) == "" {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("X-Sub2API-Fast-Service-Tier", h.cfg.ForceServiceTier)
	return nil
}

func (h *Handler) writeHealth(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":              true,
		"upstream":        h.cfg.UpstreamURL.String(),
		"service_tier":    h.cfg.ForceServiceTier,
		"openai_paths":    sortedKeys(h.cfg.OpenAIJSONPaths),
		"anthropic_paths": sortedKeys(h.cfg.AnthropicFastPaths),
	})
}

func methodCanHaveBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

func mergeHeaderToken(header http.Header, name, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	existing := header.Get(name)
	for _, part := range strings.Split(existing, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return
		}
	}
	if strings.TrimSpace(existing) == "" {
		header.Set(name, token)
		return
	}
	header.Set(name, existing+","+token)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

type bufferPool struct {
	size int
	pool sync.Pool
}

func newBufferPool(size int) *bufferPool {
	return &bufferPool{
		size: size,
		pool: sync.Pool{
			New: func() any {
				return make([]byte, size)
			},
		},
	}
}

func (p *bufferPool) Get() []byte {
	return p.pool.Get().([]byte)
}

func (p *bufferPool) Put(buf []byte) {
	if cap(buf) < p.size {
		return
	}
	p.pool.Put(buf[:p.size])
}
