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

func ask(ctx context.Context, prompts ...string) (res string, err error) {
	seed := 0
	temp := float64(0)
	maxToken := 32

	messages := make([]anyllm.Message, 1+len(prompts))
	messages[0] = anyllm.Message{
		Role:    anyllm.RoleSystem,
		Content: caveman_prompt,
	}
	for i, p := range prompts {
		messages[1+i] = anyllm.Message{
			Role:    anyllm.RoleUser,
			Content: p,
		}
	}

	response, err := provider.Completion(ctx, anyllm.CompletionParams{
		Model:       "Qwen3.5-4B-Q6_K.gguf",
		Messages:    messages,
		Temperature: &temp,
		MaxTokens:   &maxToken,
		Seed:        &seed,
	})
	if err != nil {
		return
	}
	res = response.Choices[0].Message.Content.(string)
	return
}
