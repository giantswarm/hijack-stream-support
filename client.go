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

// HijackHttpRequest performs an HTTP  request with given method, url and data and hijacks the request (after a successful connection) to stream
// data from/to the given input, output and error streams.
func HijackHttpRequest(method, url string, success chan struct{}, dockerTermProtocol bool, in io.Reader, stderr, stdout io.Writer, data interface{}) error {
	var params io.Reader
	if data != nil {
		buf, err := json.Marshal(data)
		if err != nil {
			return err
		}
		params = bytes.NewBuffer(buf)
	}

	if stdout == nil {
		stdout = ioutil.Discard
	}
	if stderr == nil {
		stderr = ioutil.Discard
	}
	req, err := http.NewRequest(method, url, params)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "plain/text")
	ep, err := neturl.Parse(url)
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
		if !dockerTermProtocol {
			// When TTY is ON, use regular copy
			_, err = io.Copy(stdout, br)
		} else {
			_, err = dockerCopy(stdout, stderr, br)
		}
		errs <- err
	}()
	go func() {
		var err error
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
