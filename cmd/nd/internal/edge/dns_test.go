package edge

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

type fakeDNSResolver struct {
	// records keyed by server -> qname(fqdn) -> qtype -> *dns.Msg or error
	responses map[string]map[string]map[uint16]fakeDNSReply
}

type fakeDNSReply struct {
	msg *dns.Msg
	err error
}

func newFakeDNSResolver() *fakeDNSResolver {
	return &fakeDNSResolver{
		responses: make(map[string]map[string]map[uint16]fakeDNSReply),
	}
}

func (f *fakeDNSResolver) set(server, qname string, qtype uint16, msg *dns.Msg, err error) {
	fqdn := dns.Fqdn(qname)
	if f.responses[server] == nil {
		f.responses[server] = make(map[string]map[uint16]fakeDNSReply)
	}
	if f.responses[server][fqdn] == nil {
		f.responses[server][fqdn] = make(map[uint16]fakeDNSReply)
	}
	f.responses[server][fqdn][qtype] = fakeDNSReply{msg: msg, err: err}
}

func (f *fakeDNSResolver) setBoth(qname string, qtype uint16, msg *dns.Msg, err error) {
	f.set("1.1.1.1:53", qname, qtype, msg, err)
	f.set("8.8.8.8:53", qname, qtype, msg, err)
}

func (f *fakeDNSResolver) Exchange(_ context.Context, server, qname string, qtype uint16) (*dns.Msg, error) {
	fqdn := dns.Fqdn(qname)
	if srvMap, ok := f.responses[server]; ok {
		if nameMap, ok := srvMap[fqdn]; ok {
			if rep, ok := nameMap[qtype]; ok {
				return rep.msg, rep.err
			}
		}
	}
	// Default empty NOERROR reply.
	m := new(dns.Msg)
	m.Rcode = dns.RcodeSuccess
	return m, nil
}

func msgA(host string, ips ...string) *dns.Msg {
	m := new(dns.Msg)
	m.Rcode = dns.RcodeSuccess
	for _, s := range ips {
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: dns.Fqdn(host), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   net.ParseIP(s).To4(),
		})
	}
	return m
}

func msgAAAA(host string, ips ...string) *dns.Msg {
	m := new(dns.Msg)
	m.Rcode = dns.RcodeSuccess
	for _, s := range ips {
		m.Answer = append(m.Answer, &dns.AAAA{
			Hdr:  dns.RR_Header{Name: dns.Fqdn(host), Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60},
			AAAA: net.ParseIP(s).To16(),
		})
	}
	return m
}

func msgCNAME(host, target string) *dns.Msg {
	m := new(dns.Msg)
	m.Rcode = dns.RcodeSuccess
	m.Answer = append(m.Answer, &dns.CNAME{
		Hdr:    dns.RR_Header{Name: dns.Fqdn(host), Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60},
		Target: dns.Fqdn(target),
	})
	return m
}

func msgNXDOMAIN() *dns.Msg {
	m := new(dns.Msg)
	m.Rcode = dns.RcodeNameError
	return m
}

func msgCAA(domain string, issues ...string) *dns.Msg {
	m := new(dns.Msg)
	m.Rcode = dns.RcodeSuccess
	for _, v := range issues {
		m.Answer = append(m.Answer, &dns.CAA{
			Hdr:   dns.RR_Header{Name: dns.Fqdn(domain), Rrtype: dns.TypeCAA, Class: dns.ClassINET, Ttl: 60},
			Tag:   "issue",
			Value: v,
		})
	}
	return m
}

type fakeSystemLookup struct {
	ips []netip.Addr
	err error
}

func (f fakeSystemLookup) LookupNetIP(_ context.Context, _, _ string) ([]netip.Addr, error) {
	return f.ips, f.err
}

