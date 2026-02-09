package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

type forwardedMetadata struct {
	forChain []netip.Addr
	host     string
	proto    string
}

func TrustedProxyMiddleware(cfg *Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := GetHTTPContext(r)
		remoteIP, err := remoteAddrIP(r.RemoteAddr)
		if err != nil {
			rejectProxyRequest(w, r, "invalid_remote_addr", r.RemoteAddr)
			return
		}

		if isHealthCheckRequest(r) && prefixContains(cfg.HealthCheckSources, remoteIP) {
			ctx.ClientIP = remoteIP
			ctx.ClientKey = "health"
			next.ServeHTTP(w, r)
			return
		}

		if cfg.IsDevelopment {
			ctx.ClientIP = remoteIP
			ctx.ClientKey = clientKey(remoteIP)
			next.ServeHTTP(w, r)
			return
		}

		if !prefixContains(cfg.TrustedProxies, remoteIP) {
			rejectProxyRequest(w, r, "untrusted_proxy_source", remoteIP.String())
			return
		}

		meta, err := parseForwardedMetadata(r)
		if err != nil {
			rejectProxyRequest(w, r, "invalid_forwarded_metadata", remoteIP.String())
			return
		}
		if meta.proto != "https" {
			rejectProxyRequest(w, r, "forwarded_proto_not_https", remoteIP.String())
			return
		}
		publicOrigin, publicOriginHost, err := forwardedPublicOrigin(meta.host)
		if err != nil {
			rejectProxyRequest(w, r, "invalid_forwarded_origin", remoteIP.String())
			return
		}
		if cfg.PublicOriginHost != "" && !strings.EqualFold(publicOriginHost, cfg.PublicOriginHost) {
			rejectProxyRequest(w, r, "forwarded_host_mismatch", remoteIP.String())
			return
		}
		clientIP, err := forwardedClientIP(meta.forChain, cfg.TrustedProxies)
		if err != nil {
			rejectProxyRequest(w, r, "invalid_forwarded_chain", remoteIP.String())
			return
		}

		ctx.ClientIP = clientIP
		ctx.ClientKey = clientKey(clientIP)
		ctx.PublicOrigin = publicOrigin
		if cfg.PublicOrigin != "" {
			ctx.PublicOrigin = cfg.PublicOrigin
		}
		next.ServeHTTP(w, r)
	})
}

func forwardedPublicOrigin(host string) (string, string, error) {
	return parsePublicOrigin("https://"+host, false)
}

func rejectProxyRequest(w http.ResponseWriter, r *http.Request, reason, remote string) {
	logSecurityEvent("proxy_rejected", "route", routePattern(r), "reason", reason, "remote", remote, "status", http.StatusBadRequest)
	writeErrorResponse(w, r, http.StatusBadRequest, "invalid_request")
}

func isHealthCheckRequest(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL != nil && r.URL.Path == "/healthz"
}

func remoteAddrIP(remoteAddr string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, err
	}
	return addr.Unmap(), nil
}

func prefixContains(prefixes []netip.Prefix, addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func clientKey(addr netip.Addr) string {
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}

func parseForwardedMetadata(r *http.Request) (forwardedMetadata, error) {
	forwardedHeader := strings.TrimSpace(r.Header.Get("Forwarded"))
	xFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	xProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	xHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))

	if forwardedHeader != "" {
		return forwardedMetadata{}, errors.New("forwarded header is not accepted")
	}
	return parseXForwardedHeaders(xFor, xProto, xHost)
}

func parseXForwardedHeaders(xFor, xProto, xHost string) (forwardedMetadata, error) {
	if xFor == "" || xProto == "" || xHost == "" {
		return forwardedMetadata{}, errors.New("missing x-forwarded metadata")
	}

	forChain, err := parseForwardedForChain(xFor)
	if err != nil {
		return forwardedMetadata{}, err
	}
	protoValues := commaValues(xProto)
	hostValues := commaValues(xHost)
	if len(protoValues) == 0 || len(hostValues) == 0 {
		return forwardedMetadata{}, errors.New("empty x-forwarded metadata")
	}
	proto := strings.ToLower(protoValues[0])
	host := hostValues[0]
	for _, value := range protoValues[1:] {
		if strings.ToLower(value) != proto {
			return forwardedMetadata{}, errors.New("conflicting x-forwarded proto")
		}
	}
	for _, value := range hostValues[1:] {
		if !strings.EqualFold(value, host) {
			return forwardedMetadata{}, errors.New("conflicting x-forwarded host")
		}
	}

	return forwardedMetadata{forChain: forChain, proto: proto, host: host}, nil
}

func parseForwardedForChain(raw string) ([]netip.Addr, error) {
	values := commaValues(raw)
	if len(values) == 0 {
		return nil, errors.New("empty forwarded chain")
	}
	chain := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		addr, err := parseForwardedAddr(value)
		if err != nil {
			return nil, err
		}
		chain = append(chain, addr)
	}
	return chain, nil
}

func parseForwardedAddr(raw string) (netip.Addr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "_") || strings.EqualFold(raw, "unknown") {
		return netip.Addr{}, fmt.Errorf("invalid forwarded address %q", raw)
	}
	if strings.HasPrefix(raw, "[") {
		end := strings.Index(raw, "]")
		if end < 0 {
			return netip.Addr{}, fmt.Errorf("invalid forwarded address %q", raw)
		}
		suffix := raw[end+1:]
		if suffix != "" {
			if !strings.HasPrefix(suffix, ":") {
				return netip.Addr{}, fmt.Errorf("invalid forwarded address %q", raw)
			}
			if _, err := strconv.Atoi(suffix[1:]); err != nil {
				return netip.Addr{}, fmt.Errorf("invalid forwarded address %q: %w", raw, err)
			}
		}
		raw = raw[1:end]
	} else if host, port, err := net.SplitHostPort(raw); err == nil {
		if _, err := strconv.Atoi(port); err != nil {
			return netip.Addr{}, err
		}
		raw = host
	}

	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, err
	}
	return addr.Unmap(), nil
}

func commaValues(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func forwardedClientIP(chain []netip.Addr, trustedProxies []netip.Prefix) (netip.Addr, error) {
	for i := len(chain) - 1; i >= 0; i-- {
		addr := chain[i].Unmap()
		if prefixContains(trustedProxies, addr) {
			continue
		}
		return addr, nil
	}
	return netip.Addr{}, errors.New("no untrusted client address")
}
