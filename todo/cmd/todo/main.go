package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/joshsteveth/grpc-test/todo/todo"
	grpc "google.golang.org/grpc"
)

func main() {
	flag.Parse()
	if flag.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "missing subcommand: list or add")
		os.Exit(1)
	}

	port := ":" + flag.Arg(0)

	conn, err := grpc.Dial(port, grpc.WithInsecure())
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not connect to backend: %v\n", err)
		os.Exit(1)
	}

	client := todo.NewTasksClient(conn)
	switch cmd := flag.Arg(1); cmd {
	case "list":
		err = list(context.Background(), client)
	case "add":
		err = add(context.Background(), client, strings.Join(flag.Args()[2:], " "))
	default:
		err = fmt.Errorf("unknown subcommand %s", cmd)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func add(ctx context.Context, client todo.TasksClient, text string) error {
	_, err := client.Add(ctx, &todo.Text{Text: text})
	if err != nil {
		return fmt.Errorf("could not add task in the backend: %v", err)
	}
	fmt.Println("task added successfully")
	return nil
}

func list(ctx context.Context, client todo.TasksClient) error {
	l, err := client.List(ctx, &todo.Void{})
	if err != nil {
		return fmt.Errorf("could not fetch tasks: %v", err)
	}

	for _, t := range l.Tasks {
		if t.Done {
			fmt.Printf("[O]")
		} else {
			fmt.Printf("[X]")
		}
		fmt.Printf(" %s\n", t.Text)
	}
	return nil
}
