package main

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAddHandler(t *testing.T) {
	res, out, err := addHandler(context.Background(), nil, addArgs{A: 4, B: 5})
	if err != nil {
		t.Fatal(err)
	}
	if out.Sum != 9 {
		t.Fatalf("sum = %d, want 9", out.Sum)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok || tc.Text != "sum=9" {
		t.Fatalf("unexpected content: %#v", res.Content)
	}
}

func TestBuildServer_AddTool(t *testing.T) {
	ctx := context.Background()
	clientT, serverT := mcp.NewInMemoryTransports()
	if _, err := buildServer().Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "add", Arguments: map[string]any{"a": 10, "b": 20}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok || tc.Text != "sum=30" {
		t.Fatalf("unexpected result: %#v", res.Content)
	}
}
