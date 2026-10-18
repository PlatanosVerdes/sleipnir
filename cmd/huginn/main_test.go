package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestFallsBackAndCaches(t *testing.T) {
	var calls atomic.Int32
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer down.Close()
	info := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"ip":"5.6.7.8","country":"TR","city":"Istanbul","org":"AS1 NordVPN"}`))
	}))
	defer info.Close()
	l := &lookup{client: http.DefaultClient, ttl: time.Minute, ipwhoURL: down.URL, ipinfoURL: info.URL}

	p := l.get()
	if p.Error != "" || p.City != "Istanbul" || p.Source != "ipinfo.io" {
		t.Fatalf("place = %+v", p)
	}
	l.get()
	if calls.Load() != 1 {
		t.Errorf("asked %d times inside the cache window", calls.Load())
	}
}
