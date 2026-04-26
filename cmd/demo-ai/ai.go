package main

import (
	"context"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/openai"

	_ "embed"
)

var provider *openai.Provider

func init() {
	var err error
	provider, err = openai.New(
		anyllm.WithBaseURL("http://192.168.1.12:8080"),
		anyllm.WithAPIKey("xxx"),
	)
	if err != nil {
		panic(err)
	}
}

//go:embed caveman_prompt.txt
var caveman_prompt string

func ask(ctx context.Context, prompt string) string {
	seed := 0
	temp := float64(0)
	maxToken := 32
	response, err := provider.Completion(ctx, anyllm.CompletionParams{
		Model: "Qwen3.5-4B-Q6_K.gguf",
		Messages: []anyllm.Message{
			{
				Role:    anyllm.RoleSystem,
				Content: caveman_prompt,
			},
			{
				Role:    anyllm.RoleUser,
				Content: prompt,
			},
		},
		Temperature: &temp,
		MaxTokens:   &maxToken,
		Seed:        &seed,
	})
	if err != nil {
		panic(err)
	}
	return response.Choices[0].Message.Content.(string)
}