func TestDNSGateScenarios(t *testing.T) {
	shieldIPs := []netip.Addr{netip.MustParseAddr("107.189.6.222")}
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	ctx := context.Background()

	t.Run("EDNS1_missing_record", func(t *testing.T) {
		res := newFakeDNSResolver()
		res.setBoth("missing.example.com", dns.TypeA, msgNXDOMAIN(), nil)
		res.setBoth("missing.example.com", dns.TypeAAAA, msgNXDOMAIN(), nil)
		g := NewDNSGate(DNSGateOptions{Resolver: res})

		out := g.CheckHost(ctx, "missing.example.com", shieldIPs, false, true, now)
		if out.Allowed || out.Outcome != DNSOutcomePending {
			t.Fatalf("expected pending_dns, got %+v", out)
		}
	})

	t.Run("EDNS2_points_elsewhere", func(t *testing.T) {
		res := newFakeDNSResolver()
		res.setBoth("wrong.example.com", dns.TypeA, msgA("wrong.example.com", "9.9.9.9"), nil)
		g := NewDNSGate(DNSGateOptions{Resolver: res})

		out := g.CheckHost(ctx, "wrong.example.com", shieldIPs, false, true, now)
		if out.Allowed || out.Outcome != DNSOutcomePending || !strings.Contains(out.Detail, "9.9.9.9") {
			t.Fatalf("expected pending_dns with actual IP 9.9.9.9, got %+v", out)
		}
	})

	t.Run("EDNS3_cloudflare_proxied", func(t *testing.T) {
		res := newFakeDNSResolver()
		res.setBoth("cf.example.com", dns.TypeA, msgA("cf.example.com", "104.21.10.5"), nil)
		res.setBoth("cf.example.com", dns.TypeAAAA, msgAAAA("cf.example.com", "2606:4700:3030::6815:a05"), nil)
		g := NewDNSGate(DNSGateOptions{Resolver: res})

		out := g.CheckHost(ctx, "cf.example.com", shieldIPs, false, true, now)
		if !out.Allowed || out.Outcome != DNSOutcomeOKProxied {
			t.Fatalf("expected dns_ok_proxied, got %+v", out)
		}
	})

	t.Run("EDNS5_wrong_aaaa", func(t *testing.T) {
		res := newFakeDNSResolver()
		res.setBoth("v6bad.example.com", dns.TypeA, msgA("v6bad.example.com", "107.189.6.222"), nil)
		res.setBoth("v6bad.example.com", dns.TypeAAAA, msgAAAA("v6bad.example.com", "2001:db8::99"), nil)
		g := NewDNSGate(DNSGateOptions{Resolver: res})

		out := g.CheckHost(ctx, "v6bad.example.com", shieldIPs, false, true, now)
		if out.Allowed || out.Outcome != DNSOutcomePending || !strings.Contains(out.Detail, "AAAA") {
			t.Fatalf("expected pending_dns mentioning AAAA, got %+v", out)
		}
	})

	t.Run("EDNS6_caa_forbids_letsencrypt", func(t *testing.T) {
		res := newFakeDNSResolver()
		res.setBoth("app.caa.example.com", dns.TypeA, msgA("app.caa.example.com", "107.189.6.222"), nil)
		res.setBoth("caa.example.com", dns.TypeCAA, msgCAA("caa.example.com", "digicert.com"), nil)
		g := NewDNSGate(DNSGateOptions{Resolver: res})

		out := g.CheckHost(ctx, "app.caa.example.com", shieldIPs, false, true, now)
		if out.Allowed || out.Outcome != DNSOutcomePendingCA {
			t.Fatalf("expected pending_caa, got %+v", out)
		}
	})

	t.Run("EDNS7_resolver_disagreement_keeps_pending", func(t *testing.T) {
		res := newFakeDNSResolver()
		res.set("1.1.1.1:53", "prop.example.com", dns.TypeA, msgA("prop.example.com", "107.189.6.222"), nil)
		res.set("8.8.8.8:53", "prop.example.com", dns.TypeA, msgNXDOMAIN(), nil)
		g := NewDNSGate(DNSGateOptions{Resolver: res})

		out := g.CheckHost(ctx, "prop.example.com", shieldIPs, false, true, now)
		if out.Allowed || out.Outcome != DNSOutcomePending {
			t.Fatalf("expected pending_dns on resolver disagreement, got %+v", out)
		}
	})

	t.Run("EDNS8_sticky_after_active", func(t *testing.T) {
		res := newFakeDNSResolver()
		res.setBoth("sticky.example.com", dns.TypeA, msgA("sticky.example.com", "9.9.9.9"), nil)
		g := NewDNSGate(DNSGateOptions{Resolver: res})

		out := g.CheckHost(ctx, "sticky.example.com", shieldIPs, true, true, now)
		if !out.Allowed || !out.Drift {
			t.Fatalf("expected Allowed=true and Drift=true for alreadyRendered host, got %+v", out)
		}
	})

	t.Run("EDNS14_sslip_embedded_ip", func(t *testing.T) {
		// No DNS records configured — sslip/nip uses embedded IP directly.
		g := NewDNSGate(DNSGateOptions{Resolver: newFakeDNSResolver()})
		okOut := g.CheckHost(ctx, "app-107-189-6-222.sslip.io", shieldIPs, false, true, now)
		if !okOut.Allowed || okOut.Outcome != DNSOutcomeOK {
			t.Fatalf("expected matching sslip to succeed, got %+v", okOut)
		}
		badOut := g.CheckHost(ctx, "app-1-2-3-4.nip.io", shieldIPs, false, true, now)
		if badOut.Allowed || badOut.Outcome != DNSOutcomePending {
			t.Fatalf("expected mismatched nip.io to be pending_dns, got %+v", badOut)
		}
	})

	t.Run("EDNS15_dns_gate_off", func(t *testing.T) {
		g := NewDNSGate(DNSGateOptions{Off: true, Resolver: newFakeDNSResolver()})
		out := g.CheckHost(ctx, "split.example.com", shieldIPs, false, true, now)
		if !out.Allowed || out.Outcome != DNSOutcomeOK {
			t.Fatalf("expected Allowed=true with dns-gate=off, got %+v", out)
		}
	})

	t.Run("EDNS16_public_blocked_system_fallback", func(t *testing.T) {
		res := newFakeDNSResolver()
		res.setBoth("fb.example.com", dns.TypeA, nil, errors.New("timeout"))
		res.setBoth("fb.example.com", dns.TypeAAAA, nil, errors.New("timeout"))
		g := NewDNSGate(DNSGateOptions{
			Resolver:  res,
			SysLookup: fakeSystemLookup{ips: []netip.Addr{netip.MustParseAddr("107.189.6.222")}},
		})
		out := g.CheckHost(ctx, "fb.example.com", shieldIPs, false, true, now)
		if !out.Allowed || !out.SystemFallback {
			t.Fatalf("expected Allowed=true and SystemFallback=true, got %+v", out)
		}
	})

	t.Run("EDNS17_cname_chain_to_cf_and_limit_8", func(t *testing.T) {
		res := newFakeDNSResolver()
		res.setBoth("alias.example.com", dns.TypeA, msgCNAME("alias.example.com", "cdn.cloudflare.net"), nil)
		res.setBoth("cdn.cloudflare.net", dns.TypeA, msgA("cdn.cloudflare.net", "104.16.1.1"), nil)
		g := NewDNSGate(DNSGateOptions{Resolver: res})

		out := g.CheckHost(ctx, "alias.example.com", shieldIPs, false, true, now)
		if !out.Allowed || out.Outcome != DNSOutcomeOKProxied {
			t.Fatalf("expected CNAME->CF to return dns_ok_proxied, got %+v", out)
		}

		// Chain of 10 CNAMEs (>8) should fail back to pending/error.
		resDeep := newFakeDNSResolver()
		names := []string{"c0.example.com", "c1.example.com", "c2.example.com", "c3.example.com", "c4.example.com", "c5.example.com", "c6.example.com", "c7.example.com", "c8.example.com", "c9.example.com", "c10.example.com"}
		for i := 0; i < 10; i++ {
			resDeep.setBoth(names[i], dns.TypeA, msgCNAME(names[i], names[i+1]), nil)
		}
		resDeep.setBoth("c10.example.com", dns.TypeA, msgA("c10.example.com", "107.189.6.222"), nil)
		gDeep := NewDNSGate(DNSGateOptions{Resolver: resDeep, SysLookup: fakeSystemLookup{err: errors.New("blocked")}})
		outDeep := gDeep.CheckHost(ctx, "c0.example.com", shieldIPs, false, true, now)
		if outDeep.Allowed {
			t.Fatalf("expected CNAME depth >8 to fail, got %+v", outDeep)
		}
	})
}
