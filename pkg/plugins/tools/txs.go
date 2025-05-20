package tools

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/qpoint-io/qtap/pkg/plugins"
)

type Headers interface {
	Get(key string) (plugins.HeaderValue, bool)
	Values(key string, iter func(value plugins.HeaderValue))
	Set(key, value string)
	Remove(key string)
	All() map[string]string
}

type HeaderMap struct {
	headers Headers
}

func NewHeaderMap(hs Headers) *HeaderMap {
	return &HeaderMap{
		headers: hs,
	}
}

func (h HeaderMap) Get(key string) (string, bool) {
	if h.headers == nil {
		return "", false
	}
	v, ok := h.headers.Get(key)
	if ok {
		return v.String(), ok
	}
	return "", false
}

func (h HeaderMap) Sprint() string {
	sb := strings.Builder{}
	for k, v := range h.headers.All() {
		sb.WriteString(fmt.Sprintf("Header: %s = Value: %s\n", k, v))
	}

	return sb.String()
}

func (h HeaderMap) UserAgent() (string, bool) {
	return h.Get("User-Agent")
}

func (h HeaderMap) Path() (string, bool) {
	return h.Get(":path")
}

func (h HeaderMap) Method() (string, bool) {
	return h.Get(":method")
}

func (h HeaderMap) Authority() (string, bool) {
	return h.Get(":authority")
}

func (h HeaderMap) Scheme() (string, bool) {
	return h.Get(":scheme")
}

func (h HeaderMap) Status() (int, bool) {
	s, ok := h.Get(":status")
	if !ok {
		return http.StatusUnprocessableEntity, false
	}

	status, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		log.Printf("converting '%s' to an int\n", s)
		return http.StatusUnprocessableEntity, false
	}

	if status <= 0 {
		return http.StatusUnprocessableEntity, false
	}

	return int(status), true
}

func (h HeaderMap) ContentType() (string, bool) {
	return h.Get("Content-Type")
}

func (h HeaderMap) QpointRequestID() (string, bool) {
	return h.Get("qpoint-request-id")
}

func (h HeaderMap) MimeCategory() (string, bool) {
	ct, ok := h.ContentType()
	if !ok {
		return "", ok
	}

	return MimeCategory(ct), ok
}

func (h HeaderMap) URL() (string, bool) {
	s, sok := h.Scheme()
	if !sok {
		return "", sok
	}

	a, hok := h.Authority()
	if !hok {
		return "", hok
	}

	p, pok := h.Path()
	if !pok {
		return "", pok
	}

	return buildAuthorityURL(s, a, p)
}

func (h HeaderMap) RulePairs(prefix string) map[string]any {
	rulePairs := make(map[string]any)

	if h.headers == nil {
		return rulePairs
	}

	for k, v := range h.headers.All() {
		key := strings.ToLower(k)
		if strings.HasPrefix(key, ":") {
			rulePairs[prefix+"."+strings.TrimPrefix(key, ":")] = v
		} else {
			rulePairs[prefix+".header."+key] = v
		}
	}

	// full url
	if url, ok := h.URL(); ok {
		rulePairs[prefix+".url"] = url
	}

	// host (just a different name for authority)
	if host, ok := h.Authority(); ok {
		rulePairs[prefix+".host"] = host
	}

	if _, ok := rulePairs["response.status"]; ok {
		if status, ok := h.Status(); ok {
			rulePairs[prefix+".status"] = status
		}
	}

	return rulePairs
}

func buildAuthorityURL(s string, a string, p string) (string, bool) {
	if s == "" {
		s = "http"
	}

	if !strings.HasPrefix(a, s+"://") {
		a = s + "://" + a
	}

	u, err := url.Parse(a)
	if err != nil {
		log.Printf("parsing authority %s: %v\n", a, err)
		return "", false
	}

	u = u.JoinPath(p)

	return u.String(), true
}

func (h HeaderMap) BinaryContentType() bool {
	contentType, _ := h.ContentType()

	binaryTypes := []string{
		"octet-stream",
		"application/pdf",
		"image/",
		"audio/",
		"video/",
		"application/zip",
		"application/x-gzip",
	}

	for _, binaryType := range binaryTypes {
		if strings.Contains(contentType, binaryType) {
			return true
		}
	}
	return false
}

func MetadataRulePairs(md map[string]plugins.MetadataValue) map[string]any {
	rulePairs := make(map[string]any)

	if md == nil {
		return rulePairs
	}

	for k, v := range md {
		if !v.OK() {
			continue
		}

		rulePairs[strings.TrimPrefix(k, "process-")] = v.String()
	}

	return rulePairs
}
