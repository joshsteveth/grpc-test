package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/joshsteveth/grpc-test/todo/todo"
	"github.com/mwitkow/go-grpc-middleware"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	dbPath1 string = "mydb.pb"
	dbPath2        = "mydb2.pb"

	port1 int = 8888
	port2     = 8889
)

type server struct {
	srv  *grpc.Server
	port int
}

func main() {
	interceptors := []grpc.UnaryServerInterceptor{
		NewRequestTracingUnaryInterceptor,
	}

	middleware := grpc_middleware.WithUnaryServerChain(interceptors...)

	srv := grpc.NewServer(middleware)
	todo.RegisterTasksServer(srv, taskServer{dbPath1})

	srv2 := grpc.NewServer(middleware)
	todo.RegisterTasksServer(srv2, taskServer{dbPath2})

	servers := []server{{srv, port1}, {srv2, port2}}
	wg := new(sync.WaitGroup)

	for _, s := range servers {
		wg.Add(1)
		go serveGRPCServer(s.srv, s.port, wg)
	}

	wg.Wait()
}

func serveGRPCServer(srv *grpc.Server, port int, wg *sync.WaitGroup) {
	defer wg.Done()
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("could not listen to :%d: %v", port, err)
	}

	fmt.Printf("Listening on port: %d\n", port)

	log.Fatal(srv.Serve(l))
}

type length int64

const (
	tidLength    int = 10
	sizeOfLength     = 8

	tid string = "traceid"
)

func NewRequestTracingUnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	// check if traceID is already there
	// if yes then simply forward it into outgoing context
	// because we want it to be forwarded into the next grpc service as incoming md
	if traceID := getTraceIDFromMetaData(ctx); traceID != "" {
		// UNCOMMENT THIS TO APPEND TRACEID INTO OUTGOING METADATA
		//ctx = metadata.AppendToOutgoingContext(ctx, tid, traceID)
		return handler(ctx, req)
	}

	// if no then generate a new one + append to both incoming and outgoing
	newTraceID := randString(tidLength)
	// UNCOMMENT THIS TO APPEND TRACEID INTO OUTGOING METADATA
	//ctx = metadata.AppendToOutgoingContext(ctx, tid, newTraceID)

	md, _ := metadata.FromIncomingContext(ctx)
	md.Append(tid, newTraceID)
	return handler(metadata.NewIncomingContext(ctx, md), req)
}

func withClientTracingUnaryInterceptor(
	ctx context.Context,
	method string,
	req interface{},
	reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption) error {
	traceID := getTraceIDFromMetaData(ctx)

	ctx = metadata.AppendToOutgoingContext(ctx, tid, traceID)

	return invoker(ctx, method, req, reply, cc, opts...)
}

func getTraceIDFromMetaData(ctx context.Context) (traceID string) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		fmt.Println("metadata is not available")
		return
	}

	tids, ok := md[tid]
	if !ok {
		fmt.Println("tid is empty", md)
		return
	}

	return tids[0]
}

var endianness = binary.LittleEndian

type taskServer struct {
	dbPath string
}

func (s taskServer) List(ctx context.Context, void *todo.Void) (*todo.TaskList, error) {
	printAllMetadata(ctx, s.dbPath)

	b, err := ioutil.ReadFile(s.dbPath)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %v", s.dbPath, err)
	}

	var tasks todo.TaskList

	// try to fetch some tasklist from other service first
	if otherTask, err := s.getListFromOtherService(ctx, void); err == nil {
		tasks.Tasks = append(tasks.Tasks, otherTask.Tasks...)
	}

	for {
		if len(b) == 0 {
			return &tasks, nil
		} else if len(b) < sizeOfLength {
			return nil, fmt.Errorf("remaining odd %d bytes, what to do?", len(b))
		}

		var l length
		if err := binary.Read(bytes.NewReader(b[:sizeOfLength]), endianness, &l); err != nil {
			return nil, fmt.Errorf("could not decode message length: %v", err)
		}
		b = b[sizeOfLength:]

		var task todo.Task
		if err := proto.Unmarshal(b[:l], &task); err != nil {
			return nil, fmt.Errorf("could not read task: %v", err)
		}

		b = b[l:]
		tasks.Tasks = append(tasks.Tasks, &task)
	}
}

var (
	// this is also kinda stupid
	// if we are serving the 8888 one, we want to get from 8889
	// and vice versa
	otherServicePortMap = map[string]string{
		dbPath1: fmt.Sprintf(":%d", port2),
		dbPath2: fmt.Sprintf(":%d", port1),
	}
)

func (s taskServer) getListFromOtherService(ctx context.Context, void *todo.Void) (*todo.TaskList, error) {
	if s.dbPath != dbPath1 {
		return nil, fmt.Errorf("not implemented")
	}

	port := otherServicePortMap[s.dbPath]

	conn, err := grpc.Dial(
		port,
		grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(withClientTracingUnaryInterceptor))
	if err != nil {
		return nil, fmt.Errorf("Invalid to do grpc dial to %s: %v", port, err)
	}
	defer conn.Close()

	client := todo.NewTasksClient(conn)

	return client.List(ctx, void)
}

func (s taskServer) Add(ctx context.Context, text *todo.Text) (*todo.Task, error) {
	printAllMetadata(ctx, s.dbPath)

	//fmt.Printf("[%v] Add api called with text: %v\n", time.Now(), text.Text)
	task := &todo.Task{
		Text: text.Text,
		Done: false,
	}

	b, err := proto.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("could not encode task: %v", err)
	}

	f, err := os.OpenFile(s.dbPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("could not open file %s: %v", s.dbPath, err)
	}

	if err = binary.Write(f, endianness, length(len(b))); err != nil {
		return nil, fmt.Errorf("could not encode length of message: %v", err)
	}

	_, err = f.Write(b)
	if err != nil {
		return nil, fmt.Errorf("could not write to file: %v", err)
	}

	if err = f.Close(); err != nil {
		return nil, fmt.Errorf("could not close file: %v", err)
	}

	return task, nil
}

func printAllMetadata(ctx context.Context, dbname string) {
	printIncomingMetadata(ctx, dbname)
	printOutgoingMetadata(ctx, dbname)
}

func printIncomingMetadata(ctx context.Context, dbname string) {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		fmt.Printf("[%s] incoming metadata: %+v\n", dbname, md)
		return
	}

	fmt.Printf("[%s] incoming metadata is not found\n", dbname)
}

func printOutgoingMetadata(ctx context.Context, dbname string) {
	md, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		fmt.Printf("[%s] outgoing metadata: %+v\n", dbname, md)
		return
	}

	fmt.Printf("[%s] outgoing metadata is not found\n", dbname)
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randString(n int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
