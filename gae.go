package mallory

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"time"
)

// all write on this should be sync between threads
type EngineGAE struct {
	// Global config
	Env *Env
	// work space for this engine
	Work string
	// place store certificates
	CertDir string
	// Loaded certificate, contains the root certificate and private key
	RootCA *tls.Certificate
	// Pool of auto generated fake certificates signed by RootCert
	Certs *CertPool
}

// Create and initialize
func CreateEngineGAE(e *Env) (self *EngineGAE, err error) {
	self = &EngineGAE{Env: e}
	self.Work = path.Join(e.Work, "gae")
	self.CertDir = path.Join(self.Work, "certs")

	err = os.MkdirAll(self.CertDir, 0755)
	if err != nil && !os.IsExist(err) {
		return
	}

	rcert, err := tls.LoadX509KeyPair(self.Env.Cert, self.Env.Key)
	if err != nil {
		return
	}
	self.RootCA = &rcert

	self.Certs = NewCertPool(self.CertDir, &rcert)
	return
}

// Data flow:
//  1. Receive client request R1
//  2. Write R1 as the body of a new request R2
//  3. Post request R2 to remote GAE
//  4. Receive response P1 from GAE
//  5. Read remote server(which the client want to connect with) resonse P2 from the body of P1
//  6. Send P2 as the response to client
func (self *EngineGAE) Serve(s *Session) {
	w, r := s.ResponseWriter, s.Request
	if r.Method == "CONNECT" {
		s.Error("this function can not handle CONNECT method")
		return
	}
	start := time.Now()

	// write the client request and post to remote
	// Note: WriteProxy keeps the full request URI
	var buf bytes.Buffer
	if err := r.WriteProxy(&buf); err != nil {
		s.Error("WriteProxy: %s", err.Error())
		return
	}

	// use httpS to keep all things secure,
	// the second phase of CONNECT also uses this.
	url := fmt.Sprintf("https://%s.appspot.com/http", self.Env.AppSpot)
	// for debug
	if self.Env.AppSpot == "debug" {
		url = "http://localhost:8080/http"
	}

	// post client request as body data
	resp, err := http.Post(url, "application/data", &buf)
	if err != nil {
		s.Error("Post: %s", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusInternalServerError {
		s.Error("%s %s", r.Method, resp.Status)
		return
	}

	// the response for the requst of client
	cres, err := http.ReadResponse(bufio.NewReader(resp.Body), r)
	if err != nil {
		s.Error("ReadResponse: %s", err.Error())
		return
	}
	defer cres.Body.Close()

	// please prepare header first and write them
	CopyHeader(w, cres)
	w.WriteHeader(cres.StatusCode)

	_, err = io.Copy(w, cres.Body)
	if err != nil {
		s.Error("Copy: %s", err.Error())
		return
	}

	d := BeautifyDuration(time.Since(start))
	s.Info("RESPONSE %s %s", resp.Status, d)
}

//  Impossible to connect gae and handle it as a normal TCP connection?
//  GAE only provide http handlers? At least I don't know how to handle to TCP connection on GAE server.
//  NOTE: GAE socket service can only be available for billing users. So free users is unable to use the
//  long term connection. And do what we did in EngineDirect.
//  So we can only use urlfetch.Client.Transport.RoundTrip to do http or https method.
//  Generally, the CONNECT method can be used for any purpose for the advantage of TCP connection.
//  The proxy doesn't need to know what the real underlying protocol or what it is, just need to copy
//  data from client to server, and copy the response from the server to client without any interpret.
//  Now what we can do and had been done by some GAE proxies is that, extract the underlying protocol!!!
//  GAE can only handle limited protocols with urlfetch module, such as http and https.
//  Use Hijacker to get the underlying connection
//
// Data flow:
//  1. Detect host and port
//  2. Hijack the client connection
//  3. Dial self
//  4. Return 200 OK if is successfully
//  5. Get cached or create new signed certificate
//  6. Wrap client connection with TLS and make handshake
//  7. Receive http request
//  8. Write request as a proxy request to self, HTTP handler
//  9. Copy response to client...
func (self *EngineGAE) Connect(s *Session) {
	w, r := s.ResponseWriter, s.Request
	if r.Method != "CONNECT" {
		s.Error("this function can only handle CONNECT method")
		return
	}
	start := time.Now()

	// Only support HTTPS protocol, which is connected with port 443
	host, port, err := net.SplitHostPort(r.URL.Host)
	if err != nil {
		s.Error("SplitHostPort: %s", err.Error())
		return
	}

	if port != "443" {
		s.Error("unsupported CONNECT port: %s", port)
		return
	}

	// hijack the connection to make SSL handshake
	hij, ok := w.(http.Hijacker)
	if !ok {
		s.Error("Server does not support Hijacker")
		return
	}

	conn, _, err := hij.Hijack()
	if err != nil {
		s.Error("Hijack: %s", err.Error())
		return
	}
	defer conn.Close()

	// dial self to transport application data, http request
	rconn, err := net.Dial("tcp", self.Env.Addr)
	if err != nil {
		s.Error("Dial: %s", err.Error())
		return
	}
	defer rconn.Close()

	// Once connected successfully, return OK
	conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))

	// get the fake cert, every host should have its own cert
	cert, err := self.Certs.Get(host)
	if err != nil {
		s.Error("CertPool.Get: %s", err.Error())
		return
	}

	// assume the protocol of client connection is HTTPS
	// wrap it with TSL server
	config := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		ServerName:   host,
	}
	sconn := tls.Server(conn, config)
	defer sconn.Close()

	// The TLS connection goes here
	if err := sconn.Handshake(); err != nil {
		// re-open browser to recover the handshake error:
		//    remote error: bad certificate
		s.Error("Handshake: %s", err.Error())
		return
	}

	// finally, we are at application layer, http request comes
	// read all requests, tls connection reues?
	// Pipeline: http://www.w3.org/Protocols/rfc2616/rfc2616-sec8.html
	rch := make(chan *http.Request, 8)
	go func() {
		// close the channel after all done, notify the reader
		defer close(rch)
		for {
			creq, err := http.ReadRequest(bufio.NewReader(sconn))
			if err != nil {
				if err != io.EOF {
					s.Error("ReadRequest: %s", err.Error())
				}
				break
			}

			// should re-wrap the URL with scheme "https://"
			creq.URL, err = url.Parse("https://" + host + creq.URL.String())
			creq.Header.Set("Mallory-Session", strconv.FormatInt(s.ID, 10))

			// Now re-write the client request to self, HTTP handler
			err = creq.WriteProxy(rconn)
			if err != nil {
				s.Error("WriteProxy: %s", err.Error())
				break
			}

			// write to chan to sync
			rch <- creq

			// break if close
			if creq.Close {
				break
			}
		}
	}()

	for {
		// write back all responses
		creq, ok := <-rch
		if !ok {
			break // closed by previous sender
		}

		// responses have the same order of requests
		cresp, err := http.ReadResponse(bufio.NewReader(rconn), creq)
		if err != nil {
			s.Error("ReadResponse: %s", err.Error())
			break
		}
		defer cresp.Body.Close()

		err = cresp.Write(sconn)
		if err != nil {
			// EOF means client close the connection when writing
			if err != io.EOF {
				s.Error("Write: %s", err.Error())
			}
			break
		}

		// close the persistent connection after reply the requset
		if cresp.Close {
			break
		}
	}

	s.Info("CLOSE %s", BeautifyDuration(time.Since(start)))
}