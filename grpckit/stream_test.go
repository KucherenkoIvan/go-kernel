package grpckit_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/KucherenkoIvan/go-kernel/auth"
	examplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/grpc/example/v1"
	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/grpckit"
	"github.com/KucherenkoIvan/go-kernel/grpckit/grpckittest"
)

// failure returns the outcome the echo handlers act out for a message, or nil.
func failure(message string) error {
	switch message {
	case "forbidden":
		return &echoForbiddenError{}
	case "panic":
		panic("boom")
	case "db":
		return errors.New("connection refused to 10.0.0.5:5432")
	}
	return nil
}

// EchoStream sends one message, then fails as the message says, or sends the
// rest of count.
func (s *echoServer) EchoStream(req *examplev1.EchoStreamRequest, stream grpc.ServerStreamingServer[examplev1.EchoStreamResponse]) error {
	s.gotRequestID = grpckit.RequestID(stream.Context())
	for i := range req.GetCount() {
		if err := stream.Send(&examplev1.EchoStreamResponse{Message: req.GetMessage()}); err != nil {
			return err
		}
		if i == 0 {
			if err := failure(req.GetMessage()); err != nil {
				return err
			}
		}
	}
	return nil
}

// EchoChat echoes every message until the client closes its side, failing on
// the messages failure knows.
func (s *echoServer) EchoChat(stream grpc.BidiStreamingServer[examplev1.EchoChatRequest, examplev1.EchoChatResponse]) error {
	s.gotRequestID = grpckit.RequestID(stream.Context())
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := failure(req.GetMessage()); err != nil {
			return err
		}
		if err := stream.Send(&examplev1.EchoChatResponse{Message: req.GetMessage()}); err != nil {
			return err
		}
	}
}

// recvAll drains a server stream: the messages, then the error that ended it
// (nil for a clean io.EOF).
func recvAll[T interface{ GetMessage() string }](recv func() (T, error)) ([]string, error) {
	var got []string
	for {
		msg, err := recv()
		if errors.Is(err, io.EOF) {
			return got, nil
		}
		if err != nil {
			return got, err
		}
		got = append(got, msg.GetMessage())
	}
}

func echoStream(t *testing.T, client examplev1.EchoServiceClient, ctx context.Context, message string, count int32) ([]string, error) {
	t.Helper()
	stream, err := client.EchoStream(ctx, &examplev1.EchoStreamRequest{Message: message, Count: count})
	if err != nil {
		t.Fatal(err)
	}
	return recvAll(stream.Recv)
}

func TestEchoStream_HappyPath_PropagatesRequestID(t *testing.T) {
	client, impl := setup(t)

	ctx := grpckit.WithRequestID(context.Background(), "req-stream")
	got, err := echoStream(t, client, ctx, "hello", 3)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "hello,hello,hello" {
		t.Errorf("received %q", got)
	}
	if impl.gotRequestID != "req-stream" {
		t.Errorf("request id on server = %q, want req-stream", impl.gotRequestID)
	}
}

func TestEchoStream_GeneratesRequestID(t *testing.T) {
	client, impl := setup(t)

	if _, err := echoStream(t, client, context.Background(), "hello", 1); err != nil {
		t.Fatal(err)
	}
	if impl.gotRequestID == "" {
		t.Error("stream handler should always see a request id")
	}
}

func TestEchoStream_DomainErrorEndsTheStream(t *testing.T) {
	client, _ := setup(t)

	got, err := echoStream(t, client, context.Background(), "forbidden", 3)
	if len(got) != 1 {
		t.Errorf("received %q before the error, want the one message sent", got)
	}
	var remote *grpckit.RemoteDomainError
	if !errors.As(err, &remote) || remote.Code != "echo_forbidden" {
		t.Fatalf("RemoteDomainError not restored from a stream: %v", err)
	}
	if !ddd.IsDomainError(err) {
		t.Error("ddd.IsDomainError lost on a stream")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("status = %v, want PermissionDenied (WithErrorCode mapping)", status.Code(err))
	}
}

func TestEchoStream_PanicAndInternalErrorAreMasked(t *testing.T) {
	client, _ := setup(t)

	for _, message := range []string{"panic", "db"} {
		_, err := echoStream(t, client, context.Background(), message, 3)
		if status.Code(err) != codes.Internal {
			t.Fatalf("%s: status = %v, want Internal", message, status.Code(err))
		}
		if st, _ := status.FromError(err); st.Message() != "internal error" {
			t.Errorf("%s: internal details leaked to the client: %q", message, st.Message())
		}
		if ddd.IsDomainError(err) {
			t.Errorf("%s: must not look like a domain error", message)
		}
	}
}

