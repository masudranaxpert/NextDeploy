// loadclient opens N long-lived TLS connections to a server and keeps them
// active (one small request every -every), like Cloudflare edges holding
// connections to an origin. It prints how many connections are alive.
package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
)

var (
	alive, failed, reqs, reqErr atomic.Int64
	errMu                       sync.Mutex
	errKinds                    = map[string]int{}
	start                       = time.Now()
)

// noteErr records why a connection died, with the connection's age bucket.
func noteErr(stage string, err error, born time.Time) {
	age := time.Since(born).Truncate(5 * time.Second)
	msg := err.Error()
	if len(msg) > 90 {
		msg = msg[:90]
	}
	errMu.Lock()
	errKinds[fmt.Sprintf("%s | age~%v | %s", stage, age, msg)]++
	errMu.Unlock()
}

func printErrs() {
	errMu.Lock()
	defer errMu.Unlock()
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range errKinds {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
	for i, e := range all {
		if i == 8 {
			break
		}
		fmt.Printf("   err x%d: %s\n", e.v, e.k)
	}
}

func main() {
	target := flag.String("target", "127.0.0.1:8443", "server addr")
	host := flag.String("host", "localhost", "SNI / Host header")
	n := flag.Int("n", 20000, "connections")
	proto := flag.String("proto", "h2", "h2 or h1")
	rate := flag.Int("rate", 1000, "new connections per second")
	every := flag.Duration("every", 20*time.Second, "request interval per connection")
	perIP := flag.Int("perip", 10000, "connections per source IP")
	basePort := flag.Int("baseport", 20000, "first source port")
	bind := flag.Bool("bind", false, "bind explicit 127.0.0.x source addresses (Windows loopback)")
	flag.Parse()

	go func() {
		n := 0
		for range time.Tick(5 * time.Second) {
			fmt.Printf("[%5.0fs] alive=%d failed=%d reqs=%d reqErr=%d\n",
				time.Since(start).Seconds(), alive.Load(), failed.Load(), reqs.Load(), reqErr.Load())
			if n++; n%6 == 0 {
				printErrs()
			}
		}
	}()

	tick := time.NewTicker(time.Second / time.Duration(*rate))
	for i := 0; i < *n; i++ {
		<-tick.C
		var laddr *net.TCPAddr
		if *bind {
			laddr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, byte(2+i / *perIP)), Port: *basePort + i%*perIP}
		}
		go worker(*target, *host, *proto, laddr, *every)
	}
	fmt.Println("all connections launched")
	select {}
}

func worker(target, host, proto string, laddr *net.TCPAddr, every time.Duration) {
	d := net.Dialer{Timeout: 10 * time.Second}
	jitter := time.Duration(time.Now().UnixNano() % 1000)
	if laddr != nil {
		d.LocalAddr = laddr
		jitter = time.Duration(laddr.Port % 1000)
	}
	raw, err := d.Dial("tcp", target)
	if err != nil {
		failed.Add(1)
		if failed.Load() <= 3 {
			fmt.Fprintln(os.Stderr, "dial:", err)
		}
		return
	}
	alpn := []string{"h2"}
	if proto == "h1" {
		alpn = []string{"http/1.1"}
	}
	tc := tls.Client(raw, &tls.Config{ServerName: host, InsecureSkipVerify: true, NextProtos: alpn})
	born := time.Now()
	if err := tc.Handshake(); err != nil {
		failed.Add(1)
		noteErr("handshake", err, born)
		raw.Close()
		return
	}
	alive.Add(1)
	defer alive.Add(-1)
	defer tc.Close()

	if proto == "h2" {
		// preface + SETTINGS go out right away, like a real client (Cloudflare)
		cc, err := (&http2.Transport{}).NewClientConn(tc)
		if err != nil {
			noteErr("h2-setup", err, born)
			return
		}
		first := true
		for {
			req, _ := http.NewRequest("GET", "https://"+host+"/", nil)
			resp, err := cc.RoundTrip(req)
			if err != nil {
				reqErr.Add(1)
				noteErr("request", err, born)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			reqs.Add(1)
			if first {
				// spread later requests out so they don't all fire at once
				first = false
				time.Sleep(jitter * every / 1000)
				continue
			}
			time.Sleep(every)
		}
	}
	time.Sleep(jitter * every / 1000)
	br := bufio.NewReader(tc)
	for {
		fmt.Fprintf(tc, "GET / HTTP/1.1\r\nHost: %s\r\n\r\n", host)
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			reqErr.Add(1)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		reqs.Add(1)
		time.Sleep(every)
	}
}
