package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	_ "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1" // registers the service descriptors
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
)

const (
	apiPackage        = "shogun.api.v1"
	connectEnvelopeSz = 5 // one flag byte and a four-byte length
	connectEndStream  = 0x02
	unauthenticated   = "unauthenticated"
	sweepRateLimit    = "1000"
)

// openProcedures are the only procedures that answer without a session. Logout
// must still clear the cookie of a session that has already expired.
var openProcedures = map[string]bool{apiv1connect.AuthServiceLogoutProcedure: true}

// apiProcedures lists every method of every shogun.api.v1 service the binary
// links, as "/shogun.api.v1.Service/Method" with whether it streams.
func apiProcedures(t *testing.T) map[string]bool {
	t.Helper()
	procedures := map[string]bool{}
	protoregistry.GlobalFiles.RangeFilesByPackage(apiPackage, func(file protoreflect.FileDescriptor) bool {
		for i := range file.Services().Len() {
			svc := file.Services().Get(i)
			for j := range svc.Methods().Len() {
				m := svc.Methods().Get(j)
				procedures["/"+string(svc.FullName())+"/"+string(m.Name())] = m.IsStreamingServer() || m.IsStreamingClient()
			}
		}
		return true
	})
	return procedures
}

// callWithoutSession posts an empty request, with no cookie, and returns the
// Connect error code the call ended with ("" when it succeeded).
func callWithoutSession(t *testing.T, base, procedure string, streaming bool) string {
	t.Helper()
	contentType, body := "application/json", []byte("{}")
	if streaming {
		contentType = "application/connect+json"
		body = append(make([]byte, connectEnvelopeSz), body...)
		binary.BigEndian.PutUint32(body[1:connectEnvelopeSz], 2)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, base+procedure, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", procedure, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", procedure, err)
	}
	if streaming {
		return endStreamCode(t, procedure, raw)
	}
	if resp.StatusCode == http.StatusOK {
		return ""
	}
	return errorCode(t, procedure, raw)
}

// endStreamCode reads the error code from the end-of-stream message of a
// Connect streaming response, where an interceptor's refusal arrives.
func endStreamCode(t *testing.T, procedure string, raw []byte) string {
	t.Helper()
	for len(raw) >= connectEnvelopeSz {
		flags, size := raw[0], int(binary.BigEndian.Uint32(raw[1:connectEnvelopeSz]))
		if len(raw) < connectEnvelopeSz+size {
			break
		}
		message := raw[connectEnvelopeSz : connectEnvelopeSz+size]
		raw = raw[connectEnvelopeSz+size:]
		if flags&connectEndStream != 0 {
			return errorCode(t, procedure, message)
		}
	}
	return ""
}

func errorCode(t *testing.T, procedure string, raw []byte) string {
	t.Helper()
	var wrapped struct {
		Code  string `json:"code"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		t.Fatalf("%s: not a Connect error: %q", procedure, raw)
	}
	if wrapped.Error.Code != "" {
		return wrapped.Error.Code
	}
	return wrapped.Code
}

func TestEveryPublicProcedureRejectsACallWithoutASession(t *testing.T) {
	// Arrange
	// The sweep makes more calls in a burst than the default per-client limit allows.
	base, _ := startTorii(t, map[string]string{"TORII_RATE_LIMIT": sweepRateLimit, "TORII_RATE_BURST": sweepRateLimit})
	procedures := apiProcedures(t)
	if len(procedures) < len(openProcedures) {
		t.Fatalf("found %d procedures, want every shogun.api.v1 method", len(procedures))
	}
	for open := range openProcedures {
		if _, known := procedures[open]; !known {
			t.Fatalf("open procedure %s is not a registered procedure", open)
		}
	}

	// Act and assert
	for procedure, streaming := range procedures {
		t.Run(strings.TrimPrefix(procedure, "/"), func(t *testing.T) {
			code := callWithoutSession(t, base, procedure, streaming)
			if openProcedures[procedure] {
				if code == unauthenticated {
					t.Errorf("%s is open but answered unauthenticated", procedure)
				}
				return
			}
			if code != unauthenticated {
				t.Errorf("%s without a session = %q, want %q", procedure, code, unauthenticated)
			}
		})
	}
}