func TestEchoChat_Bidi_PropagatesRequestID(t *testing.T) {
	client, impl := setup(t)

	ctx := grpckit.WithRequestID(context.Background(), "req-chat")
	stream, err := client.EchoChat(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"a", "b", "c"} {
		if err := stream.Send(&examplev1.EchoChatRequest{Message: m}); err != nil {
			t.Fatal(err)
		}
		resp, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if resp.GetMessage() != m {
			t.Errorf("echo = %q, want %q", resp.GetMessage(), m)
		}
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Errorf("after CloseSend: %v, want io.EOF", err)
	}
	if impl.gotRequestID != "req-chat" {
		t.Errorf("request id on server = %q, want req-chat", impl.gotRequestID)
	}
}

func TestEchoChat_DomainErrorCrossesTheWire(t *testing.T) {
	client, _ := setup(t)

	stream, err := client.EchoChat(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&examplev1.EchoChatRequest{Message: "forbidden"}); err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv()
	var remote *grpckit.RemoteDomainError
	if !errors.As(err, &remote) || remote.Code != "echo_forbidden" {
		t.Fatalf("RemoteDomainError not restored from a bidi stream: %v", err)
	}
}

// streamAuthInterceptor is authInterceptor for streams: it hands the handler
// a stream whose Context carries the Principal.
func streamAuthInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if grpckit.RequestID(ss.Context()) == "" {
			return errors.New("the standard chain must run first")
		}
		md, _ := metadata.FromIncomingContext(ss.Context())
		switch token := md.Get("authorization"); {
		case len(token) == 0:
			return status.Error(codes.Unauthenticated, "unauthenticated")
		case token[0] == "banned":
			return &bannedError{}
		case token[0] == "panic":
			panic("auth exploded")
		}
		return handler(srv, &principalStream{
			ServerStream: ss,
			ctx:          auth.WithPrincipal(ss.Context(), auth.Principal{UserID: "u-1"}),
		})
	}
}

type principalStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *principalStream) Context() context.Context { return s.ctx }

func (s *principalEchoServer) EchoStream(_ *examplev1.EchoStreamRequest, stream grpc.ServerStreamingServer[examplev1.EchoStreamResponse]) error {
	p, ok := auth.PrincipalFrom(stream.Context())
	if !ok {
		return errors.New("principal must be set by the auth interceptor")
	}
	return stream.Send(&examplev1.EchoStreamResponse{Message: p.UserID})
}

func TestStreamInterceptor_AuthFlow(t *testing.T) {
	srv := grpckit.NewServer(
		grpckit.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		grpckit.WithStreamInterceptor(streamAuthInterceptor()),
	)
	examplev1.RegisterEchoServiceServer(srv, &principalEchoServer{})
	client := examplev1.NewEchoServiceClient(grpckittest.Serve(t, srv))

	callWith := func(token string) ([]string, error) {
		ctx := context.Background()
		if token != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", token)
		}
		return echoStream(t, client, ctx, "hi", 1)
	}

	if _, err := callWith(""); status.Code(err) != codes.Unauthenticated {
		t.Errorf("no token: %v, want Unauthenticated", err)
	}
	got, err := callWith("valid")
	if err != nil || len(got) != 1 || got[0] != "u-1" {
		t.Errorf("valid token: %q, %v; want the principal u-1", got, err)
	}
	var remote *grpckit.RemoteDomainError
	if _, err := callWith("banned"); !errors.As(err, &remote) || remote.Code != "user_banned" {
		t.Errorf("domain error from a stream interceptor not encoded: %v", err)
	}
	if _, err := callWith("panic"); status.Code(err) != codes.Internal {
		t.Errorf("panic in a stream interceptor: %v, want Internal", err)
	}
}

// heldStreamServer sends one message per stream, then holds the stream until
// its context ends and returns what a push loop would: the error of its next
// Send. It also ends the stream cleanly once shutdown is cancelled — the
// pattern for streams that should not hold up a shutdown.
type heldStreamServer struct {
	examplev1.UnimplementedEchoServiceServer
	shutdown context.Context
	entered  chan struct{}
	ended    chan error // the stream context's error when the handler returned
}

func newHeldStreamServer(shutdown context.Context) *heldStreamServer {
	return &heldStreamServer{shutdown: shutdown, entered: make(chan struct{}, 1), ended: make(chan error, 1)}
}

