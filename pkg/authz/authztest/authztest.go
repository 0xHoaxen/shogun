// Package authztest checks that a running gRPC server asks every caller who
// they are.
package authztest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

const callTimeout = 10 * time.Second

// exemptServices are callable without identity: probes and reflection itself.
var exemptServices = []string{
	"grpc.health.v1.Health",
	"grpc.reflection.v1.ServerReflection",
	"grpc.reflection.v1alpha.ServerReflection",
}

// RequireIdentityOnEveryRPC lists the services the server at addr registers,
// through reflection, and calls every method of every service except probes and
// reflection with an empty request and no identity. Each must answer
// Unauthenticated. It fails if it finds no method to call, so a server that
// stops exposing reflection cannot pass by being invisible.
func RequireIdentityOnEveryRPC(t *testing.T, addr string) {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	results, err := sweep(conn)
	if err != nil {
		t.Fatalf("list the server's RPCs: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("the server registers no RPC besides health and reflection; nothing was checked")
	}
	for _, r := range results {
		t.Run(r.method, func(t *testing.T) {
			if status.Code(r.err) != codes.Unauthenticated {
				t.Errorf("%s without identity = %v, want Unauthenticated", r.method, r.err)
			}
		})
	}
}

// result is what one method answered to a call without identity.
type result struct {
	method string
	err    error
}

// sweep calls every method of the server behind conn without identity.
func sweep(conn *grpc.ClientConn) ([]result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	methods, err := registeredMethods(ctx, conn)
	if err != nil {
		return nil, err
	}
	results := make([]result, 0, len(methods))
	for _, m := range methods {
		callCtx, cancelCall := context.WithTimeout(context.Background(), callTimeout)
		results = append(results, result{method: string(m.FullName()), err: callWithoutIdentity(callCtx, conn, m)})
		cancelCall()
	}
	return results, nil
}

// registeredMethods returns every method of every non-exempt service.
func registeredMethods(ctx context.Context, conn *grpc.ClientConn) ([]protoreflect.MethodDescriptor, error) {
	stream, err := reflectionpb.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.CloseSend() }()
	ask := func(req *reflectionpb.ServerReflectionRequest) (*reflectionpb.ServerReflectionResponse, error) {
		if err := stream.Send(req); err != nil {
			return nil, err
		}
		return stream.Recv()
	}

	listed, err := ask(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_ListServices{ListServices: "*"},
	})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range listed.GetListServicesResponse().GetService() {
		if !slices.Contains(exemptServices, s.GetName()) {
			names = append(names, s.GetName())
		}
	}

	files := &protoregistry.Files{}
	for _, name := range names {
		if err := loadSymbol(files, ask, name); err != nil {
			return nil, fmt.Errorf("describe %s: %w", name, err)
		}
	}
	var methods []protoreflect.MethodDescriptor
	for _, name := range names {
		d, err := files.FindDescriptorByName(protoreflect.FullName(name))
		if err != nil {
			return nil, err
		}
		svc, ok := d.(protoreflect.ServiceDescriptor)
		if !ok {
			return nil, fmt.Errorf("%s is not a service", name)
		}
		for i := range svc.Methods().Len() {
			methods = append(methods, svc.Methods().Get(i))
		}
	}
	return methods, nil
}

type askFunc func(*reflectionpb.ServerReflectionRequest) (*reflectionpb.ServerReflectionResponse, error)

// loadSymbol adds the file that defines symbol, and everything it imports, to files.
func loadSymbol(files *protoregistry.Files, ask askFunc, symbol string) error {
	resp, err := ask(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: symbol},
	})
	if err != nil {
		return err
	}
	if e := resp.GetErrorResponse(); e != nil {
		return fmt.Errorf("reflection: %s", e.GetErrorMessage())
	}
	pending := map[string]*descriptorpb.FileDescriptorProto{}
	for _, raw := range resp.GetFileDescriptorResponse().GetFileDescriptorProto() {
		fd := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(raw, fd); err != nil {
			return err
		}
		pending[fd.GetName()] = fd
	}
	for len(pending) > 0 {
		progressed := false
		for name, fd := range pending {
			if !importsLoaded(files, fd) {
				continue
			}
			file, err := protodesc.NewFile(fd, files)
			if err != nil {
				return err
			}
			if err := files.RegisterFile(file); err != nil && !strings.Contains(err.Error(), "already registered") {
				return err
			}
			delete(pending, name)
			progressed = true
		}
		if !progressed {
			if err := fetchMissingImports(files, ask, pending); err != nil {
				return err
			}
		}
	}
	return nil
}

func importsLoaded(files *protoregistry.Files, fd *descriptorpb.FileDescriptorProto) bool {
	for _, dep := range fd.GetDependency() {
		if _, err := files.FindFileByPath(dep); err != nil {
			return false
		}
	}
	return true
}

// fetchMissingImports asks the server for imports of the pending files that
// reflection did not send along.
func fetchMissingImports(files *protoregistry.Files, ask askFunc, pending map[string]*descriptorpb.FileDescriptorProto) error {
	asked := false
	for _, fd := range pending {
		for _, dep := range fd.GetDependency() {
			if _, err := files.FindFileByPath(dep); err == nil {
				continue
			}
			if _, queued := pending[dep]; queued {
				continue
			}
			resp, err := ask(&reflectionpb.ServerReflectionRequest{
				MessageRequest: &reflectionpb.ServerReflectionRequest_FileByFilename{FileByFilename: dep},
			})
			if err != nil {
				return err
			}
			for _, raw := range resp.GetFileDescriptorResponse().GetFileDescriptorProto() {
				added := &descriptorpb.FileDescriptorProto{}
				if err := proto.Unmarshal(raw, added); err != nil {
					return err
				}
				pending[added.GetName()] = added
				asked = true
			}
		}
	}
	if !asked {
		return fmt.Errorf("unresolvable imports among %d files", len(pending))
	}
	return nil
}

// callWithoutIdentity calls m with an empty request and no identity header and
// returns the error the call ends with. A streaming call reports an interceptor's
// refusal when it is read, so it is read.
func callWithoutIdentity(ctx context.Context, conn *grpc.ClientConn, m protoreflect.MethodDescriptor) error {
	method := fmt.Sprintf("/%s/%s", m.Parent().FullName(), m.Name())
	req := dynamicpb.NewMessage(m.Input())
	resp := dynamicpb.NewMessage(m.Output())
	if !m.IsStreamingClient() && !m.IsStreamingServer() {
		return conn.Invoke(ctx, method, req, resp)
	}
	stream, err := conn.NewStream(ctx, &grpc.StreamDesc{
		StreamName:    string(m.Name()),
		ServerStreams: m.IsStreamingServer(),
		ClientStreams: m.IsStreamingClient(),
	}, method)
	if err != nil {
		return err
	}
	if err := stream.SendMsg(req); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	_ = stream.CloseSend()
	return stream.RecvMsg(resp)
}
