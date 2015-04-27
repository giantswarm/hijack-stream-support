# Helper library for hijacking HTTP streams.

This library contains helper methods for hijacking HTTP connections (on both the client side and the server side) so the connection can be used to stream data. This is used in the context of `swarm exec`. 

It is highly inspired by `docker exec`.

## Client side usage: 

```
hijackOpts := hijack.HijackHttpOptions{
    Method:             "POST",
    Url:                url,
    DockerTermProtocol: false, // If set to true, output & error stream will be multiplexed in the format of docker StdCopy, see https://github.com/docker/docker/blob/master/pkg/stdcopy/stdcopy.go
    InputStream:        myInputStream,
    OutputStream:       myOutputStream,
    ErrorStream:        myErrorStream,
    Data:               dataStructureThatWillBePostedAsJson,
    Header:             make(http.Header),
}
hijackOpts.Header.Set("User-Agent", "My user agent")
err := hijack.HijackHttpRequest(hijackOpts)
if err != nil {
    return Mask(err)
}
```

## Server side usage:

```
inStream, outStream, err := hijack.HijackServer(res)
if err != nil {
    logger.Error("Hijack server streams failed: %v", err)
    return err
}
defer hijack.CloseStreams(inStream, outStream)

// Return HTTP response header, indicating a hijacking of the stream
// Response codes inspired by docker
if _, ok := req.Header["Upgrade"]; ok {
    fmt.Fprintf(outStream, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
} else {
    fmt.Fprintf(outStream, "HTTP/1.1 200 OK\r\nContent-Type: application/vnd.docker.raw-stream\r\n\r\n")
}
// Stream data from/to inStream/outStream
```