func (s *heldStreamServer) EchoStream(req *examplev1.EchoStreamRequest, stream grpc.ServerStreamingServer[examplev1.EchoStreamResponse]) error {
	ctx := stream.Context()
	defer func() { s.ended <- ctx.Err() }()
	msg := &examplev1.EchoStreamResponse{Message: req.GetMessage()}
	if err := stream.Send(msg); err != nil {
		return err
	}
	s.entered <- struct{}{}

	select {
	case <-ctx.Done():
		return stream.Send(msg)
	case <-s.shutdown.Done():
		return nil
	}
}

// syncBuffer is a bytes.Buffer safe for the server's handler goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestEchoStream_ClientCancelIsNotAHandlerError(t *testing.T) {
	logs := &syncBuffer{}
	debug := &slog.HandlerOptions{Level: slog.LevelDebug}
	srv := grpckit.NewServer(grpckit.WithLogger(slog.New(slog.NewTextHandler(logs, debug))))
	impl := newHeldStreamServer(context.Background())
	examplev1.RegisterEchoServiceServer(srv, impl)
	client := examplev1.NewEchoServiceClient(grpckittest.Serve(t, srv))

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.EchoStream(ctx, &examplev1.EchoStreamRequest{Message: "hold"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	<-impl.entered
	cancel()

	select {
	case ctxErr := <-impl.ended:
		if ctxErr == nil {
			t.Error("handler returned before its stream context ended")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not see the client's cancel")
	}
	if _, err := stream.Recv(); status.Code(err) != codes.Canceled {
		t.Errorf("client Recv after cancel = %v, want Canceled", err)
	}
	// The interceptor logs after the handler returns.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "rpc ended by context") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "level=DEBUG msg=\"rpc ended by context\"") {
		t.Errorf("the cancel was not logged at debug:\n%s", logs)
	}
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("a client cancel was logged as an error:\n%s", logs)
	}
}

// openHeldStream starts a held stream over a Unix socket served by Serve and
// returns once the handler holds it.
func openHeldStream(t *testing.T, srv *grpc.Server, impl *heldStreamServer, serveCtx context.Context) (grpc.ServerStreamingClient[examplev1.EchoStreamResponse], <-chan error) {
	t.Helper()
	lis, sock := unixListener(t)
	examplev1.RegisterEchoServiceServer(srv, impl)
	served := startServe(serveCtx, srv, lis)

	conn, err := grpckit.Connect("unix://" + sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	stream, err := examplev1.NewEchoServiceClient(conn).EchoStream(context.Background(), &examplev1.EchoStreamRequest{Message: "hold"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	<-impl.entered
	return stream, served
}

func TestServe_LongLivedStreamShutdownIsBounded(t *testing.T) {
	const timeout = 300 * time.Millisecond
	grpckit.SetShutdownTimeout(t, timeout)

	logs := &syncBuffer{}
	srv := grpckit.NewServer(grpckit.WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
	impl := newHeldStreamServer(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, served := openHeldStream(t, srv, impl, ctx)

	start := time.Now()
	cancel()
	if err := waitServe(t, served, timeout+5*time.Second); err != nil {
		t.Errorf("Serve after cancel = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed < timeout {
		t.Errorf("Serve returned after %v, before the %v grace period for the open stream", elapsed, timeout)
	}

	select {
	case ctxErr := <-impl.ended:
		if ctxErr == nil {
			t.Error("handler returned while its stream context was live")
		}
	default:
		t.Error("Serve returned before the stream handler did")
	}
	if _, err := stream.Recv(); err == nil {
		t.Error("client stream still open after Serve returned")
	}
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("a stream closed by shutdown was logged as an error:\n%s", logs)
	}
}

func TestServe_ShutdownAwareStreamEndsBeforeTheBound(t *testing.T) {
	const timeout = 5 * time.Second
	grpckit.SetShutdownTimeout(t, timeout)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	impl := newHeldStreamServer(ctx) // the handler watches the same ctx Serve does
	stream, served := openHeldStream(t, quietServer(), impl, ctx)

	start := time.Now()
	cancel()
	if err := waitServe(t, served, timeout); err != nil {
		t.Errorf("Serve after cancel = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > timeout/2 {
		t.Errorf("Serve took %v: a stream that ends on shutdown should not wait out the %v grace period", elapsed, timeout)
	}
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Errorf("client Recv = %v, want a clean io.EOF", err)
	}
}
