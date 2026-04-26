package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/openai"

	_ "embed"
)

//go:embed caveman_prompt.txt
var caveman_prompt string

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	provider, err := openai.New(
		anyllm.WithBaseURL("http://192.168.1.12:8080"),
		anyllm.WithAPIKey("xxx"),
	)
	if err != nil {
		panic(err)
	}
	response, err := provider.Completion(ctx, anyllm.CompletionParams{
		Model: "Qwen3.5-4B-Q6_K.gguf",
		Messages: []anyllm.Message{
			{
				Role:    anyllm.RoleSystem,
				Content: caveman_prompt,
			},
			{
				Role:    anyllm.RoleUser,
				Content: "explain copy constructors in C++",
			},
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(response.Choices[0].Message.Content)
}
