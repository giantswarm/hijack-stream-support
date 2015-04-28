package support

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	neturl "net/url"
	"strings"

	docker "github.com/giantswarm/hijack-stream-support/docker"
)

type HijackHttpOptions struct {
	Method             string
	Url                string
	Host               string // If set, this will be passed as `Host` header to the request.
	DockerTermProtocol bool
	InputStream        io.Reader
	ErrorStream        io.Writer
	OutputStream       io.Writer
	Data               interface{}
	Header             http.Header
	Log                docker.Logger
}

var (
	ErrMissingMethod = errors.New("Method not set")
	ErrMissingUrl    = errors.New("Url not set")
)

// HijackHttpRequest performs an HTTP  request with given method, url and data and hijacks the request (after a successful connection) to stream
// data from/to the given input, output and error streams.
func HijackHttpRequest(options HijackHttpOptions) error {
	if options.Log == nil {
		// Make sure there is always a logger
		options.Log = &logIgnore{}
	}
	if options.Method == "" {
		return ErrMissingMethod
	}
	if options.Url == "" {
		return ErrMissingUrl
	}

	req, err := createHijackHttpRequest(options)
	if err != nil {
		return err
	}

	// Parse URL for endpoint data
	ep, err := neturl.Parse(options.Url)
	if err != nil {
		return err
	}

	protocol := ep.Scheme
	address := ep.Path
	if protocol != "unix" {
		protocol = "tcp"
		address = ep.Host
		if !strings.Contains(address, ":") {
			if ep.Scheme == "https" {
				address = address + ":443"
			} else {
				address = address + ":80"
			}
		}
	}

	// Dial the server
	var dial net.Conn
	//fmt.Printf("Dialing %s %s\n", protocol, address)
	if ep.Scheme == "https" {
		config := &tls.Config{}
		dial, err = docker.TLSDial(protocol, address, config)
		if err != nil {
			fmt.Printf("TLS Dialing %s %s failed %#v\n", protocol, address, err)
			return err
		}
	} else {
		dial, err = net.Dial(protocol, address)
		if err != nil {
			fmt.Printf("Dialing %s %s failed %#v\n", protocol, address, err)
			return err
		}
	}

	// Start initial HTTP connection
	clientconn := httputil.NewClientConn(dial, nil)
	defer clientconn.Close()

	clientconn.Do(req)

	// Hijack HTTP connection
	rwc, br := clientconn.Hijack()
	defer rwc.Close()

	// Stream data
	return streamData(rwc, br, options)
}

// createHijackHttpRequest creates an upgradable HTTP request according to the given options
func createHijackHttpRequest(options HijackHttpOptions) (*http.Request, error) {
	var params io.Reader
	if options.Data != nil {
		buf, err := json.Marshal(options.Data)
		if err != nil {
			return nil, err
		}
		params = bytes.NewBuffer(buf)
	}

	req, err := http.NewRequest(options.Method, options.Url, params)
	if err != nil {
		return nil, err
	}
	if options.Header != nil {
		for k, values := range options.Header {
			req.Header.Del(k)
			for _, v := range values {
				req.Header.Set(k, v)
			}
		}
	}
	req.Header.Set("Content-Type", "text/plain")
	//req.Header.Set("Connection", "Upgrade")
	//req.Header.Set("Upgrade", "tcp")
	if options.Host != "" {
		req.Host = options.Host
	}
	return req, nil
}

// streamData copies both input/output/error streams to/from the hijacked streams
func streamData(rwc io.Writer, br io.Reader, options HijackHttpOptions) error {
	errsIn := make(chan error, 1)
	errsOut := make(chan error, 1)
	exit := make(chan bool)

	go func() {
		defer close(exit)
		defer close(errsOut)
		var err error
		stdout := options.OutputStream
		if stdout == nil {
			stdout = ioutil.Discard
		}
		stderr := options.ErrorStream
		if stderr == nil {
			stderr = ioutil.Discard
		}
		if !options.DockerTermProtocol {
			// When TTY is ON, use regular copy
			_, err = io.Copy(stdout, br)
		} else {
			_, err = docker.StdCopy(stdout, stderr, br, options.Log)
		}
		errsOut <- err
	}()
	go func() {
		defer close(errsIn)
		var err error
		in := options.InputStream
		if in != nil {
			_, err = io.Copy(rwc, in)
		}
		if err := rwc.(closeWriter).CloseWrite(); err != nil {
			options.Log.Debugf("CloseWrite failed %#v", err)
		}
		errsIn <- err
	}()
	<-exit
	select {
	case err := <-errsOut:
		return err
	case err := <-errsIn:
		return err
	}
}

// ----------------------------------------------
// private interface supporting CloseWrite calls.

type closeWriter interface {
	CloseWrite() error
}

// ----------------------------------------------
// Helper to ignore debug los in case we got no logger

type logIgnore struct {
}

func (this *logIgnore) Debugf(msg string, args ...interface{}) {
	// Ignore the log message
}
