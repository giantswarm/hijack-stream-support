package support

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	neturl "net/url"

	docker "github.com/giantswarm/hijack-stream-support/docker"
)

type HijackHttpOptions struct {
	Method             string
	Url                string
	Success            chan struct{}
	DockerTermProtocol bool
	InputStream        io.Reader
	ErrorStream        io.Writer
	OutputStream       io.Writer
	Data               interface{}
	Header             http.Header
	Log                docker.Logger
}

// HijackHttpRequest performs an HTTP  request with given method, url and data and hijacks the request (after a successful connection) to stream
// data from/to the given input, output and error streams.
func HijackHttpRequest(options HijackHttpOptions) error {
	if options.Log == nil {
		// Make sure there is always a logger
		options.Log = &logIgnore{}
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
	}

	// Dial the server
	var dial net.Conn
	dial, err = net.Dial(protocol, address)
	if err != nil {
		return err
	}

	// Start initial HTTP connection
	clientconn := httputil.NewClientConn(dial, nil)
	defer clientconn.Close()

	clientconn.Do(req)

	// Hijack HTTP connection
	success := options.Success
	if success != nil {
		success <- struct{}{}
		<-success
	}

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
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "tcp")
	return req, nil
}

// streamData copies both input/output/error streams to/from the hijacked streams
func streamData(rwc io.Writer, br io.Reader, options HijackHttpOptions) error {
	errs := make(chan error, 2)
	exit := make(chan bool)

	go func() {
		defer close(exit)
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
		errs <- err
	}()
	go func() {
		var err error
		in := options.InputStream
		if in != nil {
			_, err = io.Copy(rwc, in)
		}
		if err := rwc.(closeWriter).CloseWrite(); err != nil {
			options.Log.Debugf("CloseWrite failed %#v", err)
		}
		errs <- err
	}()
	<-exit
	return <-errs
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
