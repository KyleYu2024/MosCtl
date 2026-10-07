package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"testing"

	"github.com/KyleYu2024/mosctl/internal/diagnostics"
	"github.com/miekg/dns"
)

func TestRankingGroupsDomainAndKeepsRejectedSeparate(t *testing.T) {
	previous := diagnostics.QueryStats
	diagnostics.QueryStats = &diagnostics.Collector{}
	defer func() { diagnostics.QueryStats = previous }()
	for i, label := range []string{"LOCAL", "REJECTED", "REMOTE"} {
		diagnostics.QueryStats.Consume(fmt.Sprintf("time\tINFO\tMOSCTL_STATS_%s\t{\"uqid\":%d,\"qname\":\"same.example.\"}", label, i))
		diagnostics.QueryStats.Consume(fmt.Sprintf("time\tINFO\tMOSCTL_STATS_ALL\t{\"uqid\":%d,\"qname\":\"same.example.\",\"qtype\":1,\"rcode\":0}", i))
	}
	s, _ := newTestServer(t)
	handler := s.Handler()
	cookie := loginCookie(t, handler)
	for _, tc := range []struct {
		route string
		count uint64
	}{{"all", 3}, {"rejected", 1}} {
		r := httptest.NewRequest("GET", "/api/stats?route="+tc.route, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var result struct {
			Ranking []rankingRow `json:"ranking"`
			Total   int          `json:"ranking_total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Total != 1 || len(result.Ranking) != 1 || result.Ranking[0].Count != tc.count {
			t.Fatalf("wrong ranking: %s", w.Body.String())
		}
	}
}

func TestTypedDNSReportsResponseCodeAndRecords(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &dns.Server{PacketConn: conn, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		reply := new(dns.Msg)
		reply.SetReply(r)
		if r.Question[0].Qtype == dns.TypeHTTPS {
			reply.Rcode = dns.RcodeNameError
		} else {
			rr, _ := dns.NewRR(r.Question[0].Name + " 60 IN A 192.0.2.10")
			reply.Answer = []dns.RR{rr}
		}
		_ = w.WriteMsg(reply)
	})}
	go server.ActivateAndServe()
	defer server.Shutdown()
	s, _ := newTestServer(t)
	s.dnsAddress = conn.LocalAddr().String()
	handler := s.Handler()
	cookie := loginCookie(t, handler)
	for _, tc := range []struct {
		typ, rcode string
		ok         bool
	}{{"A", "NOERROR", true}, {"HTTPS", "NXDOMAIN", false}} {
		body, _ := json.Marshal(map[string]string{"domain": "example.com", "type": tc.typ})
		r := httptest.NewRequest("POST", "/api/test/custom", bytes.NewReader(body))
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var result struct {
			Type    string   `json:"type"`
			Rcode   string   `json:"rcode"`
			OK      bool     `json:"ok"`
			Records []string `json:"records"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || result.Type != tc.typ || result.Rcode != tc.rcode || result.OK != tc.ok {
			t.Fatalf("wrong DNS result: %s", w.Body.String())
		}
		if tc.ok && len(result.Records) != 1 {
			t.Fatal("answer records lost")
		}
	}
}
