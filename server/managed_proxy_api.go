package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Autumn-27/artex/managedproxy"
)

func (s *Server) getManagedProxyPool(w http.ResponseWriter, r *http.Request) {
	if s.m.proxyPool == nil {
		writeErr(w, 503, "managed proxy pool not initialized")
		return
	}
	writeJSON(w, 200, s.m.proxyPool.Status())
}
func (s *Server) putManagedProxyPool(w http.ResponseWriter, r *http.Request) {
	if s.m.proxyPool == nil {
		writeErr(w, 503, "managed proxy pool not initialized")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var cfg managedproxy.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeErr(w, 400, "invalid managed proxy pool JSON: "+err.Error())
		return
	}
	if err := s.m.SetManagedProxyPool(cfg); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, s.m.proxyPool.Status())
}
func (s *Server) checkManagedProxyPool(w http.ResponseWriter, r *http.Request) {
	if s.m.proxyPool == nil {
		writeErr(w, 503, "managed proxy pool not initialized")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(managedproxy.MaxProxies)*2*time.Second)
	defer cancel()
	s.m.proxyPool.Check(ctx)
	writeJSON(w, 200, s.m.proxyPool.Status())
}
