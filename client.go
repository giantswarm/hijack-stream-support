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
}

// HijackHttpRequest performs an HTTP  request with given method, url and data and hijacks the request (after a successful connection) to stream
// data from/to the given input, output and error streams.
func HijackHttpRequest(options HijackHttpOptions) error {
	var params io.Reader
	if options.Data != nil {
		buf, err := json.Marshal(options.Data)
		if err != nil {
			return err
		}
		params = bytes.NewBuffer(buf)
	}

	stdout := options.OutputStream
	if stdout == nil {
		stdout = ioutil.Discard
	}
	stderr := options.ErrorStream
	if stderr == nil {
		stderr = ioutil.Discard
	}
	req, err := http.NewRequest(options.Method, options.Url, params)
	if err != nil {
		return err
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

	var dial net.Conn
	dial, err = net.Dial(protocol, address)
	if err != nil {
		return err
	}

	clientconn := httputil.NewClientConn(dial, nil)
	defer clientconn.Close()

	clientconn.Do(req)
	success := options.Success
	if success != nil {
		success <- struct{}{}
		<-success
	}

	rwc, br := clientconn.Hijack()
	defer rwc.Close()
	errs := make(chan error, 2)
	exit := make(chan bool)

	go func() {
		defer close(exit)
		var err error
		if !options.DockerTermProtocol {
			// When TTY is ON, use regular copy
			_, err = io.Copy(stdout, br)
		} else {
			_, err = dockerCopy(stdout, stderr, br)
		}
		errs <- err
	}()
	go func() {
		var err error
		in := options.InputStream
		if in != nil {
			_, err = io.Copy(rwc, in)
		}
		rwc.(interface {
			CloseWrite() error
		}).CloseWrite()
		errs <- err
	}()
	<-exit
	return <-errs
}
