// probe opens connections that misbehave in one specific way (idle, silent,
// slow headers, slow body, slow reader) and measures how many seconds the
// server keeps each one open before closing it. Detection is done by watching
// this socket's own TCP state in /proc/net/tcp*: CLOSE_WAIT (server sent FIN)
// or the socket vanishing (server sent RST) both mean "server closed it".
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/http2"
)

var (
	target = flag.String("target", "srv:8443", "server address")
	sni    = flag.String("sni", "localhost", "TLS SNI / Host")
	modes  = flag.String("modes", "idle-h1,idle-h2,silent,slowheader,slowbody,slowread", "modes to run")
	n      = flag.Int("n", 2, "connections per mode")
	max    = flag.Duration("max", 420*time.Second, "give up waiting after this long")
)

func main() {
	flag.Parse()
	var wg sync.WaitGroup
	var mu sync.Mutex
	res := map[string][]string{}
	for _, m := range strings.Split(*modes, ",") {
		for i := 0; i < *n; i++ {
			wg.Add(1)
			go func(m string) {
				defer wg.Done()
				r := run(m)
				mu.Lock()
				res[m] = append(res[m], r)
				mu.Unlock()
			}(m)
		}
	}
	wg.Wait()
	keys := make([]string, 0, len(res))
	for k := range res {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sort.Strings(res[k])
		fmt.Printf("%-11s %s\n", k, strings.Join(res[k], "  "))
	}
}

func dial(alpn string, smallRcv bool) (*tls.Conn, int, error) {
	d := net.Dialer{Timeout: 10 * time.Second}
	if smallRcv {
		d.Control = func(_, _ string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				_ = setSmallRcvBuf(fd)
			})
		}
	}
	raw, err := d.Dial("tcp", *target)
	if err != nil {
		return nil, 0, err
	}
	port := raw.LocalAddr().(*net.TCPAddr).Port
	tc := tls.Client(raw, &tls.Config{ServerName: *sni, InsecureSkipVerify: true, NextProtos: []string{alpn}})
	if err := tc.Handshake(); err != nil {
		raw.Close()
		return nil, 0, err
	}
	return tc, port, nil
}

// closedByServer reports whether our socket with this local port is in
// CLOSE_WAIT (08) / CLOSE (07) / LAST_ACK (09), or no longer exists.
func closedByServer(port int) bool {
	hex := fmt.Sprintf(":%04X", port)
	found := false
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, ln := range strings.Split(string(b), "\n")[1:] {
			fs := strings.Fields(ln)
			if len(fs) < 4 || !strings.HasSuffix(fs[1], hex) {
				continue
			}
			found = true
			switch fs[3] {
			case "07", "08", "09":
				return true
			}
		}
	}
	return !found
}

func wait(port int, start time.Time, tick func()) string {
	last := time.Now()
	for time.Since(start) < *max {
		if closedByServer(port) {
			return strconv.Itoa(int(time.Since(start).Seconds())) + "s"
		}
		if tick != nil && time.Since(last) >= 10*time.Second {
			last = time.Now()
			tick()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return ">" + max.String()
}

func run(mode string) string {
	switch mode {
	case "idle-h1":
		tc, port, err := dial("http/1.1", false)
		if err != nil {
			return "ERR:" + err.Error()
		}
		defer tc.Close()
		fmt.Fprintf(tc, "GET /get HTTP/1.1\r\nHost: %s\r\n\r\n", *sni)
		resp, err := http.ReadResponse(bufio.NewReader(tc), nil)
		if err != nil {
			return "ERR:" + err.Error()
		}
		io.Copy(io.Discard, resp.Body)
		return wait(port, time.Now(), nil)
	case "idle-h2":
		tc, port, err := dial("h2", false)
		if err != nil {
			return "ERR:" + err.Error()
		}
		defer tc.Close()
		cc, err := (&http2.Transport{}).NewClientConn(tc)
		if err != nil {
			return "ERR:" + err.Error()
		}
		req, _ := http.NewRequest("GET", "https://"+*sni+"/get", nil)
		resp, err := cc.RoundTrip(req)
		if err != nil {
			return "ERR:" + err.Error()
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return wait(port, time.Now(), nil)
	case "silent":
		tc, port, err := dial("h2", false)
		if err != nil {
			return "ERR:" + err.Error()
		}
		defer tc.Close()
		return wait(port, time.Now(), nil)
	case "slowheader":
		tc, port, err := dial("http/1.1", false)
		if err != nil {
			return "ERR:" + err.Error()
		}
		defer tc.Close()
		fmt.Fprintf(tc, "GET /get HTTP/1.1\r\nHost: %s\r\n", *sni)
		i := 0
		return wait(port, time.Now(), func() { i++; fmt.Fprintf(tc, "X-Slow-%d: a\r\n", i) })
	case "slowbody":
		tc, port, err := dial("http/1.1", false)
		if err != nil {
			return "ERR:" + err.Error()
		}
		defer tc.Close()
		fmt.Fprintf(tc, "POST /post HTTP/1.1\r\nHost: %s\r\nContent-Type: text/plain\r\nContent-Length: 100000\r\n\r\n", *sni)
		return wait(port, time.Now(), func() { tc.Write([]byte("a")) })
	case "slowread":
		tc, port, err := dial("http/1.1", true)
		if err != nil {
			return "ERR:" + err.Error()
		}
		defer tc.Close()
		fmt.Fprintf(tc, "GET /static/big.bin HTTP/1.1\r\nHost: %s\r\n\r\n", *sni)
		// never read: the server's writes stall once buffers fill
		return wait(port, time.Now(), nil)
	}
	return "unknown mode"
}
