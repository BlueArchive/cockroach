// Copyright 2020 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package sqlproxyccl

import (
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/jackc/pgproto3/v2"
)

// BackendDial is an example backend dialer that does a TCP/IP connection
// to a backend, SSL and forwards the start message. It is defined as a variable
// so it can be redirected for testing.
//
// BackendDial uses a dial timeout of 5 seconds to mitigate network black
// holes.
//
// TODO(jaylim-crl): Move dialer into connector in the future.
var BackendDial = func(
	msg *pgproto3.StartupMessage, serverAddress string, tlsConfig *tls.Config,
) (_ net.Conn, retErr error) {
	// TODO(JeffSwenson): This behavior may need to change once multi-region
	// multi-tenant clusters are supported. The fixed timeout may need to be
	// replaced by an adaptive timeout or the timeout could be replaced by
	// speculative retries.
	conn, err := net.DialTimeout("tcp", serverAddress, time.Second*5)
	if err != nil {
		return nil, withCode(
			errors.Wrap(err, "unable to reach backend SQL server"),
			codeBackendDialFailed)
	}

	// Ensure that conn is closed whenever BackendDial returns an error.
	defer func() {
		if retErr != nil {
			conn.Close()
		}
	}()

	// Try to upgrade the PG connection to use SSL.
	if tlsConfig != nil {
		// Send SSLRequest.
		if err := binary.Write(conn, binary.BigEndian, pgSSLRequest); err != nil {
			return nil, withCode(
				errors.Wrap(err, "sending SSLRequest to target server"),
				codeBackendDialFailed)
		}
		response := make([]byte, 1)
		if _, err = io.ReadFull(conn, response); err != nil {
			return nil, withCode(
				errors.New("reading response to SSLRequest"),
				codeBackendDialFailed)
		}
		if response[0] != pgAcceptSSLRequest {
			return nil, withCode(
				errors.New("target server refused TLS connection"),
				codeBackendDialFailed)
		}
		conn = tls.Client(conn, tlsConfig.Clone())
	}

	// Forward startup message to the backend connection.
	if _, err := conn.Write(msg.Encode(nil)); err != nil {
		return nil, withCode(
			errors.Wrapf(err, "relaying StartupMessage to target server %v", serverAddress),
			codeBackendDialFailed)
	}
	return conn, nil
}
